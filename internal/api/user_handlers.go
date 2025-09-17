package api

import (
	"encoding/json"
	"log"
	"net/http"

	"serwer-plikow/internal/auth"
	"serwer-plikow/internal/database"
)

// @Summary      Get current user info
// @Description  Retrieves information about the currently authenticated user from their JWT token.
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  auth.AppClaims
// @Failure      401  {string}  string "Unauthorized"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /me [get]
func (s *Server) GetCurrentUserHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Could not retrieve user from token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(claims)
}

type StorageUsageResponse struct {
	UsedBytes  int64 `json:"used_bytes"`
	QuotaBytes int64 `json:"quota_bytes"`
}

// @Summary      Get storage usage
// @Description  Retrieves the current storage usage and quota for the authenticated user.
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  StorageUsageResponse
// @Failure      401  {string}  string "Unauthorized"
// @Failure      404  {string}  string "User not found"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /me/storage [get]
func (s *Server) GetStorageUsageHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	user, err := s.store.GetUserByUsername(r.Context(), claims.Username)
	if err != nil {
		http.Error(w, "Failed to retrieve user data", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	response := StorageUsageResponse{
		UsedBytes:  user.StorageUsedBytes,
		QuotaBytes: user.StorageQuotaBytes,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" example:"password123"`
	NewPassword string `json:"new_password" example:"newStrongPassword456"`
}

// @Summary      Change current user's password
// @Description  Allows the authenticated user to change their own password. The new password must be at least 8 characters long. Upon successful password change, all other active sessions for the user will be terminated for security reasons.
// @Tags         users
// @Accept       json
// @Security     BearerAuth
// @Param        changePasswordRequest  body      ChangePasswordRequest  true  "Old and new password"
// @Success      204                    {null}    nil                    "No Content - Password changed successfully"
// @Failure      400                    {string}  string "Bad Request - New password is weak (less than 8 characters) or empty"
// @Failure      401                    {string}  string "Unauthorized - Old password does not match"
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

	err = s.store.UpdateUserPassword(r.Context(), claims.UserID, newPasswordHash)
	if err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
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
