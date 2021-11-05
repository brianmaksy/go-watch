package main

import (
	"net/http"

	"github.com/brianmaksy/go-watch/internal/handlers"
	"github.com/go-chi/chi/v5"
)

func routes() http.Handler {

	mux := chi.NewRouter()

	// loading default middleware
	mux.Use(SessionLoad)
	mux.Use(RecoverPanic)
	mux.Use(NoSurf)
	mux.Use(CheckRemember) // to verifies if someone has checked remember me -> logs in automatically.

	// login
	mux.Get("/", handlers.Repo.LoginScreen)
	mux.Post("/", handlers.Repo.Login)

	mux.Get("/user/logout", handlers.Repo.Logout)

	// test route
	// mux.Get("/pusher-test", handlers.Repo.TestPusher)

	mux.Route("/pusher", func(mux chi.Router) {
		mux.Use(Auth) // privileged access
		mux.Post("/auth", handlers.Repo.PusherAuth)
	})

	// admin routes - protected.
	mux.Route("/admin", func(mux chi.Router) {
		// all admin routes are protected
		mux.Use(Auth)

		// overview
		mux.Get("/overview", handlers.Repo.AdminDashboard)

		// events
		mux.Get("/events", handlers.Repo.Events)

		// settings
		mux.Get("/settings", handlers.Repo.Settings)
		mux.Post("/settings", handlers.Repo.PostSettings)

		// service status pages (all hosts)
		mux.Get("/all-healthy", handlers.Repo.AllHealthyServices)
		mux.Get("/all-warning", handlers.Repo.AllWarningServices)
		mux.Get("/all-problems", handlers.Repo.AllProblemServices)
		mux.Get("/all-pending", handlers.Repo.AllPendingServices)

		// users
		mux.Get("/users", handlers.Repo.AllUsers)
		mux.Get("/user/{id}", handlers.Repo.OneUser)
		mux.Post("/user/{id}", handlers.Repo.PostOneUser)
		mux.Get("/user/delete/{id}", handlers.Repo.DeleteUser)

		// schedule
		mux.Get("/schedule", handlers.Repo.ListEntries)

		// preferences
		mux.Post("/preference/ajax/set-system-pref", handlers.Repo.SetSystemPref)
		mux.Post("/preference/ajax/toggle-monitoring", handlers.Repo.ToggleMonitoring)

		// hosts
		mux.Get("/host/all", handlers.Repo.AllHosts)
		mux.Get("/host/{id}", handlers.Repo.Host)
		mux.Post("/host/{id}", handlers.Repo.PostHost)
		mux.Post("/host/ajax/toggle-service", handlers.Repo.ToggleServiceForHost)
		mux.Get("/perform-check/{id}/{oldStatus}", handlers.Repo.TestCheck)

	})

	// static files
	fileServer := http.FileServer(http.Dir("./static/"))
	mux.Handle("/static/*", http.StripPrefix("/static", fileServer))

	return mux
}
