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

type LoginRequest struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"password123"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxLCJ1c2VybmFtZSI6ImFkbWluIiwiZXhwIjoxNjE2NDI2NzY2fQ...."`
	RefreshToken string `json:"refresh_token" example:"V1StGXR8_Z5jdHi6B-myT78q_Z5jdHi6B-myT78q"`
}

// @Summary      Logs a user in
// @Description  Authenticates a user and returns a short-lived access token and a long-lived refresh token.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        loginRequest   body      LoginRequest  true  "Login Credentials"
// @Success      200            {object}  TokenResponse
// @Failure      400            {string}  string "Invalid request body"
// @Failure      401            {string}  string "Invalid username or password"
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

	accessToken, err := auth.GenerateJWT(user, s.config.JWT.Secret)
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
		ID:           uuid.New(),
		UserID:       user.ID,
		RefreshToken: refreshToken,
		UserAgent:    r.UserAgent(),
		ClientIP:     r.RemoteAddr,
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

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" example:"V1StGXR8_Z5jdHi6B-myT78q_Z5jdHi6B-myT78q"`
}

// @Summary      Refresh access token
// @Description  Provides a new short-lived access token and a new refresh token in exchange for a valid, non-expired refresh token. Implements refresh token rotation.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        refreshTokenRequest   body      RefreshTokenRequest  true  "Refresh Token"
// @Success      200                   {object}  TokenResponse
// @Failure      400                   {string}  string "Invalid request body or missing token"
// @Failure      401                   {string}  string "Invalid or expired refresh token"
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
		user, err := q.GetUserByRefreshToken(r.Context(), req.RefreshToken)
		if err != nil {
			return err
		}
		if user == nil {
			return errors.New("invalid or expired refresh token")
		}

		if err := q.DeleteSessionByRefreshToken(r.Context(), req.RefreshToken); err != nil {
			return err
		}

		newAccessToken, err = auth.GenerateJWT(user, s.config.JWT.Secret)
		if err != nil {
			return err
		}

		generateID, _ := nanoid.Standard(40)
		newRefreshToken = generateID()
		sessionParams := database.CreateSessionParams{
			ID:           uuid.New(),
			UserID:       user.ID,
			RefreshToken: newRefreshToken,
			UserAgent:    r.UserAgent(),
			ClientIP:     r.RemoteAddr,
			ExpiresAt:    time.Now().Add(24 * time.Hour),
		}
		return q.CreateSession(r.Context(), sessionParams)
	})

	if txErr != nil {
		if txErr.Error() == "invalid or expired refresh token" {
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
