package database

import (
	"context"
	"serwer-plikow/internal/models"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Funkcja pomocnicza do tworzenia użytkownika na potrzeby testów
func createTestUserForNodes(t *testing.T, username string) int64 {
	var userID int64
	query := `INSERT INTO users (username, password_hash, display_name) VALUES ($1, 'hash', 'Node Test User') RETURNING id`
	// Używamy unikalnej nazwy użytkownika, aby uniknąć konfliktów przy równoległym uruchamianiu testów
	err := testStore.pool.QueryRow(context.Background(), query, username).Scan(&userID)
	require.NoError(t, err)
	require.NotZero(t, userID)
	return userID
}

// Funkcja pomocnicza do tworzenia węzła (pliku/folderu)
func createTestNode(t *testing.T, params CreateNodeParams) *models.Node {
	node, err := testStore.CreateNode(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, node)
	return node
}

func TestCreateNode(t *testing.T) {
	ownerID := createTestUserForNodes(t, "user_create_node")

	params := CreateNodeParams{
		ID:       "test_folder_id_123",
		OwnerID:  ownerID,
		ParentID: nil,
		Name:     "Test Folder",
		NodeType: "folder",
	}

	createdNode, err := testStore.CreateNode(context.Background(), params)

	require.NoError(t, err)
	require.NotNil(t, createdNode)

	require.Equal(t, params.ID, createdNode.ID)
	require.Equal(t, params.OwnerID, createdNode.OwnerID)
	require.Equal(t, params.Name, createdNode.Name)
	require.Equal(t, params.NodeType, createdNode.NodeType)
	require.Nil(t, createdNode.ParentID)
	require.Nil(t, createdNode.SizeBytes)
	require.NotZero(t, createdNode.CreatedAt)
	require.NotZero(t, createdNode.ModifiedAt)

	var foundNode models.Node
	query := `SELECT id FROM nodes WHERE id = $1`
	err = testStore.pool.QueryRow(context.Background(), query, params.ID).Scan(&foundNode.ID)
	require.NoError(t, err)
	require.Equal(t, params.ID, foundNode.ID)
}

func TestMoveNodeToTrash(t *testing.T) {
	ownerID := createTestUserForNodes(t, "user_move_to_trash")

	// Arrange: Stwórz strukturę folder -> subfolder -> plik
	folder := createTestNode(t, CreateNodeParams{ID: "trash_test_folder", OwnerID: ownerID, Name: "Folder", NodeType: "folder"})
	subfolder := createTestNode(t, CreateNodeParams{ID: "trash_test_subfolder", OwnerID: ownerID, ParentID: &folder.ID, Name: "Subfolder", NodeType: "folder"})
	createTestNode(t, CreateNodeParams{ID: "trash_test_file", OwnerID: ownerID, ParentID: &subfolder.ID, Name: "plik.txt", NodeType: "file"})

	// Act: Przenieś główny folder do kosza
	success, err := testStore.MoveNodeToTrash(context.Background(), folder.ID, ownerID)

	// Assert: Sprawdź, czy operacja się powiodła i czy wszystkie elementy są w koszu
	require.NoError(t, err)
	require.True(t, success, "MoveNodeToTrash should return true on success")

	var count int
	query := `SELECT count(*) FROM nodes WHERE id IN ($1, $2, $3) AND deleted_at IS NOT NULL`
	err = testStore.pool.QueryRow(context.Background(), query, "trash_test_folder", "trash_test_subfolder", "trash_test_file").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 3, count, "Expected 3 nodes (folder, subfolder, file) to be in trash")

	// Sprawdź, czy `original_parent_id` zostało poprawnie ustawione dla dziecka
	var originalParentID *string
	query = `SELECT original_parent_id FROM nodes WHERE id = $1`
	err = testStore.pool.QueryRow(context.Background(), query, subfolder.ID).Scan(&originalParentID)
	require.NoError(t, err)
	require.NotNil(t, originalParentID)
	require.Equal(t, folder.ID, *originalParentID)

	// Act/Assert: próba usunięcia nieistniejącego elementu
	success, err = testStore.MoveNodeToTrash(context.Background(), "non_existent_id", ownerID)
	require.NoError(t, err)
	require.False(t, success, "MoveNodeToTrash should return false for a non-existent node")
}

func TestMoveNode(t *testing.T) {
	ownerID := createTestUserForNodes(t, "user_move_node")
	folder1 := createTestNode(t, CreateNodeParams{ID: "move_folder1", OwnerID: ownerID, Name: "Folder 1", NodeType: "folder"})
	folder2 := createTestNode(t, CreateNodeParams{ID: "move_folder2", OwnerID: ownerID, Name: "Folder 2", NodeType: "folder"})
	nodeToMove := createTestNode(t, CreateNodeParams{ID: "node_to_move", OwnerID: ownerID, ParentID: &folder1.ID, Name: "File to Move", NodeType: "file"})

	// Act: Przenieś plik z folder1 do folder2
	success, err := testStore.MoveNode(context.Background(), nodeToMove.ID, ownerID, &folder2.ID)

	// Assert: Sprawdź, czy operacja się powiodła i czy plik ma nowego rodzica
	require.NoError(t, err)
	require.True(t, success)

	movedNode, err := testStore.GetNodeByID(context.Background(), nodeToMove.ID, ownerID)
	require.NoError(t, err)
	require.NotNil(t, movedNode.ParentID)
	require.Equal(t, folder2.ID, *movedNode.ParentID)

	// Act/Assert: Próba przeniesienia do nieistniejącego folderu
	nonExistentParentID := "non_existent_folder_x"
	success, err = testStore.MoveNode(context.Background(), nodeToMove.ID, ownerID, &nonExistentParentID)
	require.Error(t, err) // Oczekujemy błędu (foreign key violation)
	require.False(t, success)
	require.Contains(t, err.Error(), "target folder does not exist")
}

func TestGetNodesByParentID(t *testing.T) {
	ownerID := createTestUserForNodes(t, "user_get_nodes")

	// Arrange: Stwórz pliki w katalogu głównym (parent_id = NULL)
	createTestNode(t, CreateNodeParams{ID: "get_nodes_root_file1", OwnerID: ownerID, Name: "A_Root File", NodeType: "file"})
	createTestNode(t, CreateNodeParams{ID: "get_nodes_root_folder", OwnerID: ownerID, Name: "Z_Root Folder", NodeType: "folder"})

	// Arrange: Stwórz pliki w podfolderze
	parentFolder := createTestNode(t, CreateNodeParams{ID: "get_nodes_parent", OwnerID: ownerID, Name: "Parent", NodeType: "folder"})
	createTestNode(t, CreateNodeParams{ID: "get_nodes_child_file", OwnerID: ownerID, ParentID: &parentFolder.ID, Name: "Child File", NodeType: "file"})

	// Test 1: Pobieranie z katalogu głównego
	rootNodes, err := testStore.GetNodesByParentID(context.Background(), ownerID, nil)
	require.NoError(t, err)
	require.Len(t, rootNodes, 3) // Folder "Parent" też jest w roocie
	// Sprawdź sortowanie (foldery najpierw, potem alfabetycznie)
	require.Equal(t, "Parent", rootNodes[0].Name)
	require.Equal(t, "Z_Root Folder", rootNodes[1].Name)
	require.Equal(t, "A_Root File", rootNodes[2].Name)

	// Test 2: Pobieranie z podfolderu
	childNodes, err := testStore.GetNodesByParentID(context.Background(), ownerID, &parentFolder.ID)
	require.NoError(t, err)
	require.Len(t, childNodes, 1)
	require.Equal(t, "Child File", childNodes[0].Name)

	// Test 3: Pobieranie z pustego folderu
	emptyFolder := createTestNode(t, CreateNodeParams{ID: "get_nodes_empty", OwnerID: ownerID, Name: "Empty", NodeType: "folder"})
	emptyNodes, err := testStore.GetNodesByParentID(context.Background(), ownerID, &emptyFolder.ID)
	require.NoError(t, err)
	require.Len(t, emptyNodes, 0)
}

func TestNodeExists(t *testing.T) {
	ownerID := createTestUserForNodes(t, "user_node_exists")
	node := createTestNode(t, CreateNodeParams{ID: "existing_node", OwnerID: ownerID, Name: "Existing", NodeType: "file"})

	// Test 1: Sprawdź istniejący węzeł
	exists, err := testStore.NodeExists(context.Background(), node.ID)
	require.NoError(t, err)
	require.True(t, exists)

	// Test 2: Sprawdź nieistniejący węzeł
	exists, err = testStore.NodeExists(context.Background(), "non_existent_node")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestGetNodeByID(t *testing.T) {
	ownerID := createTestUserForNodes(t, "user_get_by_id")
	otherOwnerID := createTestUserForNodes(t, "other_user_get_by_id")
	node := createTestNode(t, CreateNodeParams{ID: "get_by_id_node", OwnerID: ownerID, Name: "My Node", NodeType: "file"})

	// Test 1: Właściciel pobiera swój węzeł
	foundNode, err := testStore.GetNodeByID(context.Background(), node.ID, ownerID)
	require.NoError(t, err)
	require.NotNil(t, foundNode)
	require.Equal(t, node.ID, foundNode.ID)

	// Test 2: Inny użytkownik próbuje pobrać nie swój węzeł
	foundNode, err = testStore.GetNodeByID(context.Background(), node.ID, otherOwnerID)
	require.NoError(t, err)
	require.Nil(t, foundNode, "Should not find a node belonging to another user")

	// Test 3: Próba pobrania nieistniejącego węzła
	foundNode, err = testStore.GetNodeByID(context.Background(), "non_existent_node", ownerID)
	require.NoError(t, err)
	require.Nil(t, foundNode)
}

func TestRestoreNode(t *testing.T) {
	ownerID := createTestUserForNodes(t, "user_restore_node")
	parentFolder := createTestNode(t, CreateNodeParams{ID: "restore_parent", OwnerID: ownerID, Name: "Parent", NodeType: "folder"})
	nodeToTrash := createTestNode(t, CreateNodeParams{ID: "node_to_restore", OwnerID: ownerID, ParentID: &parentFolder.ID, Name: "File to Restore", NodeType: "file"})

	// Przenieś do kosza
	_, err := testStore.MoveNodeToTrash(context.Background(), nodeToTrash.ID, ownerID)
	require.NoError(t, err)

	// Sprawdź, czy jest w koszu
	var deletedAt *time.Time
	err = testStore.pool.QueryRow(context.Background(), `SELECT deleted_at FROM nodes WHERE id=$1`, nodeToTrash.ID).Scan(&deletedAt)
	require.NoError(t, err)
	require.NotNil(t, deletedAt)

	// Act: Przywróć węzeł
	success, err := testStore.RestoreNode(context.Background(), nodeToTrash.ID, ownerID)
	require.NoError(t, err)
	require.True(t, success)

	// Assert: Sprawdź, czy węzeł został przywrócony
	restoredNode, err := testStore.GetNodeByID(context.Background(), nodeToTrash.ID, ownerID)
	require.NoError(t, err)
	require.NotNil(t, restoredNode)
	require.NotNil(t, restoredNode.ParentID)
	require.Equal(t, parentFolder.ID, *restoredNode.ParentID) // Sprawdź, czy wrócił do oryginalnego rodzica

	// Test konfliktu nazw przy przywracaniu
	nodeToTrashAgain := createTestNode(t, CreateNodeParams{ID: "conflicting_node_newx", OwnerID: ownerID, ParentID: &parentFolder.ID, Name: "Conflicting Name", NodeType: "file"})
	_, err = testStore.MoveNodeToTrash(context.Background(), nodeToTrashAgain.ID, ownerID)
	require.NoError(t, err)
	// Stwórz nowy plik o tej samej nazwie w tym samym miejscu
	createTestNode(t, CreateNodeParams{ID: "conflicting_node_new", OwnerID: ownerID, ParentID: &parentFolder.ID, Name: "Conflicting Name", NodeType: "file"})

	// Próba przywrócenia powinna się nie udać z błędem duplikatu
	success, err = testStore.RestoreNode(context.Background(), nodeToTrashAgain.ID, ownerID)
	require.Error(t, err)
	require.False(t, success)
	require.ErrorIs(t, err, ErrDuplicateNodeName)
}

func TestGetNodeIfAccessible(t *testing.T) {
	ownerID := createTestUserForNodes(t, "user_access_owner")
	recipientID := createTestUserForNodes(t, "user_access_recipient")
	otherUserID := createTestUserForNodes(t, "user_access_other")

	// Węzły
	ownedNode := createTestNode(t, CreateNodeParams{ID: "access_owned", OwnerID: ownerID, Name: "Owned File", NodeType: "file"})
	sharedFolder := createTestNode(t, CreateNodeParams{ID: "access_shared_folder", OwnerID: ownerID, Name: "Shared Folder", NodeType: "folder"})
	nodeInShared := createTestNode(t, CreateNodeParams{ID: "access_in_shared", OwnerID: ownerID, ParentID: &sharedFolder.ID, Name: "File in Shared", NodeType: "file"})
	unrelatedNode := createTestNode(t, CreateNodeParams{ID: "access_unrelated", OwnerID: ownerID, Name: "Unrelated File", NodeType: "file"})

	// Udostępnij folder
	createTestShare(t, ShareNodeParams{NodeID: sharedFolder.ID, SharerID: ownerID, RecipientID: recipientID, Permissions: "read"})

	// Test 1: Właściciel pobiera swój własny plik
	node, err := testStore.GetNodeIfAccessible(context.Background(), ownedNode.ID, ownerID)
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Equal(t, ownedNode.ID, node.ID)

	// Test 2: Odbiorca pobiera plik w udostępnionym folderze
	node, err = testStore.GetNodeIfAccessible(context.Background(), nodeInShared.ID, recipientID)
	require.NoError(t, err)
	require.NotNil(t, node, "Recipient should be able to access a node within a shared folder")
	require.Equal(t, nodeInShared.ID, node.ID)

	// Test 3: Odbiorca próbuje pobrać plik, do którego nie ma dostępu
	node, err = testStore.GetNodeIfAccessible(context.Background(), unrelatedNode.ID, recipientID)
	require.NoError(t, err)
	require.Nil(t, node, "Recipient should not be able to access an unshared node")

	// Test 4: Inny, zupełnie niepowiązany użytkownik, próbuje pobrać plik
	node, err = testStore.GetNodeIfAccessible(context.Background(), ownedNode.ID, otherUserID)
	require.NoError(t, err)
	require.Nil(t, node, "A random user should not have access")

	// Test 5: Próba pobrania nieistniejącego węzła
	node, err = testStore.GetNodeIfAccessible(context.Background(), "non_existent_node", ownerID)
	require.NoError(t, err)
	require.Nil(t, node)
}
