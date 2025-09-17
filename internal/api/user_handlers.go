package api

import (
	"encoding/json"
	"net/http"

	_ "serwer-plikow/internal/auth"
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
