package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brianmaksy/go-watch/internal/models"
	"github.com/go-chi/chi/v5"
)

const (
	HTTP           = 1 // nts using one service for now
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

	hostServiceStatusChanged := false
	if newStatus != hs.Status {
		hostServiceStatusChanged = true
	}
	// if the host service status has changed, broadcast to all clients.
	if hostServiceStatusChanged {
		data := make(map[string]string)
		data["message"] = fmt.Sprintf("host service %s on %s has changed to %s", hs.Service.ServiceName, h.HostName, newStatus)
		repo.broadcastMessage("public-channel", "host-service-status-changed", data)

		// if appropriate, send email or SMS message.

	}

	// update host service record in db with status if changed
	// update last_check record

	hs.Status = newStatus
	hs.LastCheck = time.Now()
	err = repo.DB.UpdateHostService(hs)
	if err != nil {
		log.Println(err)
		return
	}

	if hostServiceStatusChanged {
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
	}

	log.Println("New status is", newStatus, "and msg is", msg)
}

func (repo *DBRepo) broadcastMessage(channel, messageType string, data map[string]string) {
	err := app.WsClient.Trigger(channel, messageType, data) // nts - use js on client
	if err != nil {
		log.Println(err)
	}
}

// TestCheck manually tests a host service and sends JSON response
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

	// update the host service in the DB with status (if changed) and last check
	hs.Status = newStatus
	hs.LastCheck = time.Now()
	hs.UpdatedAt = time.Now()

	err = repo.DB.UpdateHostService(hs) // nts - overwriting DB values.
	if err != nil {
		log.Println(err)
		okay = false
	}

	// broadcast service status changed event (take service from old status tab to new server's tab)

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
	}
	return newStatus, msg
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
