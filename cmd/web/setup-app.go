package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/alexedwards/scs/postgresstore"
	"github.com/alexedwards/scs/v2"
	"github.com/brianmaksy/go-watch/internal/channeldata"
	"github.com/brianmaksy/go-watch/internal/config"
	"github.com/brianmaksy/go-watch/internal/driver"
	"github.com/brianmaksy/go-watch/internal/handlers"
	"github.com/brianmaksy/go-watch/internal/helpers"
	"github.com/pusher/pusher-http-go"
	"github.com/robfig/cron/v3"
)

func setupApp() (*string, error) {
	// read flags
	insecurePort := flag.String("port", ":4000", "port to listen on")
	identifier := flag.String("identifier", "go_watch", "unique identifier")
	domain := flag.String("domain", "localhost", "domain name (e.g. example.com)")
	inProduction := flag.Bool("production", false, "application is in production")
	dbHost := flag.String("dbhost", "localhost", "database host")
	dbPort := flag.String("dbport", "5432", "database port")
	dbUser := flag.String("dbuser", "", "database user")
	dbPass := flag.String("dbpass", "", "database password")
	databaseName := flag.String("db", "go_watch", "database name")
	dbSsl := flag.String("dbssl", "disable", "database ssl setting")
	pusherHost := flag.String("pusherHost", "", "pusher host")
	pusherPort := flag.String("pusherPort", "443", "pusher port")
	pusherApp := flag.String("pusherApp", "9", "pusher app id")
	pusherKey := flag.String("pusherKey", "", "pusher key")
	pusherSecret := flag.String("pusherSecret", "", "pusher secret")
	pusherSecure := flag.Bool("pusherSecure", false, "pusher server uses SSL (true or false)")

	flag.Parse()

	if *dbUser == "" || *dbHost == "" || *dbPort == "" || *databaseName == "" || *identifier == "" {
		fmt.Println("Missing required flags.")
		os.Exit(1)
	}

	// only postgres - repository pattern.
	log.Println("Connecting to database....")
	dsnString := ""

	// when developing locally, we often don't have a db password
	if *dbPass == "" {
		dsnString = fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s timezone=UTC connect_timeout=5",
			*dbHost,
			*dbPort,
			*dbUser,
			*databaseName,
			*dbSsl)
	} else {
		dsnString = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s timezone=UTC connect_timeout=5",
			*dbHost,
			*dbPort,
			*dbUser,
			*dbPass,
			*databaseName,
			*dbSsl)
	}

	// pinging.
	db, err := driver.ConnectPostgres(dsnString)
	if err != nil {
		log.Fatal("Cannot connect to database!", err)
	}

	// session
	log.Printf("Initializing session manager....")
	session = scs.New()
	session.Store = postgresstore.New(db.SQL)
	session.Lifetime = 24 * time.Hour
	session.Cookie.Persist = true
	session.Cookie.Name = fmt.Sprintf("gbsession_id_%s", *identifier)
	session.Cookie.SameSite = http.SameSiteLaxMode
	session.Cookie.Secure = *inProduction

	// start mail channel
	log.Println("Initializing mail channel and worker pool....")
	mailQueue := make(chan channeldata.MailJob, maxWorkerPoolSize) // set to be 5 now.

	// Start the email dispatcher
	log.Println("Starting email dispatcher....")
	dispatcher := NewDispatcher(mailQueue, maxJobMaxWorkers)
	dispatcher.run()

	// define application configuration - share across application.
	a := config.AppConfig{
		DB:           db,
		Session:      session,
		InProduction: *inProduction,
		Domain:       *domain,
		PusherSecret: *pusherSecret, // default: 123abc etc
		MailQueue:    mailQueue,
		Version:      go_watchVersion,
		Identifier:   *identifier,
	}

	app = a

	repo = handlers.NewPostgresqlHandlers(db, &app) // nts - call this first (one var)
	handlers.NewHandlers(repo, &app)                // nts - then use the declared repo here.
	// nts - repo has access to both repository.DatabaseRepo methods and appconfig params.

	log.Println("Getting preferences...")
	preferenceMap = make(map[string]string) // [NTS - strictly speaking not needed since declared in main.go already?]
	preferences, err := repo.DB.AllPreferences()
	if err != nil {
		log.Fatal("Cannot read preferences:", err)
	}

	for _, pref := range preferences {
		preferenceMap[pref.Name] = string(pref.Preference)
	}

	preferenceMap["pusher-host"] = *pusherHost
	preferenceMap["pusher-port"] = *pusherPort
	preferenceMap["pusher-key"] = *pusherKey
	preferenceMap["identifier"] = *identifier
	preferenceMap["version"] = go_watchVersion

	app.PreferenceMap = preferenceMap

	// create pusher client. The official Go library for pusher. Use local pusher clone instead.
	wsClient = pusher.Client{
		AppID:  *pusherApp,
		Secret: *pusherSecret,
		Key:    *pusherKey,
		Secure: *pusherSecure,
		Host:   fmt.Sprintf("%s:%s", *pusherHost, *pusherPort),
	}

	log.Println("Host", fmt.Sprintf("%s:%s", *pusherHost, *pusherPort))
	log.Println("Secure", *pusherSecure)

	app.WsClient = wsClient
	// nts - since we can't assign entry to nil map (called in start-monitoring.go)
	monitorMap := make(map[int]cron.EntryID)
	app.MonitorMap = monitorMap

	// create a timer - wil hold our schedule
	localZone, _ := time.LoadLocation("Local")
	scheduler := cron.New(cron.WithLocation(localZone), cron.WithChain(
		cron.DelayIfStillRunning(cron.DefaultLogger),
		cron.Recover(cron.DefaultLogger),
	))
	app.Scheduler = scheduler
	// NTS - need to run this now. start-monitoring.go

	go handlers.Repo.StartMonitoring()

	// nts - do actually need this here: (to start monitoring when start app)?
	// otherwise need to turn on and off again.
	if app.PreferenceMap["monitoring_live"] == "1" {
		app.Scheduler.Start()
	}

	helpers.NewHelpers(&app)

	return insecurePort, err
}

// createDirIfNotExist creates a directory if it does not exist
func createDirIfNotExist(path string) error {
	const mode = 0755
	if _, err := os.Stat(path); os.IsNotExist(err) {
		err := os.Mkdir(path, mode)
		if err != nil {
			log.Println(err)
			return err
		}
	}
	return nil
}
