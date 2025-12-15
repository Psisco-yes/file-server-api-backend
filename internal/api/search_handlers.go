package api

import (
	"encoding/json"
	"net/http"
	_ "serwer-plikow/internal/models"
)

// @Summary      Search for files and folders
// @Description  Searches for nodes (files and folders) by name across the user's own space and all items shared with them.
// @Tags         search
// @Produce      json
// @Security     BearerAuth
// @Param        q      query     string  true  "Search query string"
// @Param        limit  query     int     false "Number of items to return" default(100)
// @Param        offset query     int     false "Offset for pagination" default(0)
// @Param        sortBy     query     string  false  "Sort by field (name, size, modifiedAt)" enums(name,size,modifiedAt)
// @Param        sortOrder  query     string  false  "Sort order (asc, desc)" enums(asc,desc)
// @Success      200    {array}   models.RichNode
// @Failure      400    {string}  string "Bad Request - Missing query"
// @Failure      401    {string}  string "Unauthorized"
// @Failure      500    {string}  string "Internal Server Error"
// @Router       /search [get]
func (s *Server) SearchHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	limit, offset := parsePagination(r)
	sortBy := r.URL.Query().Get("sortBy")
	sortOrder := r.URL.Query().Get("sortOrder")

	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Search query 'q' is required", http.StatusBadRequest)
		return
	}

	nodes, err := s.store.SearchRichNodes(r.Context(), claims.UserID, query, limit, offset, sortBy, sortOrder)
	if err != nil {
		http.Error(w, "Failed to perform search", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}
