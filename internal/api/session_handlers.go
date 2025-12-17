package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	_ "serwer-plikow/internal/models"
)

// @Summary      List active sessions
// @Description  Gets a list of all active sessions for the currently authenticated user. This can be used to display a list of devices where the user is logged in.
// @Tags         sessions
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   models.Session
// @Failure      401  {string}  string "Unauthorized"
// @Failure      429  {string}  string "Too Many Requests"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /sessions [get]
func (s *Server) ListSessionsHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	sessions, err := s.store.ListSessionsForUser(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "Failed to retrieve sessions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// @Summary      Terminate a specific session
// @Description  Terminates (logs out) a specific session by its ID. A user can only terminate their own sessions. This will also close any active WebSocket connections associated with the terminated user.
// @Tags         sessions
// @Security     BearerAuth
// @Param        sessionId  path      string  true  "ID of the session to terminate" format(uuid)
// @Success      204        {object}  nil     "No Content"
// @Failure      400        {string}  string "Bad Request - Invalid session ID format"
// @Failure      401        {string}  string "Unauthorized"
// @Failure      404        {string}  string "Not Found - Session does not exist or does not belong to the user"
// @Failure      429        {string}  string "Too Many Requests"
// @Failure      500        {string}  string "Internal Server Error"
// @Router       /sessions/{sessionId} [delete]
func (s *Server) DeleteSessionHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	sessionIDStr := chi.URLParam(r, "sessionId")

	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID format", http.StatusBadRequest)
		return
	}

	success, err := s.store.DeleteSessionByID(r.Context(), sessionID, claims.UserID)
	if err != nil {
		http.Error(w, "Failed to delete session", http.StatusInternalServerError)
		return
	}

	if success {
		s.wsHub.DisconnectUser <- claims.UserID
	}

	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Terminate all sessions (Log out everywhere)
// @Description  Terminates all active sessions for the currently authenticated user, effectively logging them out from all devices. This will also close all active WebSocket connections for the user.
// @Tags         sessions
// @Security     BearerAuth
// @Success      204  {object}  nil    "No Content"
// @Failure      401  {string}  string "Unauthorized"
// @Failure      429  {string}  string "Too Many Requests"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /sessions/terminate_all [post]
func (s *Server) TerminateAllSessionsHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	err := s.store.DeleteAllSessionsForUser(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "Failed to terminate all sessions", http.StatusInternalServerError)
		return
	}

	s.wsHub.DisconnectUser <- claims.UserID

	w.WriteHeader(http.StatusNoContent)
}
