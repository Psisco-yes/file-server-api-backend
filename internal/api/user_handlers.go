package api

import (
	"encoding/json"
	"log"
	"net/http"

	"serwer-plikow/internal/auth"
	"serwer-plikow/internal/database"
	_ "serwer-plikow/internal/models"
)

// @Summary      Get current user info
// @Description  Retrieves full information about the currently authenticated user, including their storage usage and quota.
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  models.User
// @Failure      401  {string}  string "Unauthorized"
// @Failure      429  {string}  string "Too Many Requests"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /me [get]
func (s *Server) GetCurrentUserHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Could not retrieve user from token", http.StatusInternalServerError)
		return
	}

	user, err := s.store.GetUserByID(r.Context(), claims.UserID)
	if err != nil || user == nil {
		http.Error(w, "Failed to retrieve current user data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// @Summary      Change current user's password
// @Description  Allows the authenticated user to change their own password. The new password must be at least 8 characters long. Upon successful password change, all other active sessions for the user will be terminated for security reasons.
// @Tags         users
// @Accept       json
// @Security     BearerAuth
// @Param        changePasswordRequest  body      ChangePasswordRequest  true  "Old and new password"
// @Success      204                    {object}  nil    "No Content - Password changed successfully"
// @Failure      400                    {string}  string "Bad Request - New password is weak (less than 8 characters) or empty"
// @Failure      401                    {string}  string "Unauthorized - Old password does not match"
// @Failure      429                    {string}  string "Too Many Requests"
// @Failure      500                    {string}  string "Internal Server Error"
// @Router       /me/password [patch]
func (s *Server) ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.NewPassword) < 8 {
		http.Error(w, "New password must be at least 8 characters long", http.StatusBadRequest)
		return
	}

	user, err := s.store.GetUserByUsername(r.Context(), claims.Username)
	if err != nil || user == nil {
		http.Error(w, "Could not find user", http.StatusInternalServerError)
		return
	}

	if !auth.CheckPasswordHash(req.OldPassword, user.PasswordHash) {
		http.Error(w, "Old password does not match", http.StatusUnauthorized)
		return
	}

	newPasswordHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		http.Error(w, "Failed to hash new password", http.StatusInternalServerError)
		return
	}

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		if err := q.UpdateUserPassword(r.Context(), claims.UserID, newPasswordHash); err != nil {
			return err
		}
		return q.DeleteAllSessionsForUser(r.Context(), claims.UserID)
	})

	if txErr != nil {
		log.Printf("ERROR: Failed to update password and terminate sessions in transaction: %v", txErr)
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Update current user's profile
// @Description  Updates properties of the currently authenticated user, such as their display name.
// @Tags         users
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        updateRequest  body      UpdateMeRequest  true  "Fields to update"
// @Success      200            {object}  models.User
// @Failure      400            {string}  string "Bad Request"
// @Failure      401            {string}  string "Unauthorized"
// @Failure      429            {string}  string "Too Many Requests"
// @Failure      500            {string}  string "Internal Server Error"
// @Router       /me [patch]
func (s *Server) UpdateCurrentUserHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	var req UpdateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	params := database.UpdateUserParams{
		ID:          claims.UserID,
		DisplayName: req.DisplayName,
	}

	err := s.store.UpdateUser(r.Context(), params)
	if err != nil {
		http.Error(w, "Failed to update user profile", http.StatusInternalServerError)
		return
	}

	updatedUser, err := s.store.GetUserByID(r.Context(), claims.UserID)
	if err != nil || updatedUser == nil {
		http.Error(w, "Failed to retrieve updated user profile", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedUser)
}
