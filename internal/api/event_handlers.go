package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

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
