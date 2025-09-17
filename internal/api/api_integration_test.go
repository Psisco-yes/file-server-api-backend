package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"serwer-plikow/internal/auth"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/models"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

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
	payload := CreateFolderRequest{Name: "Nowy_Folder_Sukces"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
	http.HandlerFunc(testServer.CreateFolderHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code)
	var createdNode models.Node
	err := json.Unmarshal(rr.Body.Bytes(), &createdNode)
	require.NoError(t, err)
	require.Equal(t, "Nowy_Folder_Sukces", createdNode.Name)
}

func TestAPI_CreateFolder_EmptyName(t *testing.T) {
	payload := CreateFolderRequest{Name: "  "}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
	http.HandlerFunc(testServer.CreateFolderHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestAPI_CreateFolder_NameConflict(t *testing.T) {
	folderName := "Folder_Konfliktowy_Final"
	createTestNodeAPI(t, folderName, "folder", nil, testUserClaims.UserID)

	var initialCount int
	err := testServer.store.GetPool().QueryRow(context.Background(),
		"SELECT count(*) FROM nodes WHERE name=$1 AND owner_id=$2 AND parent_id IS NULL",
		folderName, testUserClaims.UserID).Scan(&initialCount)
	require.NoError(t, err)
	require.Equal(t, 1, initialCount, "SETUP FAILED: Node should be in DB before API call")

	payload := CreateFolderRequest{Name: folderName}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
	http.HandlerFunc(testServer.CreateFolderHandler).ServeHTTP(rr, req)

	var finalCount int
	err = testServer.store.GetPool().QueryRow(context.Background(),
		"SELECT count(*) FROM nodes WHERE name=$1 AND owner_id=$2 AND parent_id IS NULL",
		folderName, testUserClaims.UserID).Scan(&finalCount)
	require.NoError(t, err)

	t.Logf("Final count of nodes with name '%s': %d", folderName, finalCount)
	if rr.Code == http.StatusCreated {
		t.Logf("Received unexpected 201 Created. Response body: %s", rr.Body.String())
	}

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

func TestLoginHandler_Integration(t *testing.T) {

	t.Run("successful login", func(t *testing.T) {
		loginReq := LoginRequest{Username: "api_test_user", Password: "password"}
		body, _ := json.Marshal(loginReq)
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
		rr := httptest.NewRecorder()

		http.HandlerFunc(testServer.LoginHandler).ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var res TokenResponse
		err := json.Unmarshal(rr.Body.Bytes(), &res)
		require.NoError(t, err)
		require.NotEmpty(t, res.AccessToken)
		require.NotEmpty(t, res.RefreshToken)

		var sessionCount int
		err = testServer.store.GetPool().QueryRow(context.Background(), "SELECT COUNT(*) FROM sessions WHERE user_id = $1", testUserClaims.UserID).Scan(&sessionCount)
		require.NoError(t, err)
		require.Equal(t, 1, sessionCount, "A session should be created in the database")
	})

	t.Run("invalid password", func(t *testing.T) {
		loginReq := LoginRequest{Username: "api_test_user", Password: "wrong_password"}
		body, _ := json.Marshal(loginReq)
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
		rr := httptest.NewRecorder()

		http.HandlerFunc(testServer.LoginHandler).ServeHTTP(rr, req)

		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})
}

func createTestUserWithPassword(t *testing.T, username, password string) *models.User {
	hashedPassword, err := auth.HashPassword(password)
	require.NoError(t, err)

	var user models.User
	query := `INSERT INTO users (username, password_hash, display_name) VALUES ($1, $2, $3) RETURNING id, username`
	err = testServer.store.GetPool().QueryRow(context.Background(), query, username, hashedPassword, "Test User "+username).Scan(&user.ID, &user.Username)
	require.NoError(t, err)
	return &user
}

func loginUserForTest(t *testing.T, username, password string) TokenResponse {
	loginReq := LoginRequest{Username: username, Password: password}
	body, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	http.HandlerFunc(testServer.LoginHandler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var res TokenResponse
	err := json.Unmarshal(rr.Body.Bytes(), &res)
	require.NoError(t, err)
	return res
}

func TestRefreshTokenHandler_Integration(t *testing.T) {
	username := "user_for_refresh_test"
	password := "strongpassword123"
	createTestUserWithPassword(t, username, password)

	loginResp := loginUserForTest(t, username, password)
	require.NotEmpty(t, loginResp.RefreshToken)

	time.Sleep(1 * time.Second)

	refreshReq := RefreshTokenRequest{RefreshToken: loginResp.RefreshToken}
	body, _ := json.Marshal(refreshReq)
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	http.HandlerFunc(testServer.RefreshTokenHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var firstRefreshResp TokenResponse
	err := json.Unmarshal(rr.Body.Bytes(), &firstRefreshResp)
	require.NoError(t, err)
	require.NotEmpty(t, firstRefreshResp.AccessToken)
	require.NotEmpty(t, firstRefreshResp.RefreshToken)
	require.NotEqual(t, loginResp.RefreshToken, firstRefreshResp.RefreshToken)

	oldRefreshReq := RefreshTokenRequest{RefreshToken: loginResp.RefreshToken}
	body, _ = json.Marshal(oldRefreshReq)
	req = httptest.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	http.HandlerFunc(testServer.RefreshTokenHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestSessionHandlers_Integration(t *testing.T) {
	username := "user_for_session_test"
	password := "password123"
	testUser := createTestUserWithPassword(t, username, password)

	loginUserForTest(t, username, password)
	time.Sleep(10 * time.Millisecond)
	loginResp2 := loginUserForTest(t, username, password)

	reqList := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	reqList.Header.Set("Authorization", "Bearer "+loginResp2.AccessToken)
	rrList := httptest.NewRecorder()

	router := chi.NewRouter()
	router.With(testServer.AuthMiddleware).Get("/api/v1/sessions", testServer.ListSessionsHandler)
	router.ServeHTTP(rrList, reqList)

	require.Equal(t, http.StatusOK, rrList.Code)
	var sessions []models.Session
	err := json.Unmarshal(rrList.Body.Bytes(), &sessions)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	sessionToDeleteID := sessions[1].ID

	urlDelete := fmt.Sprintf("/api/v1/sessions/%s", sessionToDeleteID)
	reqDelete := httptest.NewRequest("DELETE", urlDelete, nil)
	reqDelete.Header.Set("Authorization", "Bearer "+loginResp2.AccessToken)
	rrDelete := httptest.NewRecorder()

	router.With(testServer.AuthMiddleware).Delete("/api/v1/sessions/{sessionId}", testServer.DeleteSessionHandler)
	router.ServeHTTP(rrDelete, reqDelete)

	require.Equal(t, http.StatusNoContent, rrDelete.Code)

	sessionsAfterDelete, err := testServer.store.ListSessionsForUser(context.Background(), testUser.ID)
	require.NoError(t, err)
	require.Len(t, sessionsAfterDelete, 1)

	reqTerminate := httptest.NewRequest("POST", "/api/v1/sessions/terminate_all", nil)
	reqTerminate.Header.Set("Authorization", "Bearer "+loginResp2.AccessToken)
	rrTerminate := httptest.NewRecorder()

	router.With(testServer.AuthMiddleware).Post("/api/v1/sessions/terminate_all", testServer.TerminateAllSessionsHandler)
	router.ServeHTTP(rrTerminate, reqTerminate)

	require.Equal(t, http.StatusNoContent, rrTerminate.Code)

	sessionsAfterTerminate, err := testServer.store.ListSessionsForUser(context.Background(), testUser.ID)
	require.NoError(t, err)
	require.Len(t, sessionsAfterTerminate, 0)
}

func TestShareAndFavorite_Integration(t *testing.T) {
	sharer := createTestUserWithPassword(t, "sharer_user", "password")
	recipient := createTestUserWithPassword(t, "recipient_user", "password")

	sharerLogin := loginUserForTest(t, "sharer_user", "password")
	recipientLogin := loginUserForTest(t, "recipient_user", "password")

	nodeToShare := createTestNodeAPI(t, "plik_do_udostepnienia.txt", "file", nil, sharer.ID)

	var shareID int64

	t.Run("sharer shares a node with recipient", func(t *testing.T) {
		shareReq := ShareRequest{RecipientUsername: recipient.Username, Permissions: "read"}
		body, _ := json.Marshal(shareReq)
		url := fmt.Sprintf("/api/v1/nodes/%s/share", nodeToShare.ID)
		req := httptest.NewRequest("POST", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+sharerLogin.AccessToken)
		rr := httptest.NewRecorder()

		router := chi.NewRouter()
		router.With(testServer.AuthMiddleware).Post("/api/v1/nodes/{nodeId}/share", testServer.ShareNodeHandler)
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusCreated, rr.Code)
		var shareResp ShareResponse
		err := json.Unmarshal(rr.Body.Bytes(), &shareResp)
		require.NoError(t, err)
		require.Equal(t, nodeToShare.ID, shareResp.NodeID)
		require.Equal(t, recipient.ID, shareResp.RecipientID)
		shareID = shareResp.ID
	})

	t.Run("recipient can see the shared node", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/shares/incoming/nodes?sharer_username=%s", sharer.Username)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()

		router := chi.NewRouter()
		router.With(testServer.AuthMiddleware).Get("/api/v1/shares/incoming/nodes", testServer.ListSharedNodesHandler)
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []models.Node
		json.Unmarshal(rr.Body.Bytes(), &nodes)
		require.Len(t, nodes, 1)
		require.Equal(t, nodeToShare.ID, nodes[0].ID)
	})

	t.Run("recipient adds shared node to favorites", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s/favorite", nodeToShare.ID)
		req := httptest.NewRequest("POST", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()

		router := chi.NewRouter()
		router.With(testServer.AuthMiddleware).Post("/api/v1/nodes/{nodeId}/favorite", testServer.AddFavoriteHandler)
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusNoContent, rr.Code)

		favs, err := testServer.store.ListFavorites(context.Background(), recipient.ID, 10, 0)
		require.NoError(t, err)
		require.Len(t, favs, 1)
		require.Equal(t, nodeToShare.ID, favs[0].ID)
	})

	t.Run("sharer revokes the share", func(t *testing.T) {
		require.NotZero(t, shareID, "Share ID should have been set in the first sub-test")

		url := fmt.Sprintf("/api/v1/shares/%d", shareID)
		req := httptest.NewRequest("DELETE", url, nil)
		req.Header.Set("Authorization", "Bearer "+sharerLogin.AccessToken)
		rr := httptest.NewRecorder()

		router := chi.NewRouter()
		router.With(testServer.AuthMiddleware).Delete("/api/v1/shares/{shareId}", testServer.DeleteShareHandler)
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusNoContent, rr.Code)
	})

	t.Run("recipient can no longer access the node", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s/download", nodeToShare.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()

		router := chi.NewRouter()
		router.With(testServer.AuthMiddleware).Get("/api/v1/nodes/{nodeId}/download", testServer.DownloadFileHandler)
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestTrashHandlers_Integration(t *testing.T) {
	username := "user_for_trash_test"
	password := "password123"
	testUser := createTestUserWithPassword(t, username, password)
	loginResp := loginUserForTest(t, username, password)

	nodeToTrash := createTestNodeAPI(t, "plik_do_kosza.txt", "file", nil, testUser.ID)
	nodeToKeep := createTestNodeAPI(t, "plik_zostaje.txt", "file", nil, testUser.ID)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Delete("/api/v1/nodes/{nodeId}", testServer.DeleteNodeHandler)
	router.Get("/api/v1/trash", testServer.ListTrashHandler)
	router.Post("/api/v1/nodes/{nodeId}/restore", testServer.RestoreNodeHandler)
	router.Delete("/api/v1/trash/purge", testServer.PurgeTrashHandler)

	t.Run("move node to trash", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s", nodeToTrash.ID)
		req := httptest.NewRequest("DELETE", url, nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusNoContent, rr.Code)
	})

	t.Run("list trash contents", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/trash", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []models.Node
		json.Unmarshal(rr.Body.Bytes(), &nodes)
		require.Len(t, nodes, 1)
		require.Equal(t, nodeToTrash.ID, nodes[0].ID)
	})

	t.Run("restore node from trash", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s/restore", nodeToTrash.ID)
		req := httptest.NewRequest("POST", url, nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)

		reqList := httptest.NewRequest("GET", "/api/v1/trash", nil)
		reqList.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrList := httptest.NewRecorder()
		router.ServeHTTP(rrList, reqList)
		var nodes []models.Node
		json.Unmarshal(rrList.Body.Bytes(), &nodes)
		require.Len(t, nodes, 0, "Trash should be empty after restore")
	})

	t.Run("purge trash", func(t *testing.T) {
		urlTrash1 := fmt.Sprintf("/api/v1/nodes/%s", nodeToTrash.ID)
		reqTrash1 := httptest.NewRequest("DELETE", urlTrash1, nil)
		reqTrash1.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		router.ServeHTTP(httptest.NewRecorder(), reqTrash1)

		urlTrash2 := fmt.Sprintf("/api/v1/nodes/%s", nodeToKeep.ID)
		reqTrash2 := httptest.NewRequest("DELETE", urlTrash2, nil)
		reqTrash2.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		router.ServeHTTP(httptest.NewRecorder(), reqTrash2)

		reqPurge := httptest.NewRequest("DELETE", "/api/v1/trash/purge", nil)
		reqPurge.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrPurge := httptest.NewRecorder()
		router.ServeHTTP(rrPurge, reqPurge)

		require.Equal(t, http.StatusNoContent, rrPurge.Code)

		var count int
		err := testServer.store.GetPool().QueryRow(context.Background(), "SELECT COUNT(*) FROM nodes WHERE owner_id = $1", testUser.ID).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count, "All nodes for the user should be permanently deleted")
	})
}

func TestGetEventsHandler_Integration(t *testing.T) {
	username := "user_for_events_test"
	password := "password123"
	createTestUserWithPassword(t, username, password)
	loginResp := loginUserForTest(t, username, password)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Post("/api/v1/nodes/folder", testServer.CreateFolderHandler)
	router.Get("/api/v1/events", testServer.GetEventsHandler)

	createFolderReq := CreateFolderRequest{Name: "EventTestFolder"}
	body, _ := json.Marshal(createFolderReq)
	reqCreate := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
	reqCreate.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)

	rrCreate := httptest.NewRecorder()
	router.ServeHTTP(rrCreate, reqCreate)
	require.Equal(t, http.StatusCreated, rrCreate.Code, "Creating a folder to generate an event should succeed")

	reqAll := httptest.NewRequest("GET", "/api/v1/events?since=0", nil)
	reqAll.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	rrAll := httptest.NewRecorder()
	router.ServeHTTP(rrAll, reqAll)

	require.Equal(t, http.StatusOK, rrAll.Code)
	var events []database.Event
	err := json.Unmarshal(rrAll.Body.Bytes(), &events)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 1, "At least one event should be returned")

	lastEventID := events[len(events)-1].ID

	urlSince := fmt.Sprintf("/api/v1/events?since=%d", lastEventID)
	reqSince := httptest.NewRequest("GET", urlSince, nil)
	reqSince.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	rrSince := httptest.NewRecorder()
	router.ServeHTTP(rrSince, reqSince)

	require.Equal(t, http.StatusOK, rrSince.Code)
	var noEvents []database.Event
	err = json.Unmarshal(rrSince.Body.Bytes(), &noEvents)
	require.NoError(t, err)
	require.Len(t, noEvents, 0, "There should be no new events since the last known ID")
}
