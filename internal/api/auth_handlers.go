package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"serwer-plikow/internal/auth"
	"serwer-plikow/internal/database"
	"time"

	"github.com/google/uuid"
	"github.com/jaevor/go-nanoid"
)

// @Summary      Logs a user in
// @Description  Authenticates a user and establishes a new server-side session. Returns a short-lived access token (containing the unique Session ID claim) and a long-lived refresh token.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        loginRequest   body      LoginRequest  true  "Login Credentials"
// @Success      200            {object}  TokenResponse
// @Failure      400            {string}  string "Invalid request body"
// @Failure      401            {string}  string "Invalid username or password"
// @Failure      429            {string}  string "Too Many Requests - Limit: 10 requests per minute."
// @Failure      500            {string}  string "Internal Server Error"
// @Router       /auth/login [post]
func (s *Server) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if s.config == nil {
		log.Println("CRITICAL PANIC: s.config is nil in LoginHandler!")
		http.Error(w, "Server configuration error", 500)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	user, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if user == nil || !auth.CheckPasswordHash(req.Password, user.PasswordHash) {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	sessionID := uuid.New()

	accessToken, err := auth.GenerateJWT(user, s.config.JWT.Secret, sessionID)
	if err != nil {
		http.Error(w, "Failed to generate access token", http.StatusInternalServerError)
		return
	}

	generateID, err := nanoid.Standard(40)
	if err != nil {
		log.Printf("CRITICAL: Failed to initialize nanoid generator: %v", err)
		http.Error(w, "Internal server error (token generation)", http.StatusInternalServerError)
		return
	}
	refreshToken := generateID()
	expiresAt := time.Now().Add(24 * time.Hour)

	sessionParams := database.CreateSessionParams{
		ID:           sessionID,
		UserID:       user.ID,
		RefreshToken: refreshToken,
		UserAgent:    r.UserAgent(),
		ClientIP:     getClientIP(r),
		ExpiresAt:    expiresAt,
	}

	err = s.store.CreateSession(r.Context(), sessionParams)
	if err != nil {
		log.Printf("ERROR: Failed to create session for user %d: %v", user.ID, err)
		http.Error(w, "Failed to process login session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

// @Summary      Refresh access token
// @Description  Exchanges a valid refresh token for a new access token and a rotated refresh token. The new Access Token retains the original Session ID, ensuring session continuity while rotating credentials.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        refreshTokenRequest   body      RefreshTokenRequest  true  "Refresh Token"
// @Success      200                   {object}  TokenResponse
// @Failure      400                   {string}  string "Invalid request body or missing token"
// @Failure      401                   {string}  string "Invalid or expired refresh token"
// @Failure      429                   {string}  string "Too Many Requests"
// @Failure      500                   {string}  string "Internal Server Error"
// @Router       /auth/refresh [post]
func (s *Server) RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	var req RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.RefreshToken == "" {
		http.Error(w, "Refresh token is required", http.StatusBadRequest)
		return
	}

	var newAccessToken, newRefreshToken string

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		session, user, err := q.GetSessionAndUserByRefreshToken(r.Context(), req.RefreshToken)
		if err != nil {
			return err
		}
		if user == nil {
			return errors.New("invalid or expired refresh token")
		}

		newAccessToken, err = auth.GenerateJWT(user, s.config.JWT.Secret, session.ID)
		if err != nil {
			return err
		}

		generateID, _ := nanoid.Standard(40)
		newRefreshToken = generateID()

		updateParams := database.UpdateSessionParams{
			ID:              session.ID,
			NewRefreshToken: newRefreshToken,
			NewExpiresAt:    time.Now().Add(24 * time.Hour),
		}
		return q.UpdateSessionRefreshToken(r.Context(), updateParams)
	})

	if txErr != nil {
		if txErr.Error() == "invalid or expired refresh token" {
			s.store.DeleteSessionByRefreshToken(r.Context(), req.RefreshToken)
			http.Error(w, txErr.Error(), http.StatusUnauthorized)
		} else {
			log.Printf("ERROR: Refresh token transaction failed: %v", txErr)
			http.Error(w, "Failed to refresh token", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
	})
}
