package handlers

import (
	"fmt"
	"log"
	"strconv"
	"time"
)

type job struct {
	HostServiceID int
}

// nts - not a pointer receiver!
func (j job) Run() {
	// to put: unit of work to be performed
	Repo.ScheduledCheck(j.HostServiceID) // access to ScheduledCheck (which is in handlers package (perform-check.go)
	// via Repo in caps declared in handlers.go.
	// cf lower case r for repo *DBRepo methods, this method belongs to job struct. Not DBRepo struct.
}

// nts - no need app.Scheduler.Start() in setup-app.go, since it's called in ToggleMonitoring in handlers.go
func (repo *DBRepo) StartMonitoring() {
	if app.PreferenceMap["monitoring_live"] == "1" {
		log.Println("**********starting monitoring***********")
		// if set to zero, we don't want to monitor.

		// TODO trigger a message to broadcast to all clients (letting them know app is starting to monitor)
		data := make(map[string]string)
		data["message"] = "Monitoring is starting..."

		// nts - need to listen to event too, else nothing happens

		err := app.WsClient.Trigger("public-channel", "app-starting", data) // nts - use js onn client
		if err != nil {
			log.Println(err)
		}

		// get all services we want to monitor
		servicesToMonitor, err := repo.DB.GetServicesToMonitor()
		if err != nil {
			log.Println(err)
		}

		// range through the services
		for _, x := range servicesToMonitor {
			log.Println("*** Services to monitor on", x.HostName, "is", x.Service.ServiceName)

			// get the schedule unit and number
			var schedule string // the frequency of a job .
			if x.ScheduleUnit == "d" {
				schedule = fmt.Sprintf("@every %d%s", x.ScheduleNumber*24, "h") // change to 24 hours
			} else {
				schedule = fmt.Sprintf("@every %d%s", x.ScheduleNumber, x.ScheduleUnit)
			}
			// create a job of type job
			var j job
			j.HostServiceID = x.ID
			// save the id of the job so we can start/stop it.
			// schedule ID is of type cron.EntryID
			scheduleID, err := app.Scheduler.AddJob(schedule, j)
			if err != nil {
				log.Println(err)
			}

			app.MonitorMap[x.ID] = scheduleID
			// log.Printf("%s", x.HostName)

			// for each of these task, to broadcast over websockets the fact that the service is scheduled.
			// i.e. pending
			payload := make(map[string]string)
			payload["message"] = "scheduling"
			payload["host_service_id"] = strconv.Itoa(x.ID)
			// nts - non null values. Only concerned about year here.
			yearOne := time.Date(0001, 11, 17, 20, 34, 58, 651387, time.UTC)
			// nts - check if the next entry is after year one (?)
			if app.Scheduler.Entry(app.MonitorMap[x.ID]).Next.After(yearOne) {
				payload["next_run"] = app.Scheduler.Entry(app.MonitorMap[x.ID]).Next.Format("2006-01-02 3:04:05 PM")
			} else {
				// "default" - if year one (0001-01-01 default value)
				payload["next_run"] = "Pending..."
			}
			payload["host"] = x.HostName
			payload["service"] = x.Service.ServiceName
			if x.LastCheck.After(yearOne) {
				payload["last_run"] = x.LastCheck.Format("2006-01-02 3:04:05 PM")
			} else {
				payload["last_run"] = "Pending..."
			}
			payload["schedule"] = fmt.Sprintf("@every %d%s", x.ScheduleNumber, x.ScheduleUnit)

			// first to send is next-run-event (next iteration)
			err = app.WsClient.Trigger("public-channel", "next-run-event", payload)
			if err != nil {
				log.Println(err)
			}

			// next event - schedule change.
			err = app.WsClient.Trigger("public-channel", "schedule-changed-event", payload)
			if err != nil {
				log.Println(err)
			}

		}
		// nts - check to see which services being monitored.
		// for k, v := range app.MonitorMap {
		// 	log.Printf("serviceID is %d and cron id is %v", k, v)
		// }
	}
}
