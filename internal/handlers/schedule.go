package handlers

import (
	"fmt"
	"log"
	"net/http"
	"sort"

	"github.com/CloudyKit/jet/v6"
	"github.com/brianmaksy/go-watch/internal/helpers"
	"github.com/brianmaksy/go-watch/internal/models"
)

type ByHost []models.Schedule

// nts - create three functions
// Len is used to sort by host
func (a ByHost) Len() int { return len(a) }

// Less is used to sort by host
func (a ByHost) Less(i, j int) bool { return a[i].Host < a[j].Host }

// Swap is used to sort by host
func (a ByHost) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// ListEntries lists schedule entries
func (repo *DBRepo) ListEntries(w http.ResponseWriter, r *http.Request) {
	var items []models.Schedule

	// NTS - not from DB, but monitormap from app
	for k, v := range repo.App.MonitorMap {
		var item models.Schedule
		// item.ID = k // nts - the teacher is wrong! should get key of host_services
		item.EntryID = v
		item.Entry = app.Scheduler.Entry(v)

		hs, err := repo.DB.GetHostServiceByID(k)
		if err != nil {
			log.Println(err)
			return
		}
		// log.Printf("%s", hs.HostName)
		item.ScheduleText = fmt.Sprintf("@every %d%s", hs.ScheduleNumber, hs.ScheduleUnit)
		item.LastRunFromHS = hs.LastCheck
		item.Host = hs.HostName
		item.Service = hs.Service.ServiceName
		items = append(items, item)
		// nts - to sort []. Since Map doesn't sort. Not too easy to do
		// log.Printf("%s", hs.HostName)

	}
	// nts - print test
	// for _, i := range items {
	// 	log.Printf("%s", i.Host)
	// }
	// sort the slice
	sort.Sort(ByHost(items)) // the three items req by go (?) nts - see ByHost(items) logic for slice.

	data := make(jet.VarMap)
	data.Set("items", items)

	err := helpers.RenderPage(w, r, "schedule", data, nil)
	if err != nil {
		printTemplateError(w, err)
	}
}
