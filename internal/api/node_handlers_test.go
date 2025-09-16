package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/models"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

// Funkcja pomocnicza do tworzenia węzłów w testach API
func createTestNodeAPI(t *testing.T, name, nodeType string, parentID *string, ownerID int64) *models.Node {
	id, err := testServer.generateUniqueID(context.Background())
	require.NoError(t, err)

	var sizeBytes *int64
	if nodeType == "file" {
		var s int64 = 1234
		sizeBytes = &s
	}

	params := database.CreateNodeParams{
		ID:        id,
		OwnerID:   ownerID,
		ParentID:  parentID,
		Name:      name,
		NodeType:  nodeType,
		SizeBytes: sizeBytes,
	}
	node, err := testServer.store.CreateNode(context.Background(), params)
	require.NoError(t, err)
	return node
}

func TestAPI_CreateFolder_Success(t *testing.T) {
	// Arrange
	payload := CreateFolderRequest{Name: "Nowy_Folder_Sukces"} // Unikalna nazwa dla tego testu
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	// Act
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
	http.HandlerFunc(testServer.CreateFolderHandler).ServeHTTP(rr, req)

	// Assert
	require.Equal(t, http.StatusCreated, rr.Code)
	var createdNode models.Node
	err := json.Unmarshal(rr.Body.Bytes(), &createdNode)
	require.NoError(t, err)
	require.Equal(t, "Nowy_Folder_Sukces", createdNode.Name)
}

func TestAPI_CreateFolder_EmptyName(t *testing.T) {
	// Arrange
	payload := CreateFolderRequest{Name: "  "}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	// Act
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
	http.HandlerFunc(testServer.CreateFolderHandler).ServeHTTP(rr, req)

	// Assert
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestAPI_CreateFolder_NameConflict(t *testing.T) {
	// Arrange: Krok 1
	folderName := "Folder_Konfliktowy_Final"
	createTestNodeAPI(t, folderName, "folder", nil, testUserClaims.UserID)

	// Sprawdzenie #1 - Czy na pewno jest w bazie?
	var initialCount int
	err := testServer.store.GetPool().QueryRow(context.Background(),
		"SELECT count(*) FROM nodes WHERE name=$1 AND owner_id=$2 AND parent_id IS NULL",
		folderName, testUserClaims.UserID).Scan(&initialCount)
	require.NoError(t, err)
	require.Equal(t, 1, initialCount, "SETUP FAILED: Node should be in DB before API call")

	// Arrange: Krok 2
	payload := CreateFolderRequest{Name: folderName}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	// Act
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
	http.HandlerFunc(testServer.CreateFolderHandler).ServeHTTP(rr, req)

	// Sprawdzenie #2 - Ile jest teraz takich folderów w bazie?
	var finalCount int
	err = testServer.store.GetPool().QueryRow(context.Background(),
		"SELECT count(*) FROM nodes WHERE name=$1 AND owner_id=$2 AND parent_id IS NULL",
		folderName, testUserClaims.UserID).Scan(&finalCount)
	require.NoError(t, err)

	// Logujemy, żeby zobaczyć, co się stało
	t.Logf("Final count of nodes with name '%s': %d", folderName, finalCount)
	if rr.Code == http.StatusCreated {
		t.Logf("Received unexpected 201 Created. Response body: %s", rr.Body.String())
	}

	// Assert
	require.Equal(t, 1, finalCount, "The number of nodes with this name should not increase")
	require.Equal(t, http.StatusConflict, rr.Code, "Expected a conflict when creating a folder with a duplicate name")
}

func TestListNodesHandler(t *testing.T) {
	parentFolder := createTestNodeAPI(t, "Parent Folder", "folder", nil, testUserClaims.UserID)
	childFile := createTestNodeAPI(t, "Child File", "file", &parentFolder.ID, testUserClaims.UserID)

	t.Run("should list root directory", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/nodes", nil)
		rr := httptest.NewRecorder()

		req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
		http.HandlerFunc(testServer.ListNodesHandler).ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []models.Node
		err := json.Unmarshal(rr.Body.Bytes(), &nodes)
		require.NoError(t, err)

		found := false
		for _, node := range nodes {
			if node.ID == parentFolder.ID {
				found = true
				break
			}
		}
		require.True(t, found, "Expected to find the created parent folder in the root listing")
	})

	t.Run("should list subdirectory content", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes?parent_id=%s", parentFolder.ID)
		req := httptest.NewRequest("GET", url, nil)
		rr := httptest.NewRecorder()

		req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
		http.HandlerFunc(testServer.ListNodesHandler).ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []models.Node
		err := json.Unmarshal(rr.Body.Bytes(), &nodes)
		require.NoError(t, err)
		require.Len(t, nodes, 1)
		require.Equal(t, childFile.Name, nodes[0].Name)
	})
}

func TestUpdateNodeHandler_Rename(t *testing.T) {
	nodeToRename := createTestNodeAPI(t, "Stara Nazwa", "folder", nil, testUserClaims.UserID)

	payload := UpdateNodeRequest{Name: new(string)}
	*payload.Name = "Nowa Nazwa"
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("/api/v1/nodes/%s", nodeToRename.ID)
	req := httptest.NewRequest("PATCH", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testUserToken)
	rr := httptest.NewRecorder()

	router := chi.NewRouter()
	router.With(testServer.AuthMiddleware).Patch("/api/v1/nodes/{nodeId}", testServer.UpdateNodeHandler)
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	updatedNode, err := testServer.store.GetNodeByID(context.Background(), nodeToRename.ID, testUserClaims.UserID)
	require.NoError(t, err)
	require.Equal(t, "Nowa Nazwa", updatedNode.Name)
}

func TestUpdateNodeHandler_Move(t *testing.T) {
	folder1 := createTestNodeAPI(t, "Folder 1", "folder", nil, testUserClaims.UserID)
	folder2 := createTestNodeAPI(t, "Folder 2", "folder", nil, testUserClaims.UserID)
	nodeToMove := createTestNodeAPI(t, "Plik do przeniesienia", "file", &folder1.ID, testUserClaims.UserID)

	payload := UpdateNodeRequest{ParentID: &folder2.ID}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("/api/v1/nodes/%s", nodeToMove.ID)
	req := httptest.NewRequest("PATCH", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testUserToken)
	rr := httptest.NewRecorder()

	router := chi.NewRouter()
	router.With(testServer.AuthMiddleware).Patch("/api/v1/nodes/{nodeId}", testServer.UpdateNodeHandler)
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	updatedNode, err := testServer.store.GetNodeByID(context.Background(), nodeToMove.ID, testUserClaims.UserID)
	require.NoError(t, err)
	require.NotNil(t, updatedNode.ParentID)
	require.Equal(t, folder2.ID, *updatedNode.ParentID)
}

func TestDeleteNodeHandler(t *testing.T) {
	nodeToDelete := createTestNodeAPI(t, "Do Kosza", "file", nil, testUserClaims.UserID)

	url := fmt.Sprintf("/api/v1/nodes/%s", nodeToDelete.ID)
	req := httptest.NewRequest("DELETE", url, nil)
	req.Header.Set("Authorization", "Bearer "+testUserToken)
	rr := httptest.NewRecorder()

	router := chi.NewRouter()
	router.With(testServer.AuthMiddleware).Delete("/api/v1/nodes/{nodeId}", testServer.DeleteNodeHandler)
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)

	trashedNode, err := testServer.store.GetNodeByID(context.Background(), nodeToDelete.ID, testUserClaims.UserID)
	require.NoError(t, err)
	require.Nil(t, trashedNode)

	var deletedAt *time.Time
	err = testServer.store.GetPool().QueryRow(context.Background(), "SELECT deleted_at FROM nodes WHERE id=$1", nodeToDelete.ID).Scan(&deletedAt)
	require.NoError(t, err)
	require.NotNil(t, deletedAt)
}

func TestUploadFileHandler(t *testing.T) {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "testfile.txt")
	require.NoError(t, err)
	fileContent := "to jest zawartość pliku"
	part.Write([]byte(fileContent))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/v1/nodes/file", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
	http.HandlerFunc(testServer.UploadFileHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code)

	var createdNodes []models.Node
	err = json.Unmarshal(rr.Body.Bytes(), &createdNodes)
	require.NoError(t, err)
	require.Len(t, createdNodes, 1)

	uploadedNode := createdNodes[0]
	require.Equal(t, "testfile.txt", uploadedNode.Name)
	require.Equal(t, int64(len(fileContent)), *uploadedNode.SizeBytes)

	_, err = testServer.storage.Get(uploadedNode.ID)
	require.NoError(t, err, "File should exist in storage after upload")
}

func TestDownloadFileHandler(t *testing.T) {
	fileNode := createTestNodeAPI(t, "plik_do_pobrania.txt", "file", nil, testUserClaims.UserID)
	fileContent := "tajna zawartość"
	err := testServer.storage.Save(fileNode.ID, strings.NewReader(fileContent))
	require.NoError(t, err)

	url := fmt.Sprintf("/api/v1/nodes/%s/download", fileNode.ID)
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+testUserToken)
	rr := httptest.NewRecorder()

	router := chi.NewRouter()
	router.With(testServer.AuthMiddleware).Get("/api/v1/nodes/{nodeId}/download", testServer.DownloadFileHandler)
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, fileContent, rr.Body.String())
	require.Contains(t, rr.Header().Get("Content-Disposition"), "attachment; filename=\"plik_do_pobrania.txt\"")
}
