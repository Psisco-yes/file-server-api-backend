package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/go-chi/httprate"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func createTestNodeAPI(t *testing.T, name, nodeType string, parentID *string, ownerID int64) (*models.Node, error) {
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
	return node, err
}

func TestAPI_CreateFolder_Success(t *testing.T) {
	payload := CreateFolderRequest{Name: "Nowy_Folder_Sukces"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUserClaims))
	http.HandlerFunc(testServer.CreateFolderHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code)
	var createdNode models.RichNode
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
	folderName := "Folder_Konfliktowy_" + uuid.NewString()
	createTestNodeAPI(t, folderName, "folder", nil, testUserClaims.UserID)

	var initialCount int
	err := testServer.store.GetPool().QueryRow(context.Background(),
		"SELECT count(*) FROM nodes WHERE name=$1 AND owner_id=$2 AND parent_id IS NULL AND deleted_at IS NULL",
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

func TestListNodesHandler_Sorting(t *testing.T) {
	user := createTestUserWithPassword(t, "user_multi_sort", "password")
	loginResp := loginUserForTest(t, "user_multi_sort", "password")

	_, err := testServer.store.GetPool().Exec(context.Background(), "DELETE FROM nodes WHERE owner_id = $1 AND parent_id IS NULL", user.ID)
	require.NoError(t, err)

	nodeA, _ := createTestNodeAPI(t, "A_Folder", "folder", nil, user.ID)
	nodeB, _ := createTestNodeAPI(t, "B_Folder", "folder", nil, user.ID)
	nodeX, _ := createTestNodeAPI(t, "X_File.txt", "file", nil, user.ID)
	nodeZ, _ := createTestNodeAPI(t, "Z_File.txt", "file", nil, user.ID)

	var size1 int64 = 100
	var size2 int64 = 500
	_, err = testServer.store.GetPool().Exec(context.Background(), "UPDATE nodes SET size_bytes = $1, modified_at = $2 WHERE id = $3", size1, time.Now().Add(-2*time.Hour), nodeA.ID)
	require.NoError(t, err)
	_, err = testServer.store.GetPool().Exec(context.Background(), "UPDATE nodes SET size_bytes = $1, modified_at = $2 WHERE id = $3", size1, time.Now().Add(-1*time.Hour), nodeB.ID)
	require.NoError(t, err)
	_, err = testServer.store.GetPool().Exec(context.Background(), "UPDATE nodes SET size_bytes = $1, modified_at = $2 WHERE id = $3", size1, time.Now().Add(-30*time.Minute), nodeX.ID)
	require.NoError(t, err)
	_, err = testServer.store.GetPool().Exec(context.Background(), "UPDATE nodes SET size_bytes = $1, modified_at = $2 WHERE id = $3", size2, time.Now(), nodeZ.ID)
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/nodes", testServer.ListNodesHandler)

	runSortTest := func(t *testing.T, url string, expectedOrder []string) {
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []*models.RichNode
		json.Unmarshal(rr.Body.Bytes(), &nodes)

		require.Len(t, nodes, len(expectedOrder))
		var actualOrder []string
		for _, n := range nodes {
			actualOrder = append(actualOrder, n.Name)
		}
		require.Equal(t, expectedOrder, actualOrder, "Sort order is incorrect")
	}

	t.Run("default sort (type desc, name asc)", func(t *testing.T) {
		runSortTest(t, "/api/v1/nodes", []string{"A_Folder", "B_Folder", "X_File.txt", "Z_File.txt"})
	})

	t.Run("single column sort (name descending)", func(t *testing.T) {
		runSortTest(t, "/api/v1/nodes?sort=-name", []string{"Z_File.txt", "X_File.txt", "B_Folder", "A_Folder"})
	})

	t.Run("multi-column sort (type asc, name desc)", func(t *testing.T) {
		runSortTest(t, "/api/v1/nodes?sort=-type,-name", []string{"Z_File.txt", "X_File.txt", "B_Folder", "A_Folder"})
	})

	t.Run("multi-column sort (type desc, name desc)", func(t *testing.T) {
		runSortTest(t, "/api/v1/nodes?sort=type,-name", []string{"B_Folder", "A_Folder", "Z_File.txt", "X_File.txt"})
	})

	t.Run("sort by invalid column falls back to default", func(t *testing.T) {
		runSortTest(t, "/api/v1/nodes?sort=drop-tables", []string{"A_Folder", "B_Folder", "X_File.txt", "Z_File.txt"})
	})
}

func TestUpdateNodeHandler_Rename(t *testing.T) {
	t.Run("rename successfully", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_rename_success", "password")
		loginResp := loginUserForTest(t, "user_rename_success", "password")
		nodeToRename, err := createTestNodeAPI(t, "Stara Nazwa", "folder", nil, user.ID)
		require.NoError(t, err)

		payload := UpdateNodeRequest{Name: new(string)}
		*payload.Name = "Nowa Nazwa"
		body, _ := json.Marshal(payload)
		url := fmt.Sprintf("/api/v1/nodes/%s", nodeToRename.ID)
		req := httptest.NewRequest("PATCH", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()

		router := chi.NewRouter()
		router.With(testServer.AuthMiddleware).Patch("/api/v1/nodes/{nodeId}", testServer.UpdateNodeHandler)
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		updatedNode, err := testServer.store.GetNodeByID(context.Background(), nodeToRename.ID, user.ID)
		require.NoError(t, err)
		require.Equal(t, "Nowa Nazwa", updatedNode.Name)
	})

	t.Run("rename to a conflicting name", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_rename_conflict", "password")
		loginResp := loginUserForTest(t, "user_rename_conflict", "password")

		nodeToRename, _ := createTestNodeAPI(t, "Original Name", "file", nil, user.ID)
		createTestNodeAPI(t, "Existing Name", "file", nil, user.ID)

		payload := UpdateNodeRequest{Name: new(string)}
		*payload.Name = "Existing Name"
		body, _ := json.Marshal(payload)
		url := fmt.Sprintf("/api/v1/nodes/%s", nodeToRename.ID)
		req := httptest.NewRequest("PATCH", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()

		router := chi.NewRouter()
		router.Use(testServer.AuthMiddleware)
		router.Patch("/api/v1/nodes/{nodeId}", testServer.UpdateNodeHandler)
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusConflict, rr.Code)

		nodeAfter, err := testServer.store.GetNodeByID(context.Background(), nodeToRename.ID, user.ID)
		require.NoError(t, err)
		require.Equal(t, "Original Name", nodeAfter.Name)
	})
}

func TestUpdateNodeHandler_Move(t *testing.T) {
	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Patch("/api/v1/nodes/{nodeId}", testServer.UpdateNodeHandler)

	t.Run("move file between folders successfully", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_move_success", "password")
		loginResp := loginUserForTest(t, "user_move_success", "password")

		folder1, _ := createTestNodeAPI(t, "Folder 1", "folder", nil, user.ID)
		folder2, _ := createTestNodeAPI(t, "Folder 2", "folder", nil, user.ID)
		nodeToMove, _ := createTestNodeAPI(t, "Plik do przeniesienia", "file", &folder1.ID, user.ID)

		payload := UpdateNodeRequest{ParentID: &folder2.ID}
		body, _ := json.Marshal(payload)
		url := fmt.Sprintf("/api/v1/nodes/%s", nodeToMove.ID)
		req := httptest.NewRequest("PATCH", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		updatedNode, err := testServer.store.GetNodeByID(context.Background(), nodeToMove.ID, user.ID)
		require.NoError(t, err)
		require.NotNil(t, updatedNode.ParentID)
		require.Equal(t, folder2.ID, *updatedNode.ParentID)
	})

	t.Run("fail to move folder into its own child (circular move)", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_move_circular", "password")
		loginResp := loginUserForTest(t, "user_move_circular", "password")

		parent, _ := createTestNodeAPI(t, "Parent", "folder", nil, user.ID)
		child, _ := createTestNodeAPI(t, "Child", "folder", &parent.ID, user.ID)

		payload := UpdateNodeRequest{ParentID: &child.ID}
		body, _ := json.Marshal(payload)
		url := fmt.Sprintf("/api/v1/nodes/%s", parent.ID)
		req := httptest.NewRequest("PATCH", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), "Cannot move a folder into one of its subfolders")
	})

	t.Run("fail to move folder into itself", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_move_self", "password")
		loginResp := loginUserForTest(t, "user_move_self", "password")

		folder, _ := createTestNodeAPI(t, "SomeFolder", "folder", nil, user.ID)

		payload := UpdateNodeRequest{ParentID: &folder.ID}
		body, _ := json.Marshal(payload)
		url := fmt.Sprintf("/api/v1/nodes/%s", folder.ID)
		req := httptest.NewRequest("PATCH", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), "Cannot move a folder into itself")
	})

	t.Run("fail to move node to an inaccessible folder", func(t *testing.T) {
		userA := createTestUserWithPassword(t, "user_move_owner_a", "password")
		userB := createTestUserWithPassword(t, "user_move_owner_b", "password")
		loginA := loginUserForTest(t, "user_move_owner_a", "password")

		nodeToMove, _ := createTestNodeAPI(t, "FileOfA", "file", nil, userA.ID)
		targetFolderB, _ := createTestNodeAPI(t, "FolderOfB", "folder", nil, userB.ID)

		payload := UpdateNodeRequest{ParentID: &targetFolderB.ID}
		body, _ := json.Marshal(payload)
		url := fmt.Sprintf("/api/v1/nodes/%s", nodeToMove.ID)
		req := httptest.NewRequest("PATCH", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginA.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.Contains(t, rr.Body.String(), "Target folder not found or access denied")
	})
}

func TestDeleteNodeHandler(t *testing.T) {
	nodeToDelete, err := createTestNodeAPI(t, "Do Kosza", "file", nil, testUserClaims.UserID)
	require.NoError(t, err)

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

	var createdNodes []models.RichNode
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
	fileNode, err := createTestNodeAPI(t, "plik_do_pobrania.txt", "file", nil, testUserClaims.UserID)
	require.NoError(t, err)
	fileContent := "tajna zawartość"
	err = testServer.storage.Save(fileNode.ID, strings.NewReader(fileContent))
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
		testServer.store.GetPool().Exec(context.Background(), "DELETE FROM sessions WHERE user_id = $1", testUserClaims.UserID)
		http.HandlerFunc(testServer.LoginHandler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body)))
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
	query := `INSERT INTO users (username, password_hash, display_name) VALUES ($1, $2, $3) 
			  ON CONFLICT (username) DO UPDATE SET password_hash = $2
			  RETURNING id, username`
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

	claims, err := auth.VerifyJWT(loginResp.AccessToken, testServer.config.JWT.Secret)
	require.NoError(t, err)
	originalSessionID := claims.SessionID

	time.Sleep(1 * time.Second)

	refreshReq := RefreshTokenRequest{RefreshToken: loginResp.RefreshToken}
	body, _ := json.Marshal(refreshReq)
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	http.HandlerFunc(testServer.RefreshTokenHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var firstRefreshResp TokenResponse
	err = json.Unmarshal(rr.Body.Bytes(), &firstRefreshResp)
	require.NoError(t, err)
	require.NotEmpty(t, firstRefreshResp.AccessToken)
	require.NotEmpty(t, firstRefreshResp.RefreshToken)
	require.NotEqual(t, loginResp.RefreshToken, firstRefreshResp.RefreshToken)

	newClaims, err := auth.VerifyJWT(firstRefreshResp.AccessToken, testServer.config.JWT.Secret)
	require.NoError(t, err)
	require.Equal(t, originalSessionID, newClaims.SessionID, "Session ID should be preserved after refresh")

	sessions, err := testServer.store.ListSessionsForUser(context.Background(), claims.UserID)
	require.NoError(t, err)
	require.Len(t, sessions, 1, "There should still be only one session in the database")

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
	sharer := createTestUserWithPassword(t, "sharer_user_fav", "password")
	recipient := createTestUserWithPassword(t, "recipient_user_fav", "password")

	sharerLogin := loginUserForTest(t, "sharer_user_fav", "password")
	recipientLogin := loginUserForTest(t, "recipient_user_fav", "password")

	nodeToShare, err := createTestNodeAPI(t, "plik_do_udostepnienia_fav.txt", "file", nil, sharer.ID)
	require.NoError(t, err)

	var shareID int64

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Post("/api/v1/nodes/folder", testServer.CreateFolderHandler)
	router.Post("/api/v1/nodes/{nodeId}/share", testServer.ShareNodeHandler)
	router.Get("/api/v1/shares/incoming/nodes", testServer.ListSharedNodesHandler)
	router.Post("/api/v1/nodes/{nodeId}/favorite", testServer.AddFavoriteHandler)
	router.Delete("/api/v1/shares/{shareId}", testServer.DeleteShareHandler)
	router.Get("/api/v1/nodes/{nodeId}/download", testServer.DownloadFileHandler)
	router.Get("/api/v1/favorites", testServer.ListFavoritesHandler)
	router.Delete("/api/v1/nodes/{nodeId}/favorite", testServer.RemoveFavoriteHandler)

	t.Run("sharer shares a node with recipient", func(t *testing.T) {
		shareReq := ShareRequest{RecipientUsername: recipient.Username, Permissions: "read"}
		body, _ := json.Marshal(shareReq)
		url := fmt.Sprintf("/api/v1/nodes/%s/share", nodeToShare.ID)
		req := httptest.NewRequest("POST", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+sharerLogin.AccessToken)
		rr := httptest.NewRecorder()

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

		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []*models.RichNode
		err := json.Unmarshal(rr.Body.Bytes(), &nodes)
		require.NoError(t, err)
		require.Len(t, nodes, 1)
		require.Equal(t, nodeToShare.ID, nodes[0].ID)
		require.Equal(t, sharer.Username, nodes[0].Owner.Username)
	})

	t.Run("recipient adds shared node to favorites and lists them", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s/favorite", nodeToShare.ID)
		req := httptest.NewRequest("POST", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusNoContent, rr.Code)

		reqList := httptest.NewRequest("GET", "/api/v1/favorites", nil)
		reqList.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rrList := httptest.NewRecorder()
		router.ServeHTTP(rrList, reqList)

		require.Equal(t, http.StatusOK, rrList.Code)
		var favs []*models.RichNode
		err := json.Unmarshal(rrList.Body.Bytes(), &favs)
		require.NoError(t, err)
		require.Len(t, favs, 1)
		require.Equal(t, nodeToShare.ID, favs[0].ID)
	})

	t.Run("recipient removes node from favorites", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s/favorite", nodeToShare.ID)
		req := httptest.NewRequest("DELETE", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusNoContent, rr.Code)

		favs, err := testServer.store.GetRichFavorites(context.Background(), recipient.ID, 10, 0, "")
		require.NoError(t, err)
		require.Len(t, favs, 0)
	})

	t.Run("sharer revokes the share", func(t *testing.T) {
		require.NotZero(t, shareID, "Share ID should have been set in the first sub-test")

		url := fmt.Sprintf("/api/v1/shares/%d", shareID)
		req := httptest.NewRequest("DELETE", url, nil)
		req.Header.Set("Authorization", "Bearer "+sharerLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusNoContent, rr.Code)
	})

	t.Run("recipient can no longer access the node", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s/download", nodeToShare.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("recipient cannot re-share a read-only node", func(t *testing.T) {
		stranger := createTestUserWithPassword(t, "stranger_user_share", "password")

		shareReq := ShareRequest{RecipientUsername: stranger.Username, Permissions: "read"}
		body, _ := json.Marshal(shareReq)
		url := fmt.Sprintf("/api/v1/nodes/%s/share", nodeToShare.ID)
		req := httptest.NewRequest("POST", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestPermissions_ReadOnlyShare(t *testing.T) {
	sharer := createTestUserWithPassword(t, "user_perm_sharer", "password")
	recipient := createTestUserWithPassword(t, "user_perm_recipient", "password")
	recipientLogin := loginUserForTest(t, "user_perm_recipient", "password")

	readOnlyFolder, _ := createTestNodeAPI(t, "ReadOnlySharedFolder", "folder", nil, sharer.ID)
	_, err := testServer.store.ShareNode(context.Background(), database.ShareNodeParams{
		NodeID: readOnlyFolder.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "read",
	})
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Post("/api/v1/nodes/folder", testServer.CreateFolderHandler)
	router.Post("/api/v1/nodes/file", testServer.UploadFileHandler)

	t.Run("recipient cannot create a subfolder in a read-only share", func(t *testing.T) {
		payload := CreateFolderRequest{Name: "Illegal Subfolder", ParentID: &readOnlyFolder.ID}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("recipient cannot upload a file to a read-only share", func(t *testing.T) {
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		writer.WriteField("parent_id", readOnlyFolder.ID)
		part, _ := writer.CreateFormFile("file", "illegal_file.txt")
		part.Write([]byte("some data"))
		writer.Close()

		req := httptest.NewRequest("POST", "/api/v1/nodes/file", body)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusForbidden, rr.Code)
	})
}

func TestTrashHandlers_Integration(t *testing.T) {
	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Delete("/api/v1/nodes/{nodeId}", testServer.DeleteNodeHandler)
	router.Get("/api/v1/trash", testServer.ListTrashHandler)
	router.Post("/api/v1/nodes/{nodeId}/restore", testServer.RestoreNodeHandler)
	router.Delete("/api/v1/trash/purge", testServer.PurgeTrashHandler)
	router.Delete("/api/v1/trash/{nodeId}", testServer.PurgeSingleNodeHandler)
	router.Post("/api/v1/nodes/file", testServer.UploadFileHandler)

	t.Run("full trash lifecycle", func(t *testing.T) {
		username := "user_trash_lifecycle"
		testUser := createTestUserWithPassword(t, username, "password")
		loginResp := loginUserForTest(t, username, "password")
		nodeToTrash, _ := createTestNodeAPI(t, "plik_do_kosza.txt", "file", nil, testUser.ID)

		urlDelete := fmt.Sprintf("/api/v1/nodes/%s", nodeToTrash.ID)
		reqDelete := httptest.NewRequest("DELETE", urlDelete, nil)
		reqDelete.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrDelete := httptest.NewRecorder()
		router.ServeHTTP(rrDelete, reqDelete)
		require.Equal(t, http.StatusNoContent, rrDelete.Code)

		reqList := httptest.NewRequest("GET", "/api/v1/trash", nil)
		reqList.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrList := httptest.NewRecorder()
		router.ServeHTTP(rrList, reqList)
		require.Equal(t, http.StatusOK, rrList.Code)
		var nodes []*models.RichNode
		json.Unmarshal(rrList.Body.Bytes(), &nodes)
		require.Len(t, nodes, 1)
		require.Equal(t, nodeToTrash.ID, nodes[0].ID)

		urlRestore := fmt.Sprintf("/api/v1/nodes/%s/restore", nodeToTrash.ID)
		reqRestore := httptest.NewRequest("POST", urlRestore, nil)
		reqRestore.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrRestore := httptest.NewRecorder()
		router.ServeHTTP(rrRestore, reqRestore)
		require.Equal(t, http.StatusOK, rrRestore.Code)

		reqListAfterRestore := httptest.NewRequest("GET", "/api/v1/trash", nil)
		reqListAfterRestore.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrListAfterRestore := httptest.NewRecorder()
		router.ServeHTTP(rrListAfterRestore, reqListAfterRestore)
		var nodesAfterRestore []*models.RichNode
		json.Unmarshal(rrListAfterRestore.Body.Bytes(), &nodesAfterRestore)
		require.Len(t, nodesAfterRestore, 0)
	})

	t.Run("purge all trash", func(t *testing.T) {
		username := "user_purge_all"
		testUser := createTestUserWithPassword(t, username, "password")
		loginResp := loginUserForTest(t, username, "password")
		node1, _ := createTestNodeAPI(t, "purge_all_1.txt", "file", nil, testUser.ID)
		node2, _ := createTestNodeAPI(t, "purge_all_2.txt", "file", nil, testUser.ID)

		testServer.store.MoveNodeToTrash(context.Background(), node1.ID, testUser.ID)
		testServer.store.MoveNodeToTrash(context.Background(), node2.ID, testUser.ID)

		reqPurge := httptest.NewRequest("DELETE", "/api/v1/trash/purge", nil)
		reqPurge.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrPurge := httptest.NewRecorder()
		router.ServeHTTP(rrPurge, reqPurge)
		require.Equal(t, http.StatusNoContent, rrPurge.Code)

		var count int
		err := testServer.store.GetPool().QueryRow(context.Background(), "SELECT COUNT(*) FROM nodes WHERE owner_id = $1", testUser.ID).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	t.Run("purge single item from trash", func(t *testing.T) {
		username := "user_purge_single"
		testUser := createTestUserWithPassword(t, username, "password")
		loginResp := loginUserForTest(t, username, "password")

		var fileSize int64 = 1234
		folder, _ := createTestNodeAPI(t, "FolderToPurge", "folder", nil, testUser.ID)
		fileInFolder, _ := createTestNodeAPI(t, "FileInFolder", "file", &folder.ID, testUser.ID)
		fileToKeep, _ := createTestNodeAPI(t, "FileToKeepInTrash", "file", nil, testUser.ID)
		testServer.store.GetPool().Exec(context.Background(), "UPDATE nodes SET size_bytes=$1 WHERE id=$2", fileSize, fileInFolder.ID)

		testServer.store.MoveNodeToTrash(context.Background(), folder.ID, testUser.ID)
		testServer.store.MoveNodeToTrash(context.Background(), fileToKeep.ID, testUser.ID)

		url := fmt.Sprintf("/api/v1/trash/%s", folder.ID)
		req := httptest.NewRequest("DELETE", url, nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusNoContent, rr.Code)

		var count int
		err := testServer.store.GetPool().QueryRow(context.Background(), "SELECT COUNT(*) FROM nodes WHERE id IN ($1, $2)", folder.ID, fileInFolder.ID).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)

		trashItems, _ := testServer.store.GetRichTrash(context.Background(), testUser.ID, 10, 0, "")
		require.Len(t, trashItems, 1)
		require.Equal(t, fileToKeep.ID, trashItems[0].ID)
	})

	t.Run("recursively restore folder from trash", func(t *testing.T) {
		username := "user_recursive_restore"
		testUser := createTestUserWithPassword(t, username, "password")
		loginResp := loginUserForTest(t, username, "password")

		folder, _ := createTestNodeAPI(t, "RestoreRecursive", "folder", nil, testUser.ID)
		fileInFolder, _ := createTestNodeAPI(t, "FileInsideRestore", "file", &folder.ID, testUser.ID)

		_, err := testServer.store.MoveNodeToTrash(context.Background(), folder.ID, testUser.ID)
		require.NoError(t, err)

		var count int
		err = testServer.store.GetPool().QueryRow(context.Background(), "SELECT COUNT(*) FROM nodes WHERE id IN ($1, $2) AND deleted_at IS NOT NULL", folder.ID, fileInFolder.ID).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)

		urlRestore := fmt.Sprintf("/api/v1/nodes/%s/restore", folder.ID)
		reqRestore := httptest.NewRequest("POST", urlRestore, nil)
		reqRestore.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrRestore := httptest.NewRecorder()
		router.ServeHTTP(rrRestore, reqRestore)
		require.Equal(t, http.StatusOK, rrRestore.Code)

		err = testServer.store.GetPool().QueryRow(context.Background(), "SELECT COUNT(*) FROM nodes WHERE id IN ($1, $2) AND deleted_at IS NOT NULL", folder.ID, fileInFolder.ID).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)

		restoredFile, err := testServer.store.GetNodeIfAccessible(context.Background(), fileInFolder.ID, testUser.ID)
		require.NoError(t, err)
		require.NotNil(t, restoredFile.ParentID)
		require.Equal(t, folder.ID, *restoredFile.ParentID)
	})

	t.Run("fail to restore on conflict without rename flag", func(t *testing.T) {
		username := "user_restore_fail_conflict"
		testUser := createTestUserWithPassword(t, username, "password")
		loginResp := loginUserForTest(t, username, "password")

		nodeInTrash, _ := createTestNodeAPI(t, "conflict_file.txt", "file", nil, testUser.ID)
		testServer.store.MoveNodeToTrash(context.Background(), nodeInTrash.ID, testUser.ID)

		createTestNodeAPI(t, "conflict_file.txt", "file", nil, testUser.ID)

		urlRestore := fmt.Sprintf("/api/v1/nodes/%s/restore", nodeInTrash.ID)
		reqRestore := httptest.NewRequest("POST", urlRestore, nil)
		reqRestore.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrRestore := httptest.NewRecorder()
		router.ServeHTTP(rrRestore, reqRestore)

		require.Equal(t, http.StatusConflict, rrRestore.Code)

		var count int
		err := testServer.store.GetPool().QueryRow(context.Background(), "SELECT COUNT(*) FROM nodes WHERE id = $1 AND deleted_at IS NOT NULL", nodeInTrash.ID).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count, "Node should remain in the trash after a failed restore")
	})

	t.Run("restore with rename on conflict", func(t *testing.T) {
		username := "user_restore_rename"
		testUser := createTestUserWithPassword(t, username, "password")
		loginResp := loginUserForTest(t, username, "password")

		nodeInTrash, _ := createTestNodeAPI(t, "file_to_rename.txt", "file", nil, testUser.ID)
		testServer.store.MoveNodeToTrash(context.Background(), nodeInTrash.ID, testUser.ID)

		createTestNodeAPI(t, "file_to_rename.txt", "file", nil, testUser.ID)

		urlRestore := fmt.Sprintf("/api/v1/nodes/%s/restore?renameOnConflict=true", nodeInTrash.ID)
		reqRestore := httptest.NewRequest("POST", urlRestore, nil)
		reqRestore.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrRestore := httptest.NewRecorder()
		router.ServeHTTP(rrRestore, reqRestore)

		require.Equal(t, http.StatusOK, rrRestore.Code)

		var restoredNode models.RichNode
		err := json.Unmarshal(rrRestore.Body.Bytes(), &restoredNode)
		require.NoError(t, err)

		require.NotEqual(t, "file_to_rename.txt", restoredNode.Name, "Node name should have been changed")
		require.Equal(t, "file_to_rename (1).txt", restoredNode.Name)
		require.Nil(t, restoredNode.ParentID, "Node should be restored to root")
	})

	t.Run("verify physical file deletion on purge", func(t *testing.T) {
		username := "user_physical_del"
		testUser := createTestUserWithPassword(t, username, "password")
		login := loginUserForTest(t, username, "password")

		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "to_be_deleted.txt")
		part.Write([]byte("some data"))
		writer.Close()

		reqUp := httptest.NewRequest("POST", "/api/v1/nodes/file", body)
		reqUp.Header.Set("Content-Type", writer.FormDataContentType())
		reqUp.Header.Set("Authorization", "Bearer "+login.AccessToken)
		rrUp := httptest.NewRecorder()
		router.ServeHTTP(rrUp, reqUp)
		require.Equal(t, http.StatusCreated, rrUp.Code)

		var uploadedNodes []*models.RichNode
		json.Unmarshal(rrUp.Body.Bytes(), &uploadedNodes)
		nodeID := uploadedNodes[0].ID

		_, err := testServer.store.MoveNodeToTrash(context.Background(), nodeID, testUser.ID)
		require.NoError(t, err)

		urlPurge := fmt.Sprintf("/api/v1/trash/%s", nodeID)
		reqPurge := httptest.NewRequest("DELETE", urlPurge, nil)
		reqPurge.Header.Set("Authorization", "Bearer "+login.AccessToken)
		rrPurge := httptest.NewRecorder()
		router.ServeHTTP(rrPurge, reqPurge)
		require.Equal(t, http.StatusNoContent, rrPurge.Code)

		_, err = testServer.storage.Get(nodeID)
		require.Error(t, err, "File should no longer exist in local storage")
		require.Contains(t, err.Error(), "not found")
	})
}

func TestTrash_DataIntegrity(t *testing.T) {
	user := createTestUserWithPassword(t, "user_trash_integrity", "password")
	loginResp := loginUserForTest(t, "user_trash_integrity", "password")

	parentFolder, _ := createTestNodeAPI(t, "ParentForTrash", "folder", nil, user.ID)
	nodeToTrash, _ := createTestNodeAPI(t, "FileToTrash.txt", "file", &parentFolder.ID, user.ID)

	var initialParentID *string
	err := testServer.store.GetPool().QueryRow(context.Background(), "SELECT parent_id FROM nodes WHERE id=$1", nodeToTrash.ID).Scan(&initialParentID)
	require.NoError(t, err)
	require.NotNil(t, initialParentID)
	require.Equal(t, parentFolder.ID, *initialParentID)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Delete("/api/v1/nodes/{nodeId}", testServer.DeleteNodeHandler)

	url := fmt.Sprintf("/api/v1/nodes/%s", nodeToTrash.ID)
	req := httptest.NewRequest("DELETE", url, nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)

	var deletedAt *time.Time
	var finalParentID *string
	var originalParentID *string

	query := "SELECT deleted_at, parent_id, original_parent_id FROM nodes WHERE id = $1"
	err = testServer.store.GetPool().QueryRow(context.Background(), query, nodeToTrash.ID).Scan(&deletedAt, &finalParentID, &originalParentID)
	require.NoError(t, err)

	require.NotNil(t, deletedAt, "deleted_at should be set after moving to trash")

	require.NotNil(t, originalParentID, "original_parent_id should be set")
	require.Equal(t, parentFolder.ID, *originalParentID, "original_parent_id should be the ID of the old parent folder")

	require.NotNil(t, finalParentID, "parent_id should NOT be null")
	require.Equal(t, parentFolder.ID, *finalParentID, "parent_id should remain unchanged")
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

func TestHealthCheckHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	http.HandlerFunc(testServer.HealthCheckHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var status map[string]string
	err := json.Unmarshal(rr.Body.Bytes(), &status)
	require.NoError(t, err)
	require.Equal(t, "ok", status["status"])
	require.Equal(t, "connected", status["database"])
}

func TestUserHandlers_Integration(t *testing.T) {
	username := "user_for_me_handlers"
	password := "oldPassword123"
	user := createTestUserWithPassword(t, username, password)
	loginResp := loginUserForTest(t, username, password)

	fileForStorage, err := createTestNodeAPI(t, "file_for_storage.txt", "file", nil, user.ID)
	require.NoError(t, err)
	err = testServer.store.UpdateUserStorage(context.Background(), user.ID, *fileForStorage.SizeBytes)
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/me", testServer.GetCurrentUserHandler)
	router.Patch("/api/v1/me/password", testServer.ChangePasswordHandler)

	t.Run("get current user with storage info", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)

		var returnedUser models.User
		err := json.Unmarshal(rr.Body.Bytes(), &returnedUser)
		require.NoError(t, err)

		require.Equal(t, user.ID, returnedUser.ID)
		require.Equal(t, user.Username, returnedUser.Username)

		require.Equal(t, *fileForStorage.SizeBytes, returnedUser.StorageUsedBytes)
		require.Greater(t, returnedUser.StorageQuotaBytes, int64(0))
	})

	t.Run("change password successfully", func(t *testing.T) {
		loginUserForTest(t, username, password)

		payload := ChangePasswordRequest{OldPassword: password, NewPassword: "newPassword456"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("PATCH", "/api/v1/me/password", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusNoContent, rr.Code)

		sessions, err := testServer.store.ListSessionsForUser(context.Background(), user.ID)
		require.NoError(t, err)
		require.Empty(t, sessions, "All sessions should be terminated after password change")

		loginUserForTest(t, username, "newPassword456")
	})
}

func TestDownloadArchiveHandler(t *testing.T) {
	user := createTestUserWithPassword(t, "archive_user", "password")
	loginResp := loginUserForTest(t, "archive_user", "password")

	folder1, err := createTestNodeAPI(t, "Folder_A", "folder", nil, user.ID)
	require.NoError(t, err)
	file1, err := createTestNodeAPI(t, "plik1.txt", "file", &folder1.ID, user.ID)
	require.NoError(t, err)
	err = testServer.storage.Save(file1.ID, strings.NewReader("content1"))
	require.NoError(t, err)

	file2, err := createTestNodeAPI(t, "plik2.txt", "file", nil, user.ID)
	require.NoError(t, err)
	err = testServer.storage.Save(file2.ID, strings.NewReader("content2"))
	require.NoError(t, err)

	ids := fmt.Sprintf("%s,%s", folder1.ID, file2.ID)
	url := fmt.Sprintf("/api/v1/nodes/archive?ids=%s", ids)
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	rr := httptest.NewRecorder()

	router := chi.NewRouter()
	router.With(testServer.AuthMiddleware).Get("/api/v1/nodes/archive", testServer.DownloadArchiveHandler)
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/zip", rr.Header().Get("Content-Type"))

	zipBody := rr.Body.Bytes()
	zipReader, err := zip.NewReader(bytes.NewReader(zipBody), int64(len(zipBody)))
	require.NoError(t, err)

	foundFiles := make(map[string]bool)
	for _, f := range zipReader.File {
		foundFiles[f.Name] = true
	}

	require.True(t, foundFiles["Folder_A/"], "Expected to find directory entry for Folder_A")
	require.True(t, foundFiles["Folder_A/plik1.txt"], "Expected to find file inside Folder_A")
	require.True(t, foundFiles["plik2.txt"], "Expected to find root file plik2.txt")
	require.Len(t, foundFiles, 3, "Archive should contain exactly 3 entries")
}

func TestDownloadArchiveHandler_LargeFolder(t *testing.T) {
	username := "user_for_zip_test"
	password := "password123"
	testUser := createTestUserWithPassword(t, username, password)

	t.Cleanup(func() {
		testServer.store.GetPool().Exec(context.Background(), "DELETE FROM users WHERE id = $1", testUser.ID)
	})

	loginResp := loginUserForTest(t, username, password)

	largeFolder, err := createTestNodeAPI(t, "LargeFolder", "folder", nil, testUser.ID)
	require.NoError(t, err)

	fileCount := 1005
	fileContent := "test content"
	for i := 0; i < fileCount; i++ {
		fileName := fmt.Sprintf("file_%04d.txt", i)
		fileNode, err := createTestNodeAPI(t, fileName, "file", &largeFolder.ID, testUser.ID)
		require.NoError(t, err)
		err = testServer.storage.Save(fileNode.ID, strings.NewReader(fileContent))
		require.NoError(t, err)
	}

	url := fmt.Sprintf("/api/v1/nodes/archive?ids=%s", largeFolder.ID)
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	rr := httptest.NewRecorder()

	router := chi.NewRouter()
	router.With(testServer.AuthMiddleware).Get("/api/v1/nodes/archive", testServer.DownloadArchiveHandler)
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	zipBody := rr.Body.Bytes()
	zipReader, err := zip.NewReader(bytes.NewReader(zipBody), int64(len(zipBody)))
	require.NoError(t, err, "Should be able to read the returned ZIP archive")

	archivedFileCount := 0
	for _, f := range zipReader.File {
		if !strings.HasSuffix(f.Name, "/") {
			archivedFileCount++
		}
	}

	require.Equal(t, fileCount, archivedFileCount, "ZIP archive should contain all 1005 files")
}

func TestCopyNodeHandler_Integration(t *testing.T) {
	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Post("/api/v1/nodes/{nodeId}/copy", testServer.CopyNodeHandler)

	t.Run("successful file copy", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_copy_success", "password")
		loginResp := loginUserForTest(t, "user_copy_success", "password")
		sourceFile, err := createTestNodeAPI(t, "file_to_copy.txt", "file", nil, user.ID)
		require.NoError(t, err)
		targetFolder, err := createTestNodeAPI(t, "target_folder", "folder", nil, user.ID)
		require.NoError(t, err)
		sourceContent := "oryginalna zawartość"
		err = testServer.storage.Save(sourceFile.ID, strings.NewReader(sourceContent))
		require.NoError(t, err)

		copyReq := CopyNodeRequest{ParentID: targetFolder.ID}
		body, _ := json.Marshal(copyReq)
		url := fmt.Sprintf("/api/v1/nodes/%s/copy", sourceFile.ID)
		req := httptest.NewRequest("POST", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusCreated, rr.Code)
		var copiedNode models.RichNode
		json.Unmarshal(rr.Body.Bytes(), &copiedNode)
		require.Equal(t, sourceFile.Name, copiedNode.Name)
		require.Equal(t, targetFolder.ID, *copiedNode.ParentID)
	})

	t.Run("copy with name conflict", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_copy_conflict", "password")
		loginResp := loginUserForTest(t, "user_copy_conflict", "password")
		sourceFile, err := createTestNodeAPI(t, "file_with_conflict.txt", "file", nil, user.ID)
		require.NoError(t, err)
		targetFolder, err := createTestNodeAPI(t, "target_folder_conflict", "folder", nil, user.ID)
		require.NoError(t, err)
		_, err = createTestNodeAPI(t, "file_with_conflict.txt", "file", &targetFolder.ID, user.ID)
		require.NoError(t, err)

		copyReq := CopyNodeRequest{ParentID: targetFolder.ID}
		body, _ := json.Marshal(copyReq)
		url := fmt.Sprintf("/api/v1/nodes/%s/copy", sourceFile.ID)
		req := httptest.NewRequest("POST", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusConflict, rr.Code)
	})

	t.Run("copy folder recursively with new name", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_copy_recursive_rename", "password")
		loginResp := loginUserForTest(t, "user_copy_recursive_rename", "password")
		sourceFolder, err := createTestNodeAPI(t, "SourceFolderRecursive", "folder", nil, user.ID)
		require.NoError(t, err)

		innerFile, err := createTestNodeAPI(t, "wewnetrzny.txt", "file", &sourceFolder.ID, user.ID)
		require.NoError(t, err)

		sourceContent := "to jest treść pliku testowego"
		err = testServer.storage.Save(innerFile.ID, strings.NewReader(sourceContent))
		require.NoError(t, err, "Failed to save content for the source file")

		newName := "CopiedSourceFolder"
		copyReq := CopyNodeRequest{ParentID: "root", NewName: &newName}
		body, _ := json.Marshal(copyReq)
		url := fmt.Sprintf("/api/v1/nodes/%s/copy", sourceFolder.ID)
		req := httptest.NewRequest("POST", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusCreated, rr.Code)

		var copiedFolder models.RichNode
		err = json.Unmarshal(rr.Body.Bytes(), &copiedFolder)
		require.NoError(t, err)
		require.Equal(t, newName, copiedFolder.Name)
		require.Equal(t, user.Username, copiedFolder.Owner.Username)

		children, err := testServer.store.GetRichNodesByParentID(context.Background(), user.ID, user.ID, &copiedFolder.ID, 10, 0, "", "")
		require.NoError(t, err)
		require.Len(t, children, 1)
		require.Equal(t, "wewnetrzny.txt", children[0].Name)
		require.Equal(t, user.Username, children[0].Owner.Username)

		copiedFileContent, err := testServer.storage.Get(children[0].ID)
		require.NoError(t, err)
		defer copiedFileContent.Close()
		contentBytes, err := io.ReadAll(copiedFileContent)
		require.NoError(t, err)
		require.Equal(t, sourceContent, string(contentBytes))
	})

	t.Run("copy fails when destination quota is exceeded", func(t *testing.T) {
		userA := createTestUserWithPassword(t, "user_copy_quota_a", "password")
		userB := createTestUserWithPassword(t, "user_copy_quota_b", "password")
		loginA := loginUserForTest(t, "user_copy_quota_a", "password")

		var smallQuota int64 = 100
		_, err := testServer.store.GetPool().Exec(context.Background(), "UPDATE users SET storage_quota_bytes = $1 WHERE id = $2", smallQuota, userB.ID)
		require.NoError(t, err)

		var fileSize int64 = 200
		sourceFile, _ := createTestNodeAPI(t, "large_file.txt", "file", nil, userA.ID)
		testServer.store.GetPool().Exec(context.Background(), "UPDATE nodes SET size_bytes=$1 WHERE id=$2", fileSize, sourceFile.ID)

		targetFolder, _ := createTestNodeAPI(t, "target_for_large_file", "folder", nil, userB.ID)

		_, err = testServer.store.ShareNode(context.Background(), database.ShareNodeParams{
			NodeID: targetFolder.ID, SharerID: userB.ID, RecipientID: userA.ID, Permissions: "write",
		})
		require.NoError(t, err)

		copyReq := CopyNodeRequest{ParentID: targetFolder.ID}
		body, _ := json.Marshal(copyReq)
		url := fmt.Sprintf("/api/v1/nodes/%s/copy", sourceFile.ID)
		req := httptest.NewRequest("POST", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginA.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
	})
}

func TestSearchHandler_Integration(t *testing.T) {
	userA := createTestUserWithPassword(t, "user_search_a", "password")
	userB := createTestUserWithPassword(t, "user_search_b", "password")
	loginA := loginUserForTest(t, "user_search_a", "password")

	createTestNodeAPI(t, "Raport Roczny A.pdf", "file", nil, userA.ID)
	createTestNodeAPI(t, "Projekt Tajny A", "folder", nil, userA.ID)
	createTestNodeAPI(t, "Notatki B.txt", "file", nil, userB.ID)
	sharedWithA, _ := createTestNodeAPI(t, "Raport Wspólny B.docx", "file", nil, userB.ID)

	_, err := testServer.store.ShareNode(context.Background(), database.ShareNodeParams{
		NodeID: sharedWithA.ID, SharerID: userB.ID, RecipientID: userA.ID, Permissions: "read",
	})
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/search", testServer.SearchHandler)

	t.Run("search finds own and shared files with rich data", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/search?q=Raport", nil)
		req.Header.Set("Authorization", "Bearer "+loginA.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var results []*models.RichNode
		json.Unmarshal(rr.Body.Bytes(), &results)

		require.Len(t, results, 2, "Should find two reports: own and shared")

		foundNames := make(map[string]bool)
		for _, node := range results {
			foundNames[node.Name] = true
			if node.Name == "Raport Roczny A.pdf" {
				require.Equal(t, userA.Username, node.Owner.Username)
			}
			if node.Name == "Raport Wspólny B.docx" {
				require.Equal(t, userB.Username, node.Owner.Username)
			}
		}
		require.Contains(t, foundNames, "Raport Roczny A.pdf")
		require.Contains(t, foundNames, "Raport Wspólny B.docx")
	})

	t.Run("search does not find other users private files", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/search?q=Notatki", nil)
		req.Header.Set("Authorization", "Bearer "+loginA.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var results []*models.RichNode
		json.Unmarshal(rr.Body.Bytes(), &results)
		require.Len(t, results, 0, "Should not find private files of other users")
	})
}

func TestUpdateCurrentUserHandler_Integration(t *testing.T) {
	username := "user_update_me"
	password := "password123"
	testUser := createTestUserWithPassword(t, username, password)
	loginResp := loginUserForTest(t, username, password)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Patch("/api/v1/me", testServer.UpdateCurrentUserHandler)

	t.Run("update display name", func(t *testing.T) {
		newDisplayName := "Nowa Super Nazwa"
		payload := UpdateMeRequest{DisplayName: &newDisplayName}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("PATCH", "/api/v1/me", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)

		var updatedUser models.User
		err := json.Unmarshal(rr.Body.Bytes(), &updatedUser)
		require.NoError(t, err)
		require.NotNil(t, updatedUser.DisplayName)
		require.Equal(t, newDisplayName, *updatedUser.DisplayName)

		userFromDB, err := testServer.store.GetUserByID(context.Background(), testUser.ID)
		require.NoError(t, err)
		require.NotNil(t, userFromDB.DisplayName)
		require.Equal(t, newDisplayName, *userFromDB.DisplayName)
	})
}

func TestGetNodeHandler_Integration(t *testing.T) {
	owner := createTestUserWithPassword(t, "user_getnode_owner", "password")
	recipient := createTestUserWithPassword(t, "user_getnode_recipient", "password")
	stranger := createTestUserWithPassword(t, "user_getnode_stranger", "password")

	ownerLogin := loginUserForTest(t, owner.Username, "password")
	recipientLogin := loginUserForTest(t, recipient.Username, "password")
	strangerLogin := loginUserForTest(t, stranger.Username, "password")

	folderA, _ := createTestNodeAPI(t, "FolderA_Details", "folder", nil, owner.ID)
	fileB, _ := createTestNodeAPI(t, "FileB_Details.txt", "file", &folderA.ID, owner.ID)

	_, err := testServer.store.ShareNode(context.Background(), database.ShareNodeParams{
		NodeID:      fileB.ID,
		SharerID:    owner.ID,
		RecipientID: recipient.ID,
		Permissions: "read",
	})
	require.NoError(t, err)

	err = testServer.store.AddFavorite(context.Background(), owner.ID, fileB.ID)
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/nodes/{nodeId}", testServer.GetNodeHandler)

	t.Run("owner gets full details of their node", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s", fileB.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+ownerLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp NodeDetailResponse
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)

		require.Equal(t, fileB.ID, resp.ID)
		require.Equal(t, owner.Username, resp.Owner.Username)
		require.True(t, resp.IsFavorited, "File should be favorited by owner")
		require.True(t, resp.IsShared, "File should be marked as shared")

		require.Len(t, resp.Path, 1, "Path should contain one ancestor")
		require.Equal(t, folderA.ID, resp.Path[0].ID)
		require.Equal(t, "FolderA_Details", resp.Path[0].Name)
		require.Equal(t, owner.Username, resp.Path[0].Owner.Username)

		require.Len(t, resp.Shares, 1, "Shares should contain one entry")
		require.Equal(t, recipient.Username, resp.Shares[0].RecipientUsername)
		require.Equal(t, "read", resp.Shares[0].Permissions)
	})

	t.Run("recipient gets details of a shared node", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s", fileB.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp NodeDetailResponse
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)

		require.Equal(t, fileB.ID, resp.ID)
		require.Equal(t, owner.Username, resp.Owner.Username)
		require.False(t, resp.IsFavorited, "File should not be favorited by recipient by default")
		require.True(t, resp.IsShared, "Shared flag should be visible to recipient")

		require.Empty(t, resp.Shares, "Shares field should be empty for non-owners")

		require.Len(t, resp.Path, 1)
		require.Equal(t, folderA.ID, resp.Path[0].ID)
	})

	t.Run("stranger cannot get details of a private node", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s", fileB.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+strangerLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("recipient gets details with relative path using share_context", func(t *testing.T) {
		ownerCtx := createTestUserWithPassword(t, "owner_path_context", "password")
		recipientCtx := createTestUserWithPassword(t, "recipient_path_context", "password")
		recipientLoginCtx := loginUserForTest(t, recipientCtx.Username, "password")

		privateRoot, _ := createTestNodeAPI(t, "Private Root", "folder", nil, ownerCtx.ID)
		sharedFolder, _ := createTestNodeAPI(t, "Shared Folder", "folder", &privateRoot.ID, ownerCtx.ID)
		innerFile, _ := createTestNodeAPI(t, "Inner File.txt", "file", &sharedFolder.ID, ownerCtx.ID)

		_, err := testServer.store.ShareNode(context.Background(), database.ShareNodeParams{
			NodeID: sharedFolder.ID, SharerID: ownerCtx.ID, RecipientID: recipientCtx.ID, Permissions: "read",
		})
		require.NoError(t, err)

		url := fmt.Sprintf("/api/v1/nodes/%s?share_context=%s", innerFile.ID, sharedFolder.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLoginCtx.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp NodeDetailResponse
		err = json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)

		require.Len(t, resp.Path, 0, "Path should be empty relative to the share context root")

		innerFolder, _ := createTestNodeAPI(t, "Inner Folder", "folder", &sharedFolder.ID, ownerCtx.ID)
		deepFile, _ := createTestNodeAPI(t, "Deep File.txt", "file", &innerFolder.ID, ownerCtx.ID)

		urlDeep := fmt.Sprintf("/api/v1/nodes/%s?share_context=%s", deepFile.ID, sharedFolder.ID)
		reqDeep := httptest.NewRequest("GET", urlDeep, nil)
		reqDeep.Header.Set("Authorization", "Bearer "+recipientLoginCtx.AccessToken)
		rrDeep := httptest.NewRecorder()
		router.ServeHTTP(rrDeep, reqDeep)

		require.Equal(t, http.StatusOK, rrDeep.Code)
		var respDeep NodeDetailResponse
		err = json.Unmarshal(rrDeep.Body.Bytes(), &respDeep)
		require.NoError(t, err)

		require.Len(t, respDeep.Path, 1, "Path should contain one element relative to share context")
		require.Equal(t, "Inner Folder", respDeep.Path[0].Name, "Path should not show 'Private Root'")
	})
}

func TestGetLatestEventHandler_Integration(t *testing.T) {
	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/events/latest", testServer.GetLatestEventHandler)
	router.Post("/api/v1/nodes/folder", testServer.CreateFolderHandler)
	router.Get("/api/v1/events", testServer.GetEventsHandler)

	t.Run("returns zero when no events exist", func(t *testing.T) {
		username := "user_latest_event_empty"
		password := "password123"
		createTestUserWithPassword(t, username, password)
		loginResp := loginUserForTest(t, username, password)

		req := httptest.NewRequest("GET", "/api/v1/events/latest", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp LatestEventResponse
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, int64(0), resp.LatestEventID, "LatestEventID should be 0 for a user with no events")
	})

	t.Run("returns the ID of the last event", func(t *testing.T) {
		username := "user_latest_event_filled"
		password := "password123"
		createTestUserWithPassword(t, username, password)
		loginResp := loginUserForTest(t, username, password)

		for i := 0; i < 3; i++ {
			createFolderReq := CreateFolderRequest{Name: fmt.Sprintf("FolderForEvent_%d", i)}
			body, _ := json.Marshal(createFolderReq)
			reqCreate := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
			reqCreate.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
			rrCreate := httptest.NewRecorder()
			router.ServeHTTP(rrCreate, reqCreate)
			require.Equal(t, http.StatusCreated, rrCreate.Code)
		}

		req := httptest.NewRequest("GET", "/api/v1/events/latest", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp LatestEventResponse
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)

		require.Greater(t, resp.LatestEventID, int64(0), "LatestEventID should be greater than 0")

		reqAllEvents := httptest.NewRequest("GET", "/api/v1/events?since=0", nil)
		reqAllEvents.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrAllEvents := httptest.NewRecorder()
		router.ServeHTTP(rrAllEvents, reqAllEvents)

		require.Equal(t, http.StatusOK, rrAllEvents.Code, "Verification request for all events should succeed")
		var allEvents []database.Event
		json.Unmarshal(rrAllEvents.Body.Bytes(), &allEvents)

		require.Equal(t, allEvents[len(allEvents)-1].ID, resp.LatestEventID, "LatestEventID should match the ID of the last event in the list")
	})
}

func TestListSharedNodesHandler_SortingAndPagination(t *testing.T) {
	sharer := createTestUserWithPassword(t, "sharer_sort_pagination", "password")
	recipient := createTestUserWithPassword(t, "recipient_sort_pagination", "password")
	recipientLogin := loginUserForTest(t, recipient.Username, "password")

	readFolder, _ := createTestNodeAPI(t, "Read Folder", "folder", nil, sharer.ID)
	nodeInRead, _ := createTestNodeAPI(t, "File_In_Read", "file", &readFolder.ID, sharer.ID)
	_, err := testServer.store.ShareNode(context.Background(), database.ShareNodeParams{
		NodeID: readFolder.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "read",
	})
	require.NoError(t, err)

	writeFolder, _ := createTestNodeAPI(t, "Write Folder", "folder", nil, sharer.ID)
	nodeInWrite, _ := createTestNodeAPI(t, "File_In_Write", "file", &writeFolder.ID, sharer.ID)
	_, err = testServer.store.ShareNode(context.Background(), database.ShareNodeParams{
		NodeID: writeFolder.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "write",
	})
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/shares/incoming/nodes", testServer.ListSharedNodesHandler)

	runAndVerify := func(t *testing.T, url string, expectedName, expectedPermission string) {
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []*models.RichNode
		err := json.Unmarshal(rr.Body.Bytes(), &nodes)
		require.NoError(t, err)

		require.Len(t, nodes, 1)
		node := nodes[0]

		require.Equal(t, expectedName, node.Name)
		require.NotNil(t, node.Permissions)
		require.Equal(t, expectedPermission, *node.Permissions, "Node '%s' should have '%s' permission", node.Name, expectedPermission)
	}

	t.Run("lists root of shared items with correct permissions", func(t *testing.T) {
		baseQuery := fmt.Sprintf("/api/v1/shares/incoming/nodes?sharer_username=%s", sharer.Username)

		req := httptest.NewRequest("GET", baseQuery, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []*models.RichNode
		json.Unmarshal(rr.Body.Bytes(), &nodes)
		require.Len(t, nodes, 2)

		for _, node := range nodes {
			require.NotNil(t, node.Permissions, "Permissions field should not be nil for a shared node")
			if node.ID == readFolder.ID {
				require.Equal(t, "read", *node.Permissions)
			}
			if node.ID == writeFolder.ID {
				require.Equal(t, "write", *node.Permissions)
			}
		}
	})

	t.Run("lists content of read-only folder with inherited read permission", func(t *testing.T) {
		query := fmt.Sprintf("/api/v1/shares/incoming/nodes?sharer_username=%s&parent_id=%s", sharer.Username, readFolder.ID)
		runAndVerify(t, query, nodeInRead.Name, "read")
	})

	t.Run("lists content of writeable folder with inherited write permission", func(t *testing.T) {
		query := fmt.Sprintf("/api/v1/shares/incoming/nodes?sharer_username=%s&parent_id=%s", sharer.Username, writeFolder.ID)
		runAndVerify(t, query, nodeInWrite.Name, "write")
	})
}

func TestChunkedUpload_Integration(t *testing.T) {
	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Post("/api/v1/nodes/upload/initiate", testServer.InitiateUploadHandler)
	router.Patch("/api/v1/nodes/upload/{uploadId}", testServer.UploadChunkHandler)
	router.Post("/api/v1/nodes/upload/{uploadId}/complete", testServer.CompleteUploadHandler)

	t.Run("successful upload of a small file in two chunks", func(t *testing.T) {
		createTestUserWithPassword(t, "user_chunk_success", "password")
		loginResp := loginUserForTest(t, "user_chunk_success", "password")

		fileContent := "This is the full content of the file we are uploading."
		fileSize := int64(len(fileContent))
		fileName := "successful_upload.txt"

		initReqPayload := InitiateUploadRequest{
			Name: fileName, Size: fileSize, MimeType: "text/plain",
		}
		body, _ := json.Marshal(initReqPayload)
		reqInit := httptest.NewRequest("POST", "/api/v1/nodes/upload/initiate", bytes.NewReader(body))
		reqInit.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrInit := httptest.NewRecorder()
		router.ServeHTTP(rrInit, reqInit)
		require.Equal(t, http.StatusCreated, rrInit.Code)

		var initResp InitiateUploadResponse
		err := json.Unmarshal(rrInit.Body.Bytes(), &initResp)
		require.NoError(t, err)
		require.NotEmpty(t, initResp.UploadID)
		uploadID, err := uuid.Parse(initResp.UploadID)
		require.NoError(t, err)

		chunk1 := fileContent[:15]
		chunk2 := fileContent[15:]

		reqChunk1 := httptest.NewRequest("PATCH", "/api/v1/nodes/upload/"+uploadID.String(), strings.NewReader(chunk1))
		reqChunk1.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		reqChunk1.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(chunk1)-1, fileSize))
		rrChunk1 := httptest.NewRecorder()
		router.ServeHTTP(rrChunk1, reqChunk1)
		require.Equal(t, http.StatusNoContent, rrChunk1.Code)

		reqChunk2 := httptest.NewRequest("PATCH", "/api/v1/nodes/upload/"+uploadID.String(), strings.NewReader(chunk2))
		reqChunk2.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		reqChunk2.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", len(chunk1), fileSize-1, fileSize))
		rrChunk2 := httptest.NewRecorder()
		router.ServeHTTP(rrChunk2, reqChunk2)
		require.Equal(t, http.StatusNoContent, rrChunk2.Code)

		reqComplete := httptest.NewRequest("POST", "/api/v1/nodes/upload/"+uploadID.String()+"/complete", nil)
		reqComplete.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrComplete := httptest.NewRecorder()
		router.ServeHTTP(rrComplete, reqComplete)
		require.Equal(t, http.StatusCreated, rrComplete.Code)

		var finalNode models.RichNode
		err = json.Unmarshal(rrComplete.Body.Bytes(), &finalNode)
		require.NoError(t, err)

		require.Equal(t, fileName, finalNode.Name)
		require.Equal(t, fileSize, *finalNode.SizeBytes)

		reader, err := testServer.storage.Get(finalNode.ID)
		require.NoError(t, err)
		defer reader.Close()
		retrievedContent, _ := io.ReadAll(reader)
		require.Equal(t, string(fileContent), string(retrievedContent))

		uploadSession, err := testServer.store.GetUploadByID(context.Background(), uploadID)
		require.NoError(t, err)
		require.Nil(t, uploadSession)
	})

	t.Run("initiate fails on quota exceeded", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_chunk_quota", "password")
		loginResp := loginUserForTest(t, "user_chunk_quota", "password")

		var smallQuota int64 = 50
		_, err := testServer.store.GetPool().Exec(context.Background(), "UPDATE users SET storage_quota_bytes = $1, storage_used_bytes = 0 WHERE id = $2", smallQuota, user.ID)
		require.NoError(t, err)

		initReqPayload := InitiateUploadRequest{Name: "too_big.txt", Size: 100}
		body, _ := json.Marshal(initReqPayload)
		reqInit := httptest.NewRequest("POST", "/api/v1/nodes/upload/initiate", bytes.NewReader(body))
		reqInit.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrInit := httptest.NewRecorder()
		router.ServeHTTP(rrInit, reqInit)

		require.Equal(t, http.StatusRequestEntityTooLarge, rrInit.Code)
	})

	t.Run("upload chunk fails on wrong byte offset", func(t *testing.T) {
		createTestUserWithPassword(t, "user_chunk_offset", "password")
		loginResp := loginUserForTest(t, "user_chunk_offset", "password")

		initReqPayload := InitiateUploadRequest{Name: "wrong_offset.txt", Size: 100}
		body, _ := json.Marshal(initReqPayload)
		reqInit := httptest.NewRequest("POST", "/api/v1/nodes/upload/initiate", bytes.NewReader(body))
		reqInit.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrInit := httptest.NewRecorder()
		router.ServeHTTP(rrInit, reqInit)
		require.Equal(t, http.StatusCreated, rrInit.Code)
		var initResp InitiateUploadResponse
		json.Unmarshal(rrInit.Body.Bytes(), &initResp)
		uploadID := initResp.UploadID

		reqChunk := httptest.NewRequest("PATCH", "/api/v1/nodes/upload/"+uploadID, strings.NewReader("some data"))
		reqChunk.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		reqChunk.Header.Set("Content-Range", "bytes 10-18/100")
		rrChunk := httptest.NewRecorder()
		router.ServeHTTP(rrChunk, reqChunk)

		require.Equal(t, http.StatusRequestedRangeNotSatisfiable, rrChunk.Code)
	})

	t.Run("complete fails if upload is incomplete", func(t *testing.T) {
		createTestUserWithPassword(t, "user_chunk_incomplete", "password")
		loginResp := loginUserForTest(t, "user_chunk_incomplete", "password")

		initReqPayload := InitiateUploadRequest{Name: "incomplete.txt", Size: 1000}
		body, _ := json.Marshal(initReqPayload)
		reqInit := httptest.NewRequest("POST", "/api/v1/nodes/upload/initiate", bytes.NewReader(body))
		reqInit.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrInit := httptest.NewRecorder()
		router.ServeHTTP(rrInit, reqInit)
		require.Equal(t, http.StatusCreated, rrInit.Code)
		var initResp InitiateUploadResponse
		json.Unmarshal(rrInit.Body.Bytes(), &initResp)
		uploadID := initResp.UploadID

		reqChunk := httptest.NewRequest("PATCH", "/api/v1/nodes/upload/"+uploadID, strings.NewReader("some data"))
		reqChunk.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		reqChunk.Header.Set("Content-Range", "bytes 0-8/1000")
		router.ServeHTTP(httptest.NewRecorder(), reqChunk)

		reqComplete := httptest.NewRequest("POST", "/api/v1/nodes/upload/"+uploadID+"/complete", nil)
		reqComplete.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrComplete := httptest.NewRecorder()
		router.ServeHTTP(rrComplete, reqComplete)

		require.Equal(t, http.StatusBadRequest, rrComplete.Code)
		require.Contains(t, rrComplete.Body.String(), "upload is incomplete")
	})

	t.Run("initiate fails on name conflict", func(t *testing.T) {
		user := createTestUserWithPassword(t, "user_chunk_conflict", "password")
		loginResp := loginUserForTest(t, "user_chunk_conflict", "password")

		existingFileName := "conflict_upload.txt"
		createTestNodeAPI(t, existingFileName, "file", nil, user.ID)

		initReqPayload := InitiateUploadRequest{Name: existingFileName, Size: 100}
		body, _ := json.Marshal(initReqPayload)
		reqInit := httptest.NewRequest("POST", "/api/v1/nodes/upload/initiate", bytes.NewReader(body))
		reqInit.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rrInit := httptest.NewRecorder()
		router.ServeHTTP(rrInit, reqInit)

		require.Equal(t, http.StatusConflict, rrInit.Code)
	})
}

func TestListOutgoingSharedNodesHandler(t *testing.T) {
	sharer := createTestUserWithPassword(t, "user_outgoing_sharer", "password")
	recipient1 := createTestUserWithPassword(t, "user_outgoing_rec1", "password")
	recipient2 := createTestUserWithPassword(t, "user_outgoing_rec2", "password")
	loginResp := loginUserForTest(t, "user_outgoing_sharer", "password")

	nodeA, _ := createTestNodeAPI(t, "A_Shared_Twice", "file", nil, sharer.ID)
	nodeZ, _ := createTestNodeAPI(t, "Z_Shared_Once", "file", nil, sharer.ID)
	createTestNodeAPI(t, "Not_Shared", "file", nil, sharer.ID)

	testServer.store.ShareNode(context.Background(), database.ShareNodeParams{NodeID: nodeA.ID, SharerID: sharer.ID, RecipientID: recipient1.ID, Permissions: "read"})
	testServer.store.ShareNode(context.Background(), database.ShareNodeParams{NodeID: nodeA.ID, SharerID: sharer.ID, RecipientID: recipient2.ID, Permissions: "read"})
	testServer.store.ShareNode(context.Background(), database.ShareNodeParams{NodeID: nodeZ.ID, SharerID: sharer.ID, RecipientID: recipient1.ID, Permissions: "read"})

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/shares/outgoing/nodes", testServer.ListOutgoingSharedNodesHandler)

	t.Run("returns unique shared nodes with default sort", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/shares/outgoing/nodes", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []*models.RichNode
		json.Unmarshal(rr.Body.Bytes(), &nodes)

		require.Len(t, nodes, 2, "Should return 2 unique nodes")
		require.Equal(t, "A_Shared_Twice", nodes[0].Name)
		require.Equal(t, "Z_Shared_Once", nodes[1].Name)
		require.True(t, nodes[0].IsShared)
	})

	t.Run("returns nodes sorted by name descending", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/shares/outgoing/nodes?sort=-name", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []*models.RichNode
		json.Unmarshal(rr.Body.Bytes(), &nodes)

		require.Len(t, nodes, 2)
		require.Equal(t, "Z_Shared_Once", nodes[0].Name)
		require.Equal(t, "A_Shared_Twice", nodes[1].Name)
	})
}

func TestInputValidation(t *testing.T) {
	createTestUserWithPassword(t, "user_validation", "password")
	loginResp := loginUserForTest(t, "user_validation", "password")

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Post("/api/v1/nodes/folder", testServer.CreateFolderHandler)
	router.Get("/api/v1/nodes", testServer.ListNodesHandler)

	t.Run("rejects overly long names", func(t *testing.T) {
		longName := strings.Repeat("a", 256)
		payload := CreateFolderRequest{Name: longName}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/api/v1/nodes/folder", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
	})

	t.Run("handles invalid pagination parameters gracefully", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/nodes?limit=-10", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		req = httptest.NewRequest("GET", "/api/v1/nodes?offset=abc", nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
	})
}

func TestPermissions_NestedShare(t *testing.T) {
	sharer := createTestUserWithPassword(t, "user_nested_share_owner", "password")
	recipient := createTestUserWithPassword(t, "user_nested_share_recipient", "password")
	recipientLogin := loginUserForTest(t, "user_nested_share_recipient", "password")

	parent, _ := createTestNodeAPI(t, "Parent", "folder", nil, sharer.ID)
	child, _ := createTestNodeAPI(t, "Child", "folder", &parent.ID, sharer.ID)
	grandchild, _ := createTestNodeAPI(t, "Grandchild", "file", &child.ID, sharer.ID)

	_, err := testServer.store.ShareNode(context.Background(), database.ShareNodeParams{
		NodeID: parent.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "read",
	})
	require.NoError(t, err)

	_, err = testServer.store.ShareNode(context.Background(), database.ShareNodeParams{
		NodeID: child.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "write",
	})
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/nodes/{nodeId}", testServer.GetNodeHandler)

	t.Run("more specific permission (write) overrides parent permission (read)", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s", grandchild.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp NodeDetailResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		require.NotNil(t, resp.Permissions)
		require.Equal(t, "write", *resp.Permissions, "Permission for Grandchild should be 'write', inherited from Child")
	})

	t.Run("less specific permission (read) is used when no other applies", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/nodes/%s", parent.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+recipientLogin.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var resp NodeDetailResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		require.NotNil(t, resp.Permissions)
		require.Equal(t, "read", *resp.Permissions, "Permission for Parent should be 'read'")
	})
}

func TestAuthMiddleware(t *testing.T) {
	router := chi.NewRouter()
	router.With(testServer.AuthMiddleware).Get("/api/v1/me", testServer.GetCurrentUserHandler)

	t.Run("request with no token returns 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("request with invalid token format returns 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearerinvalid_token")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("request with token signed by wrong secret returns 401", func(t *testing.T) {
		user := &models.User{ID: 999, Username: "fakeuser"}
		wrongToken, err := auth.GenerateJWT(user, "this_is_a_wrong_secret", uuid.New())
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+wrongToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("request with expired token returns 401", func(t *testing.T) {
		secret := testServer.config.JWT.Secret
		expirationTime := time.Now().Add(1 * time.Nanosecond)
		claims := &auth.AppClaims{
			UserID: 1, Username: "expired_user", SessionID: uuid.New(),
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(expirationTime),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		expiredToken, err := token.SignedString([]byte(secret))
		require.NoError(t, err)

		time.Sleep(5 * time.Millisecond)

		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+expiredToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})
}

func TestRateLimiting(t *testing.T) {
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		r.Use(httprate.LimitByIP(60, 1*time.Second))

		r.With(testServer.AuthMiddleware).Get("/me", testServer.GetCurrentUserHandler)

		r.Group(func(r chi.Router) {
			r.Use(httprate.LimitByIP(5, 10*time.Second))
			r.Post("/auth/login", testServer.LoginHandler)
		})
	})

	t.Run("allows requests within the general limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+testUserToken)

		for i := 0; i < 10; i++ {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			require.Equal(t, http.StatusOK, rr.Code, "Request %d should be allowed", i+1)
		}
	})

	t.Run("blocks requests exceeding the login rate limit", func(t *testing.T) {
		loginReq := LoginRequest{Username: "any_user", Password: "any_password"}
		body, _ := json.Marshal(loginReq)

		for i := 0; i < 5; i++ {
			req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			require.NotEqual(t, http.StatusTooManyRequests, rr.Code, "Request %d should not be rate limited", i+1)
		}

		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusTooManyRequests, rr.Code, "The 6th request should be rate limited")

		time.Sleep(10 * time.Second)

		reqAfterWait := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
		rrAfterWait := httptest.NewRecorder()
		router.ServeHTTP(rrAfterWait, reqAfterWait)
		require.NotEqual(t, http.StatusTooManyRequests, rrAfterWait.Code, "Request after waiting period should be allowed")
	})
}

func TestListWriteableSharedFoldersHandler(t *testing.T) {
	sharer := createTestUserWithPassword(t, "user_api_writeable", "password")
	recipient := createTestUserWithPassword(t, "recipient_api_writeable", "password")
	loginResp := loginUserForTest(t, "recipient_api_writeable", "password")

	writeFolder, _ := createTestNodeAPI(t, "API Writeable", "folder", nil, sharer.ID)
	readFolder, _ := createTestNodeAPI(t, "API Readable", "folder", nil, sharer.ID)
	createTestNodeAPI(t, "API Subfolder", "folder", &writeFolder.ID, sharer.ID)

	testServer.store.ShareNode(context.Background(), database.ShareNodeParams{NodeID: writeFolder.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "write"})
	testServer.store.ShareNode(context.Background(), database.ShareNodeParams{NodeID: readFolder.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "read"})

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/shares/incoming/writeable-folders", testServer.ListWriteableSharedFoldersHandler)

	t.Run("lists root writeable folders from a specific sharer", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/shares/incoming/writeable-folders?sharer_username=%s", sharer.Username)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []*models.RichNode
		json.Unmarshal(rr.Body.Bytes(), &nodes)

		require.Len(t, nodes, 1)
		require.Equal(t, "API Writeable", nodes[0].Name)
	})

	t.Run("lists subfolders within a writeable folder", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/shares/incoming/writeable-folders?sharer_username=%s&parent_id=%s", sharer.Username, writeFolder.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var nodes []*models.RichNode
		json.Unmarshal(rr.Body.Bytes(), &nodes)

		require.Len(t, nodes, 1)
		require.Equal(t, "API Subfolder", nodes[0].Name)
	})

	t.Run("returns forbidden when trying to list content of a read-only folder", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/shares/incoming/writeable-folders?sharer_username=%s&parent_id=%s", sharer.Username, readFolder.ID)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("returns not found for a non-existent parent folder", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/shares/incoming/writeable-folders?sharer_username=%s&parent_id=non_existent_id_123", sharer.Username)
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestSessionIdentifier_Integration(t *testing.T) {
	createTestUserWithPassword(t, "user_session_id", "password")
	loginResp := loginUserForTest(t, "user_session_id", "password")

	claims, err := auth.VerifyJWT(loginResp.AccessToken, testServer.config.JWT.Secret)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, claims.SessionID, "Token should contain a valid SessionID (jti)")

	router := chi.NewRouter()
	router.Use(testServer.AuthMiddleware)
	router.Get("/api/v1/sessions", testServer.ListSessionsHandler)

	req := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var sessions []models.Session
	err = json.Unmarshal(rr.Body.Bytes(), &sessions)
	require.NoError(t, err)

	require.NotEmpty(t, sessions)
	found := false
	for _, s := range sessions {
		if s.ID == claims.SessionID {
			found = true
			break
		}
	}
	require.True(t, found, "The session ID from the JWT claim should match a session ID in the database")
}
