package handlers

import (
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/pusher/pusher-http-go"
)

// to connect to local pusher client.
func (repo *DBRepo) PusherAuth(w http.ResponseWriter, r *http.Request) {
	userID := repo.App.Session.GetInt(r.Context(), "userID") // from authentication-handlers.go

	u, _ := repo.DB.GetUserById(userID)

	params, _ := io.ReadAll(r.Body)

	presenceData := pusher.MemberData{
		UserID: strconv.Itoa(userID),
		UserInfo: map[string]string{
			"name": u.FirstName,
			"id":   strconv.Itoa(userID),
		},
	}

	response, err := app.WsClient.AuthenticatePresenceChannel(params, presenceData)
	if err != nil {
		log.Println(err)
		return
	}

	w.Header().Set("Content-Type", "application/json") // knows about to get json
	_, _ = w.Write(response)
}

// func (repo *DBRepo) TestPusher(w http.ResponseWriter, r *http.Request) {
// 	// have it send something to the client
// 	data := make(map[string]string)
// 	data["message"] = "Hello, world"
// 	// push something use ws client in app.config
// 	// NTS - not handlers.repo.App because we're in the handlers package.
// 	err := repo.App.WsClient.Trigger("public-channel", "test-event", data) // payload
// 	if err != nil {
// 		log.Println(err)
// 	}
// }
