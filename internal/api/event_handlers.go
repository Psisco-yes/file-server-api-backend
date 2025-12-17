package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// @Summary      Get new events
// @Description  Retrieves a list of events that have occurred since a given event ID. Used for client-side cache synchronization.
// @Tags         events
// @Produce      json
// @Security     BearerAuth
// @Param        since  query     int  false  "The ID of the last event received. Omit or use 0 to get all events."
// @Success      200    {array}   EventResponse
// @Failure      400    {string}  string "Bad Request"
// @Failure      401    {string}  string "Unauthorized"
// @Failure      429    {string}  string "Too Many Requests"
// @Failure      500    {string}  string "Internal Server Error"
// @Router       /events [get]
func (s *Server) GetEventsHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		sinceStr = "0"
	}

	sinceID, err := strconv.ParseInt(sinceStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid 'since' parameter, must be a number", http.StatusBadRequest)
		return
	}

	events, err := s.store.GetEventsSince(r.Context(), claims.UserID, sinceID)
	if err != nil {
		http.Error(w, "Failed to retrieve events", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// @Summary      Get latest event ID
// @Description  Retrieves the ID of the most recent event for the authenticated user. This is useful for clients to get an initial synchronization point after building their cache from scratch.
// @Tags         events
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  LatestEventResponse
// @Failure      401  {string}  string "Unauthorized"
// @Failure      429  {string}  string "Too Many Requests"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /events/latest [get]
func (s *Server) GetLatestEventHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	latestID, err := s.store.GetLatestEventID(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "Failed to retrieve latest event ID", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LatestEventResponse{LatestEventID: latestID})
}
