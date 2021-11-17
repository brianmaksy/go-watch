package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brianmaksy/go-watch/internal/certificateutils"
	"github.com/brianmaksy/go-watch/internal/models"
	"github.com/go-chi/chi/v5"
)

const (
	HTTP           = 1
	HTTPS          = 2
	SSLCertificate = 3
)

type jsonResp struct {
	OK            bool      `json:"ok"`
	Message       string    `json:"message"`
	ServiceID     int       `json:"service_id"`
	HostServiceID int       `json:"host_service_id"`
	HostID        int       `json:"host_id"`
	OldStatus     string    `json:"old_status"`
	NewStatus     string    `json:"new_status"`
	LastCheck     time.Time `json:"last_check"`
}

// ScheduledCheck performs a scheduled check on a host service by id
// nts - this is run in the cron package via scheduleID, err := app.Scheduler.AddJob(schedule, j)? in start-mon.go
func (repo *DBRepo) ScheduledCheck(hostServiceID int) {
	log.Println("***** Running check for", hostServiceID)

	// get host and hostservice
	hs, err := repo.DB.GetHostServiceByID(hostServiceID)
	if err != nil {
		log.Println(err)
		return
	}
	h, err := repo.DB.GetHostByID(hs.HostID)
	if err != nil {
		log.Println(err)
		return
	}

	// tests the service
	newStatus, msg := repo.testServiceForHost(h, hs)

	// now reports only when status changes?
	if newStatus != hs.Status {
		repo.updateHostServiceStatusCount(h, hs, newStatus, msg)
	}

}

func (repo *DBRepo) updateHostServiceStatusCount(h models.Host, hs models.HostService, newStatus, msg string) {
	// update host service record in db with status and last check if changed
	hs.Status = newStatus
	hs.LastMessage = msg
	hs.LastCheck = time.Now()
	err := repo.DB.UpdateHostService(hs)
	if err != nil {
		log.Println(err)
		return
	}

	// nts - get updated values
	pending, healthy, warning, problem, err := repo.DB.GetAllServiceStatusCounts()
	if err != nil {
		log.Println(err)
		return
	}
	data := make(map[string]string)
	data["healthy_count"] = strconv.Itoa(healthy)
	data["pending_count"] = strconv.Itoa(pending)
	data["problem_count"] = strconv.Itoa(problem)
	data["warning_count"] = strconv.Itoa(warning)
	repo.broadcastMessage("public-channel", "host-service-count-changed", data)

	log.Println("New status is", newStatus, "and msg is", msg)
}

func (repo *DBRepo) broadcastMessage(channel, messageType string, data map[string]string) {
	err := app.WsClient.Trigger(channel, messageType, data) // nts - use js on client
	if err != nil {
		log.Println(err)
	}
}

// TestCheck manually tests a host service and sends JSON response (nts - the button next to serviceName in host.jet)
func (repo *DBRepo) TestCheck(w http.ResponseWriter, r *http.Request) {
	hostServiceID, _ := strconv.Atoi(chi.URLParam(r, "id"))
	oldStatus := chi.URLParam(r, "oldStatus")
	okay := true

	// get host service
	hs, err := repo.DB.GetHostServiceByID(hostServiceID)
	if err != nil {
		log.Println(err)
		okay = false
	}

	// get host
	h, err := repo.DB.GetHostByID(hs.HostID)
	if err != nil {
		log.Println(err)
		okay = false
	}
	// test the service
	newStatus, msg := repo.testServiceForHost(h, hs)

	// broadcast service status changed event (take service from old status tab to new server's tab)
	if newStatus != hs.Status {
		repo.pushStatusChangedEvent(h, hs, newStatus)
		event := models.Event{
			HostServiceID: hs.ServiceID,
			EventType:     newStatus,
			HostID:        h.ID,
			ServiceName:   hs.Service.ServiceName,
			HostName:      h.HostName,
			Message:       msg,
			CreatedAt:     time.Time{},
			UpdatedAt:     time.Time{},
		}

		err := repo.DB.InsertEvent(event)
		if err != nil {
			log.Println(err)
		}
	}

	// update the host service in the DB with status (if changed) and last check
	hs.Status = newStatus
	hs.LastMessage = msg
	hs.LastCheck = time.Now()
	hs.UpdatedAt = time.Now()

	err = repo.DB.UpdateHostService(hs) // nts - overwriting DB values.
	if err != nil {
		log.Println(err)
		okay = false
	}

	var resp jsonResp
	// create json response
	if okay {
		resp = jsonResp{
			OK:            true,
			Message:       msg,
			ServiceID:     hs.ServiceID,
			HostServiceID: hs.ID,
			HostID:        hs.HostID,
			OldStatus:     oldStatus,
			NewStatus:     newStatus,
			LastCheck:     time.Now(),
		}
	} else {
		resp.OK = false
		resp.Message = "Something went wrong"
	}

	// send json to client

	out, _ := json.MarshalIndent(resp, "", "    ") // this sends to host.jet's checkNow(id, oldStatus) function
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

func (repo *DBRepo) testServiceForHost(h models.Host, hs models.HostService) (string, string) {
	var msg, newStatus string

	switch hs.ServiceID {
	case HTTP:
		msg, newStatus = testHTTPForHost(h.URL)
		break

	case HTTPS:
		msg, newStatus = testHTTPSForHost(h.URL)
		break

	case SSLCertificate:
		msg, newStatus = testSSLForHost(h.URL)
		break
	}

	// broadcast to clients if appropriate
	if hs.Status != newStatus {
		repo.pushStatusChangedEvent(h, hs, newStatus)
		// save event
		event := models.Event{
			HostServiceID: hs.ServiceID,
			EventType:     newStatus,
			HostID:        h.ID,
			ServiceName:   hs.Service.ServiceName,
			HostName:      h.HostName,
			Message:       msg,
			CreatedAt:     time.Time{},
			UpdatedAt:     time.Time{},
		}

		err := repo.DB.InsertEvent(event)
		if err != nil {
			log.Println(err)
		}
	}
	repo.pushScheduleChangedEvent(hs, newStatus)

	// if appropriate, send email or SMS message.

	return newStatus, msg
}

func (repo *DBRepo) pushStatusChangedEvent(h models.Host, hs models.HostService, newStatus string) {
	yearOne := time.Date(0001, 2, 2, 0, 0, 0, 1, time.UTC) // nts - changed to feb, because if set to all one, then may actually be after this! (day light saving?!)

	data := make(map[string]string)
	data["host_id"] = strconv.Itoa(hs.HostID)
	data["host_service_id"] = strconv.Itoa(hs.ID)
	data["host_name"] = h.HostName
	data["service_name"] = hs.Service.ServiceName
	data["icon"] = hs.Service.Icon
	data["status"] = newStatus
	data["message"] = fmt.Sprintf("host service %s on %s has changed to %s", hs.Service.ServiceName, h.HostName, newStatus)
	if hs.LastCheck.After(yearOne) {
		data["last_run"] = time.Now().Format("2006-01-02 3:04:05 PM")
	} else {
		data["last_run"] = "Pending"
	}
	// nts - status change, not count.
	repo.broadcastMessage("public-channel", "host-service-status-changed", data)
}

func (repo *DBRepo) pushScheduleChangedEvent(hs models.HostService, newStatus string) {
	yearOne := time.Date(0001, 2, 2, 0, 0, 0, 1, time.UTC)
	data := make(map[string]string)
	// broadcast schedule has changed
	data["host_service_id"] = strconv.Itoa(hs.ID)
	data["service_id"] = strconv.Itoa(hs.ServiceID)
	data["host_id"] = strconv.Itoa(hs.HostID)

	// nts - .Next = next scheduled cron job.
	if app.Scheduler.Entry(repo.App.MonitorMap[hs.ID]).Next.After(yearOne) {
		data["next_run"] = repo.App.Scheduler.Entry(repo.App.MonitorMap[hs.ID]).Next.Format("2006-01-02 3:04:05 PM")
	} else {
		data["next_run"] = "Pending"
	}
	if hs.LastCheck.After(yearOne) {
		data["last_run"] = time.Now().Format("2006-01-02 3:04:05 PM")
	} else {
		data["last_run"] = "Pending"
	}
	data["host"] = hs.HostName
	data["service"] = hs.Service.ServiceName
	data["schedule"] = fmt.Sprintf("@every %d%s", hs.ScheduleNumber, hs.ScheduleUnit) // nts - data type is a formatted string
	data["status"] = newStatus
	data["icon"] = hs.Service.Icon

	repo.broadcastMessage("public-channel", "schedule-changed-event", data) // also broadcasted when starting monitoring.
	// nts - rebroadcast after change?
}

func testHTTPForHost(url string) (string, string) {
	// strip suffix
	if strings.HasSuffix(url, "/") {
		url = strings.TrimSuffix(url, "/")
	}
	url = strings.Replace(url, "https://", "http://", -1) // -1 or smaller means no lim on num of replacements

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Sprintf("%s - %s", url, "error connecting"), "problem"
	}
	defer resp.Body.Close() // nts - otherwise get mem leak

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("%s - %s", url, resp.Status), "problem"
	}
	return fmt.Sprintf("%s - %s", url, resp.Status), "healthy"

}

func testHTTPSForHost(url string) (string, string) {
	// strip suffix
	if strings.HasSuffix(url, "/") {
		url = strings.TrimSuffix(url, "/")
	}
	url = strings.Replace(url, "http://", "https://", -1) // -1 or smaller means no lim on num of replacements

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Sprintf("%s - %s", url, "error connecting"), "problem"
	}
	defer resp.Body.Close() // nts - otherwise get mem leak

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("%s - %s", url, resp.Status), "problem"
	}
	return fmt.Sprintf("%s - %s", url, resp.Status), "healthy"

}

func scanHost(hostname string, certDetailsChannel chan certificateutils.CertificateDetails, errorsChannel chan error) {

	res, err := certificateutils.GetCertificateDetails(hostname, 10)
	if err != nil {
		errorsChannel <- err
	} else {
		certDetailsChannel <- res
	}
}

func testSSLForHost(url string) (string, string) {
	if strings.HasPrefix(url, "https://") {
		url = strings.Replace(url, "https://", "", -1)
	}
	if strings.HasPrefix(url, "http://") {
		url = strings.Replace(url, "http://", "", -1)
	}

	var certDetailsChannel chan certificateutils.CertificateDetails
	var errorsChannel chan error
	certDetailsChannel = make(chan certificateutils.CertificateDetails, 1)
	errorsChannel = make(chan error, 1)

	var msg, newStatus string

	scanHost(url, certDetailsChannel, errorsChannel)

	// nts - for loop with two var declared. len(certDetailsChannel) doesn't change.
	for i, certDetailsInQueue := 0, len(certDetailsChannel); i < certDetailsInQueue; i++ {
		certDetails := <-certDetailsChannel
		certificateutils.CheckExpirationStatus(&certDetails, 30)

		if certDetails.ExpiringSoon {

			if certDetails.DaysUntilExpiration < 7 {
				msg = certDetails.Hostname + " expiring in " + strconv.Itoa(certDetails.DaysUntilExpiration) + " days"
				newStatus = "problem"
			} else {
				msg = certDetails.Hostname + " expiring in " + strconv.Itoa(certDetails.DaysUntilExpiration) + " days"
				newStatus = "warning"
			}

		} else {
			msg = certDetails.Hostname + " expiring in " + strconv.Itoa(certDetails.DaysUntilExpiration) + " days"
			newStatus = "healthy"
		}
	}
	return msg, newStatus
}

func (repo *DBRepo) addToMonitorMap(hs models.HostService) {
	if repo.App.PreferenceMap["monitoring_live"] == "1" {
		var j job
		j.HostServiceID = hs.ID
		scheduleID, err := repo.App.Scheduler.AddJob(fmt.Sprintf("@every %d%s", hs.ScheduleNumber, hs.ScheduleUnit), j)
		if err != nil {
			log.Println(err)
			return
		}

		repo.App.MonitorMap[hs.ID] = scheduleID
		data := make(map[string]string)
		data["message"] = "scheduling"
		data["host_service_id"] = strconv.Itoa(hs.ID)
		data["next_run"] = "Pending"
		data["service"] = hs.Service.ServiceName
		data["host"] = hs.HostName
		data["last_run"] = hs.LastCheck.Format("2006-01-02 3:04:05 PM")
		data["schedule"] = fmt.Sprintf("@every %d%s", hs.ScheduleNumber, hs.ScheduleUnit)

		repo.broadcastMessage("public-channel", "schedule-changed-event", data)
	}
}

// **nts - still has problem of duplication, and when removing, doesn't remove original entry added in schedule.go
func (repo *DBRepo) removeFromMonitorMap(hs models.HostService) {
	if repo.App.PreferenceMap["monitoring_live"] == "1" {
		repo.App.Scheduler.Remove(repo.App.MonitorMap[hs.ID])
		data := make(map[string]string)
		data["host_service_id"] = strconv.Itoa(hs.ID)
		repo.broadcastMessage("public-channel", "schedule-item-removed-event", data)

	}
}
