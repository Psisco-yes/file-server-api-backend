package database

import (
	"context"
	"encoding/json"
	"fmt"
	"serwer-plikow/internal/auth"
	"serwer-plikow/internal/models"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func createTestUser(t *testing.T, username string) *models.User {
	var user models.User
	query := `INSERT INTO users (username, password_hash, display_name) VALUES ($1, 'hash', $2) 
			  RETURNING id, username, password_hash, display_name, created_at, storage_quota_bytes, storage_used_bytes`
	err := testStore.pool.QueryRow(context.Background(), query, username, fmt.Sprintf("User %s", username)).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.CreatedAt,
		&user.StorageQuotaBytes, &user.StorageUsedBytes,
	)
	require.NoError(t, err)
	return &user
}

func createTestNode(t *testing.T, params CreateNodeParams) *models.Node {
	node, err := testStore.CreateNode(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, node)
	return node
}

func createTestShare(t *testing.T, params ShareNodeParams) *models.Share {
	share, err := testStore.ShareNode(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, share)
	return share
}

func TestAddFavorite(t *testing.T) {
	user := createTestUser(t, "user_fav_add")
	otherUser := createTestUser(t, "other_user_fav_add")
	node := createTestNode(t, CreateNodeParams{ID: "fav_node_1", OwnerID: user.ID, Name: "My Fav File", NodeType: "file"})
	sharedFolder := createTestNode(t, CreateNodeParams{ID: "fav_shared_folder", OwnerID: otherUser.ID, Name: "Shared Folder", NodeType: "folder"})
	nodeInSharedFolder := createTestNode(t, CreateNodeParams{ID: "fav_node_in_shared", OwnerID: otherUser.ID, ParentID: &sharedFolder.ID, Name: "File in Shared", NodeType: "file"})

	err := testStore.AddFavorite(context.Background(), user.ID, node.ID)
	require.NoError(t, err)

	err = testStore.AddFavorite(context.Background(), user.ID, node.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrFavoriteAlreadyExists)

	err = testStore.AddFavorite(context.Background(), user.ID, "non_existent_node")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNodeNotFound)

	createTestShare(t, ShareNodeParams{NodeID: sharedFolder.ID, SharerID: otherUser.ID, RecipientID: user.ID, Permissions: "read"})
	err = testStore.AddFavorite(context.Background(), user.ID, nodeInSharedFolder.ID)
	require.NoError(t, err, "Should be able to favorite an accessible shared node")
}

func TestRemoveFavorite(t *testing.T) {
	user := createTestUser(t, "user_fav_remove")
	node := createTestNode(t, CreateNodeParams{ID: "fav_node_2", OwnerID: user.ID, Name: "File to Unfavorite", NodeType: "file"})

	err := testStore.AddFavorite(context.Background(), user.ID, node.ID)
	require.NoError(t, err)

	success, err := testStore.RemoveFavorite(context.Background(), user.ID, node.ID)
	require.NoError(t, err)
	require.True(t, success)

	var count int
	err = testStore.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_favorites WHERE user_id=$1 AND node_id=$2`, user.ID, node.ID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count)

	success, err = testStore.RemoveFavorite(context.Background(), user.ID, node.ID)
	require.NoError(t, err)
	require.False(t, success)
}

func TestListFavorites(t *testing.T) {
	user := createTestUser(t, "user_fav_list")
	otherUser := createTestUser(t, "other_user_fav_list")

	node1 := createTestNode(t, CreateNodeParams{ID: "fav_list_1", OwnerID: user.ID, Name: "A_My File", NodeType: "file"})
	node2_shared := createTestNode(t, CreateNodeParams{ID: "fav_list_2", OwnerID: otherUser.ID, Name: "B_Shared File", NodeType: "file"})
	node3_trashed := createTestNode(t, CreateNodeParams{ID: "fav_list_3", OwnerID: user.ID, Name: "C_Trashed Fav", NodeType: "file"})

	createTestShare(t, ShareNodeParams{NodeID: node2_shared.ID, SharerID: otherUser.ID, RecipientID: user.ID, Permissions: "read"})

	err := testStore.AddFavorite(context.Background(), user.ID, node1.ID)
	require.NoError(t, err)
	err = testStore.AddFavorite(context.Background(), user.ID, node2_shared.ID)
	require.NoError(t, err)
	err = testStore.AddFavorite(context.Background(), user.ID, node3_trashed.ID)
	require.NoError(t, err)

	_, err = testStore.MoveNodeToTrash(context.Background(), node3_trashed.ID, user.ID)
	require.NoError(t, err)

	favorites, err := testStore.ListFavorites(context.Background(), user.ID, 100, 0)
	require.NoError(t, err)

	require.Len(t, favorites, 2)
	require.Equal(t, "A_My File", favorites[0].Name)
	require.Equal(t, "B_Shared File", favorites[1].Name)
}

func TestCreateNode(t *testing.T) {
	owner := createTestUser(t, "user_create_node")

	params := CreateNodeParams{
		ID:       "test_folder_id_123",
		OwnerID:  owner.ID,
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
	owner := createTestUser(t, "user_move_to_trash")

	folder := createTestNode(t, CreateNodeParams{ID: "trash_test_folder", OwnerID: owner.ID, Name: "Folder", NodeType: "folder"})
	subfolder := createTestNode(t, CreateNodeParams{ID: "trash_test_subfolder", OwnerID: owner.ID, ParentID: &folder.ID, Name: "Subfolder", NodeType: "folder"})
	createTestNode(t, CreateNodeParams{ID: "trash_test_file", OwnerID: owner.ID, ParentID: &subfolder.ID, Name: "plik.txt", NodeType: "file"})

	success, err := testStore.MoveNodeToTrash(context.Background(), folder.ID, owner.ID)

	require.NoError(t, err)
	require.True(t, success, "MoveNodeToTrash should return true on success")

	var count int
	query := `SELECT count(*) FROM nodes WHERE id IN ($1, $2, $3) AND deleted_at IS NOT NULL`
	err = testStore.pool.QueryRow(context.Background(), query, "trash_test_folder", "trash_test_subfolder", "trash_test_file").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 3, count, "Expected 3 nodes (folder, subfolder, file) to be in trash")

	var originalParentID *string
	query = `SELECT original_parent_id FROM nodes WHERE id = $1`
	err = testStore.pool.QueryRow(context.Background(), query, subfolder.ID).Scan(&originalParentID)
	require.NoError(t, err)
	require.NotNil(t, originalParentID)
	require.Equal(t, folder.ID, *originalParentID)

	success, err = testStore.MoveNodeToTrash(context.Background(), "non_existent_id", owner.ID)
	require.NoError(t, err)
	require.False(t, success, "MoveNodeToTrash should return false for a non-existent node")
}

func TestMoveNode(t *testing.T) {
	owner := createTestUser(t, "user_move_node")
	folder1 := createTestNode(t, CreateNodeParams{ID: "move_folder1", OwnerID: owner.ID, Name: "Folder 1", NodeType: "folder"})
	folder2 := createTestNode(t, CreateNodeParams{ID: "move_folder2", OwnerID: owner.ID, Name: "Folder 2", NodeType: "folder"})
	nodeToMove := createTestNode(t, CreateNodeParams{ID: "node_to_move", OwnerID: owner.ID, ParentID: &folder1.ID, Name: "File to Move", NodeType: "file"})

	success, err := testStore.MoveNode(context.Background(), nodeToMove.ID, owner.ID, &folder2.ID)

	require.NoError(t, err)
	require.True(t, success)

	movedNode, err := testStore.GetNodeByID(context.Background(), nodeToMove.ID, owner.ID)
	require.NoError(t, err)
	require.NotNil(t, movedNode.ParentID)
	require.Equal(t, folder2.ID, *movedNode.ParentID)

	nonExistentParentID := "non_existent_folder_x"
	success, err = testStore.MoveNode(context.Background(), nodeToMove.ID, owner.ID, &nonExistentParentID)
	require.Error(t, err)
	require.False(t, success)
	require.Contains(t, err.Error(), "target folder does not exist")
}

func TestGetNodesByParentID(t *testing.T) {
	owner := createTestUser(t, "user_get_nodes")

	createTestNode(t, CreateNodeParams{ID: "get_nodes_root_file1", OwnerID: owner.ID, Name: "A_Root File", NodeType: "file"})
	createTestNode(t, CreateNodeParams{ID: "get_nodes_root_folder", OwnerID: owner.ID, Name: "Z_Root Folder", NodeType: "folder"})

	parentFolder := createTestNode(t, CreateNodeParams{ID: "get_nodes_parent", OwnerID: owner.ID, Name: "Parent", NodeType: "folder"})
	createTestNode(t, CreateNodeParams{ID: "get_nodes_child_file", OwnerID: owner.ID, ParentID: &parentFolder.ID, Name: "Child File", NodeType: "file"})

	rootNodes, err := testStore.GetNodesByParentID(context.Background(), owner.ID, nil, 100, 0)
	require.NoError(t, err)
	require.Len(t, rootNodes, 3)
	require.Equal(t, "Parent", rootNodes[0].Name)
	require.Equal(t, "Z_Root Folder", rootNodes[1].Name)
	require.Equal(t, "A_Root File", rootNodes[2].Name)

	childNodes, err := testStore.GetNodesByParentID(context.Background(), owner.ID, &parentFolder.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, childNodes, 1)
	require.Equal(t, "Child File", childNodes[0].Name)

	emptyFolder := createTestNode(t, CreateNodeParams{ID: "get_nodes_empty", OwnerID: owner.ID, Name: "Empty", NodeType: "folder"})
	emptyNodes, err := testStore.GetNodesByParentID(context.Background(), owner.ID, &emptyFolder.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, emptyNodes, 0)
}

func TestNodeExists(t *testing.T) {
	owner := createTestUser(t, "user_node_exists")
	node := createTestNode(t, CreateNodeParams{ID: "existing_node", OwnerID: owner.ID, Name: "Existing", NodeType: "file"})

	exists, err := testStore.NodeExists(context.Background(), node.ID)
	require.NoError(t, err)
	require.True(t, exists)

	exists, err = testStore.NodeExists(context.Background(), "non_existent_node")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestGetNodeByID(t *testing.T) {
	owner := createTestUser(t, "user_get_by_id")
	otherOwner := createTestUser(t, "other_user_get_by_id")
	node := createTestNode(t, CreateNodeParams{ID: "get_by_id_node", OwnerID: owner.ID, Name: "My Node", NodeType: "file"})

	foundNode, err := testStore.GetNodeByID(context.Background(), node.ID, owner.ID)
	require.NoError(t, err)
	require.NotNil(t, foundNode)
	require.Equal(t, node.ID, foundNode.ID)

	foundNode, err = testStore.GetNodeByID(context.Background(), node.ID, otherOwner.ID)
	require.NoError(t, err)
	require.Nil(t, foundNode, "Should not find a node belonging to another user")

	foundNode, err = testStore.GetNodeByID(context.Background(), "non_existent_node", owner.ID)
	require.NoError(t, err)
	require.Nil(t, foundNode)
}

func TestRestoreNode(t *testing.T) {
	owner := createTestUser(t, "user_restore_node")
	parentFolder := createTestNode(t, CreateNodeParams{ID: "restore_parent", OwnerID: owner.ID, Name: "Parent", NodeType: "folder"})
	nodeToTrash := createTestNode(t, CreateNodeParams{ID: "node_to_restore", OwnerID: owner.ID, ParentID: &parentFolder.ID, Name: "File to Restore", NodeType: "file"})

	_, err := testStore.MoveNodeToTrash(context.Background(), nodeToTrash.ID, owner.ID)
	require.NoError(t, err)

	var deletedAt *time.Time
	err = testStore.pool.QueryRow(context.Background(), `SELECT deleted_at FROM nodes WHERE id=$1`, nodeToTrash.ID).Scan(&deletedAt)
	require.NoError(t, err)
	require.NotNil(t, deletedAt)

	success, err := testStore.RestoreNode(context.Background(), nodeToTrash.ID, owner.ID)
	require.NoError(t, err)
	require.True(t, success)

	restoredNode, err := testStore.GetNodeByID(context.Background(), nodeToTrash.ID, owner.ID)
	require.NoError(t, err)
	require.NotNil(t, restoredNode)
	require.NotNil(t, restoredNode.ParentID)
	require.Equal(t, parentFolder.ID, *restoredNode.ParentID)

	nodeToTrashAgain := createTestNode(t, CreateNodeParams{ID: "conflicting_node_newx", OwnerID: owner.ID, ParentID: &parentFolder.ID, Name: "Conflicting Name", NodeType: "file"})
	_, err = testStore.MoveNodeToTrash(context.Background(), nodeToTrashAgain.ID, owner.ID)
	require.NoError(t, err)
	createTestNode(t, CreateNodeParams{ID: "conflicting_node_new", OwnerID: owner.ID, ParentID: &parentFolder.ID, Name: "Conflicting Name", NodeType: "file"})

	success, err = testStore.RestoreNode(context.Background(), nodeToTrashAgain.ID, owner.ID)
	require.Error(t, err)
	require.False(t, success)
	require.ErrorIs(t, err, ErrDuplicateNodeName)
}

func TestGetNodeIfAccessible(t *testing.T) {
	owner := createTestUser(t, "user_access_owner")
	recipient := createTestUser(t, "user_access_recipient")
	otherUser := createTestUser(t, "user_access_other")

	ownedNode := createTestNode(t, CreateNodeParams{ID: "access_owned", OwnerID: owner.ID, Name: "Owned File", NodeType: "file"})
	sharedFolder := createTestNode(t, CreateNodeParams{ID: "access_shared_folder", OwnerID: owner.ID, Name: "Shared Folder", NodeType: "folder"})
	nodeInShared := createTestNode(t, CreateNodeParams{ID: "access_in_shared", OwnerID: owner.ID, ParentID: &sharedFolder.ID, Name: "File in Shared", NodeType: "file"})
	unrelatedNode := createTestNode(t, CreateNodeParams{ID: "access_unrelated", OwnerID: owner.ID, Name: "Unrelated File", NodeType: "file"})

	createTestShare(t, ShareNodeParams{NodeID: sharedFolder.ID, SharerID: owner.ID, RecipientID: recipient.ID, Permissions: "read"})

	node, err := testStore.GetNodeIfAccessible(context.Background(), ownedNode.ID, owner.ID)
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Equal(t, ownedNode.ID, node.ID)

	node, err = testStore.GetNodeIfAccessible(context.Background(), nodeInShared.ID, recipient.ID)
	require.NoError(t, err)
	require.NotNil(t, node, "Recipient should be able to access a node within a shared folder")
	require.Equal(t, nodeInShared.ID, node.ID)

	node, err = testStore.GetNodeIfAccessible(context.Background(), unrelatedNode.ID, recipient.ID)
	require.NoError(t, err)
	require.Nil(t, node, "Recipient should not be able to access an unshared node")

	node, err = testStore.GetNodeIfAccessible(context.Background(), ownedNode.ID, otherUser.ID)
	require.NoError(t, err)
	require.Nil(t, node, "A random user should not have access")

	node, err = testStore.GetNodeIfAccessible(context.Background(), "non_existent_node", owner.ID)
	require.NoError(t, err)
	require.Nil(t, node)
}

func TestShareNode(t *testing.T) {
	sharer := createTestUser(t, "sharer_user")
	recipient := createTestUser(t, "recipient_user")
	node := createTestNode(t, CreateNodeParams{ID: "share_node_1", OwnerID: sharer.ID, Name: "Shared File", NodeType: "file"})

	params := ShareNodeParams{
		NodeID:      node.ID,
		SharerID:    sharer.ID,
		RecipientID: recipient.ID,
		Permissions: "read",
	}

	share := createTestShare(t, params)

	require.Equal(t, params.NodeID, share.NodeID)
	require.Equal(t, params.SharerID, share.SharerID)
	require.Equal(t, params.RecipientID, share.RecipientID)
	require.Equal(t, params.Permissions, share.Permissions)
	require.NotZero(t, share.ID)
	require.NotZero(t, share.SharedAt)

	_, err := testStore.ShareNode(context.Background(), params)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrShareAlreadyExists)
}

func TestGetSharingUsers(t *testing.T) {
	recipient := createTestUser(t, "recipient_for_list")
	sharer1 := createTestUser(t, "sharer1_for_list")
	sharer2 := createTestUser(t, "sharer2_for_list")
	node1 := createTestNode(t, CreateNodeParams{ID: "share_list_node1", OwnerID: sharer1.ID, Name: "File 1", NodeType: "file"})
	node2 := createTestNode(t, CreateNodeParams{ID: "share_list_node2", OwnerID: sharer1.ID, Name: "File 2", NodeType: "file"})
	node3 := createTestNode(t, CreateNodeParams{ID: "share_list_node3", OwnerID: sharer2.ID, Name: "File 3", NodeType: "file"})

	createTestShare(t, ShareNodeParams{NodeID: node1.ID, SharerID: sharer1.ID, RecipientID: recipient.ID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node2.ID, SharerID: sharer1.ID, RecipientID: recipient.ID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node3.ID, SharerID: sharer2.ID, RecipientID: recipient.ID, Permissions: "read"})

	users, err := testStore.GetSharingUsers(context.Background(), recipient.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, users, 2)

	userMap := make(map[int64]string)
	for _, u := range users {
		userMap[u.ID] = u.Username
	}
	require.Equal(t, "sharer1_for_list", userMap[sharer1.ID])
	require.Equal(t, "sharer2_for_list", userMap[sharer2.ID])

	nobody := createTestUser(t, "nobody")
	users, err = testStore.GetSharingUsers(context.Background(), nobody.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, users, 0)
}

func TestListDirectlySharedNodes(t *testing.T) {
	recipient := createTestUser(t, "recipient_for_direct")
	sharer := createTestUser(t, "sharer_for_direct")
	otherSharer := createTestUser(t, "other_sharer_for_direct")
	node1 := createTestNode(t, CreateNodeParams{ID: "direct_share_node1", OwnerID: sharer.ID, Name: "A_File", NodeType: "file"})
	node2 := createTestNode(t, CreateNodeParams{ID: "direct_share_node2", OwnerID: sharer.ID, Name: "Z_Folder", NodeType: "folder"})
	node3 := createTestNode(t, CreateNodeParams{ID: "direct_share_node3", OwnerID: otherSharer.ID, Name: "Other File", NodeType: "file"})

	createTestShare(t, ShareNodeParams{NodeID: node1.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node2.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node3.ID, SharerID: otherSharer.ID, RecipientID: recipient.ID, Permissions: "read"})

	nodes, err := testStore.ListDirectlySharedNodes(context.Background(), recipient.ID, sharer.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, "Z_Folder", nodes[0].Name)
	require.Equal(t, "A_File", nodes[1].Name)
}

func TestHasAccessToNode(t *testing.T) {
	sharer := createTestUser(t, "h_sharer_for_access")
	recipient := createTestUser(t, "h_recipient_for_access")
	folder := createTestNode(t, CreateNodeParams{ID: "h_access_folder", OwnerID: sharer.ID, Name: "Parent", NodeType: "folder"})
	subFolder := createTestNode(t, CreateNodeParams{ID: "h_access_subfolder", OwnerID: sharer.ID, ParentID: &folder.ID, Name: "Child", NodeType: "folder"})
	file := createTestNode(t, CreateNodeParams{ID: "h_access_file", OwnerID: sharer.ID, ParentID: &subFolder.ID, Name: "file.txt", NodeType: "file"})
	unrelatedNode := createTestNode(t, CreateNodeParams{ID: "h_access_unrelated", OwnerID: sharer.ID, Name: "Unrelated", NodeType: "file"})

	createTestShare(t, ShareNodeParams{NodeID: folder.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "read"})

	hasAccess, err := testStore.HasAccessToNode(context.Background(), file.ID, recipient.ID)
	require.NoError(t, err)
	require.True(t, hasAccess, "Should have access to child file")

	hasAccess, err = testStore.HasAccessToNode(context.Background(), subFolder.ID, recipient.ID)
	require.NoError(t, err)
	require.True(t, hasAccess, "Should have access to child folder")

	hasAccess, err = testStore.HasAccessToNode(context.Background(), folder.ID, recipient.ID)
	require.NoError(t, err)
	require.True(t, hasAccess, "Should have access to shared folder itself")

	hasAccess, err = testStore.HasAccessToNode(context.Background(), unrelatedNode.ID, recipient.ID)
	require.NoError(t, err)
	require.False(t, hasAccess, "Should not have access to unrelated node")

	hasAccess, err = testStore.HasAccessToNode(context.Background(), file.ID, sharer.ID)
	require.NoError(t, err)
	require.False(t, hasAccess, "Owner should not have access via shares table")
}

func TestGetOutgoingShares(t *testing.T) {
	sharer := createTestUser(t, "sharer_outgoing")
	recipient1 := createTestUser(t, "recipient1_outgoing")
	recipient2 := createTestUser(t, "recipient2_outgoing")
	node1 := createTestNode(t, CreateNodeParams{ID: "outgoing_node1", OwnerID: sharer.ID, Name: "Doc", NodeType: "file"})
	node2 := createTestNode(t, CreateNodeParams{ID: "outgoing_node2", OwnerID: sharer.ID, Name: "Images", NodeType: "folder"})

	createTestShare(t, ShareNodeParams{NodeID: node1.ID, SharerID: sharer.ID, RecipientID: recipient1.ID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node2.ID, SharerID: sharer.ID, RecipientID: recipient2.ID, Permissions: "write"})

	shares, err := testStore.GetOutgoingShares(context.Background(), sharer.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, shares, 2)

	shareMap := make(map[string]OutgoingShare)
	for _, s := range shares {
		shareMap[s.NodeID] = s
	}

	require.Equal(t, "Doc", shareMap[node1.ID].NodeName)
	require.Equal(t, "file", shareMap[node1.ID].NodeType)
	require.Equal(t, "recipient1_outgoing", shareMap[node1.ID].RecipientUsername)
	require.Equal(t, "read", shareMap[node1.ID].Permissions)

	require.Equal(t, "Images", shareMap[node2.ID].NodeName)
	require.Equal(t, "folder", shareMap[node2.ID].NodeType)
	require.Equal(t, "recipient2_outgoing", shareMap[node2.ID].RecipientUsername)
	require.Equal(t, "write", shareMap[node2.ID].Permissions)
}

func TestDeleteAndGetShareByID(t *testing.T) {
	sharer := createTestUser(t, "sharer_delete")
	recipient := createTestUser(t, "recipient_delete")
	otherUser := createTestUser(t, "other_user_delete")
	node := createTestNode(t, CreateNodeParams{ID: "delete_share_node", OwnerID: sharer.ID, Name: "File to unshare", NodeType: "file"})

	share := createTestShare(t, ShareNodeParams{NodeID: node.ID, SharerID: sharer.ID, RecipientID: recipient.ID, Permissions: "read"})

	foundShare, err := testStore.GetShareByID(context.Background(), share.ID, sharer.ID)
	require.NoError(t, err)
	require.NotNil(t, foundShare)
	require.Equal(t, share.ID, foundShare.ID)

	foundShare, err = testStore.GetShareByID(context.Background(), share.ID, otherUser.ID)
	require.NoError(t, err)
	require.Nil(t, foundShare)

	err = testStore.DeleteShare(context.Background(), share.ID, sharer.ID)
	require.NoError(t, err)

	foundShare, err = testStore.GetShareByID(context.Background(), share.ID, sharer.ID)
	require.NoError(t, err)
	require.Nil(t, foundShare)
}

func TestGetUserByUsername(t *testing.T) {
	createdUser := createTestUser(t, "testuser_getbyusername")

	foundUser, err := testStore.GetUserByUsername(context.Background(), createdUser.Username)

	require.NoError(t, err)
	require.NotNil(t, foundUser)

	require.Equal(t, createdUser.ID, foundUser.ID)
	require.Equal(t, createdUser.Username, foundUser.Username)
	require.NotEmpty(t, foundUser.PasswordHash)

	require.NotNil(t, foundUser.DisplayName)
	require.Equal(t, fmt.Sprintf("User %s", createdUser.Username), *foundUser.DisplayName)

	nonExistentUser, err := testStore.GetUserByUsername(context.Background(), "nonexistentuser")
	require.NoError(t, err)
	require.Nil(t, nonExistentUser)
}

func TestCreateSession(t *testing.T) {
	user := createTestUser(t, "user_session_create")
	refreshToken := "test_refresh_token_session_create"

	params := CreateSessionParams{
		ID:           uuid.New(),
		UserID:       user.ID,
		RefreshToken: refreshToken,
		UserAgent:    "test-agent",
		ClientIP:     "127.0.0.1",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}

	err := testStore.CreateSession(context.Background(), params)
	require.NoError(t, err)

	var foundToken string
	query := "SELECT refresh_token FROM sessions WHERE id = $1"
	err = testStore.pool.QueryRow(context.Background(), query, params.ID).Scan(&foundToken)

	require.NoError(t, err)
	require.Equal(t, params.RefreshToken, foundToken)
}

func TestLogAndGetEvents(t *testing.T) {
	user := createTestUser(t, "user_events")
	otherUser := createTestUser(t, "other_user_events")

	payload1 := map[string]string{"nodeId": "node1", "action": "create"}
	payload2 := map[string]string{"nodeId": "node2", "action": "delete"}

	err := testStore.LogEvent(context.Background(), user.ID, "NODE_CREATE", payload1)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	err = testStore.LogEvent(context.Background(), user.ID, "NODE_DELETE", payload2)
	require.NoError(t, err)

	events, err := testStore.GetEventsSince(context.Background(), user.ID, 0)
	require.NoError(t, err)
	require.Len(t, events, 2)

	type EventPayloadWrapper struct {
		EventType string            `json:"event_type"`
		Payload   map[string]string `json:"payload"`
	}

	var wrapper1 EventPayloadWrapper
	err = json.Unmarshal(events[0].Payload, &wrapper1)
	require.NoError(t, err)
	require.Equal(t, "NODE_CREATE", wrapper1.EventType)
	require.Equal(t, payload1, wrapper1.Payload)

	var wrapper2 EventPayloadWrapper
	err = json.Unmarshal(events[1].Payload, &wrapper2)
	require.NoError(t, err)
	require.Equal(t, "NODE_DELETE", wrapper2.EventType)
	require.Equal(t, payload2, wrapper2.Payload)

	eventsSince, err := testStore.GetEventsSince(context.Background(), user.ID, events[0].ID)
	require.NoError(t, err)
	require.Len(t, eventsSince, 1)
	require.Equal(t, events[1].ID, eventsSince[0].ID)

	noEvents, err := testStore.GetEventsSince(context.Background(), otherUser.ID, 0)
	require.NoError(t, err)
	require.Len(t, noEvents, 0)
}

func TestUpdateUserStorage(t *testing.T) {
	user := createTestUser(t, "user_storage")
	require.Equal(t, int64(0), user.StorageUsedBytes)

	err := testStore.UpdateUserStorage(context.Background(), user.ID, 1024)
	require.NoError(t, err)

	updatedUser, err := testStore.GetUserByUsername(context.Background(), user.Username)
	require.NoError(t, err)
	require.Equal(t, int64(1024), updatedUser.StorageUsedBytes)

	err = testStore.UpdateUserStorage(context.Background(), user.ID, -512)
	require.NoError(t, err)

	updatedUser2, err := testStore.GetUserByUsername(context.Background(), user.Username)
	require.NoError(t, err)
	require.Equal(t, int64(512), updatedUser2.StorageUsedBytes)
}

func TestPurgeTrash(t *testing.T) {
	user := createTestUser(t, "user_purge")
	otherUser := createTestUser(t, "other_user_purge")

	var fileSize int64 = 100
	node1 := createTestNode(t, CreateNodeParams{ID: "purge_1", OwnerID: user.ID, Name: "file1.txt", NodeType: "file", SizeBytes: &fileSize})
	node2 := createTestNode(t, CreateNodeParams{ID: "purge_2", OwnerID: user.ID, Name: "file2.txt", NodeType: "file", SizeBytes: &fileSize})
	node3 := createTestNode(t, CreateNodeParams{ID: "purge_3", OwnerID: otherUser.ID, Name: "other_file.txt", NodeType: "file", SizeBytes: &fileSize})

	_, err := testStore.MoveNodeToTrash(context.Background(), node1.ID, user.ID)
	require.NoError(t, err)
	_, err = testStore.MoveNodeToTrash(context.Background(), node2.ID, user.ID)
	require.NoError(t, err)
	_, err = testStore.MoveNodeToTrash(context.Background(), node3.ID, otherUser.ID)
	require.NoError(t, err)

	deletedIDs, sizeFreed, err := testStore.PurgeTrash(context.Background(), user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(200), sizeFreed)
	require.ElementsMatch(t, []string{node1.ID, node2.ID}, deletedIDs)

	exists, err := testStore.NodeExists(context.Background(), node1.ID)
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = testStore.NodeExists(context.Background(), node2.ID)
	require.NoError(t, err)
	require.False(t, exists)

	var count int
	err = testStore.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM nodes WHERE id=$1 AND deleted_at IS NOT NULL`, node3.ID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestRenameNode(t *testing.T) {
	user := createTestUser(t, "user_rename")
	node := createTestNode(t, CreateNodeParams{ID: "rename_1", OwnerID: user.ID, Name: "old_name.txt", NodeType: "file"})

	success, err := testStore.RenameNode(context.Background(), node.ID, user.ID, "new_name.txt")
	require.NoError(t, err)
	require.True(t, success)

	renamedNode, err := testStore.GetNodeByID(context.Background(), node.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, "new_name.txt", renamedNode.Name)

	createTestNode(t, CreateNodeParams{ID: "rename_2", OwnerID: user.ID, Name: "existing.txt", NodeType: "file"})
	success, err = testStore.RenameNode(context.Background(), node.ID, user.ID, "existing.txt")
	require.Error(t, err)
	require.False(t, success)
	require.ErrorIs(t, err, ErrDuplicateNodeName)

	success, err = testStore.RenameNode(context.Background(), "non_existent", user.ID, "any_name")
	require.NoError(t, err)
	require.False(t, success)
}

func TestListTrash(t *testing.T) {
	user := createTestUser(t, "user_list_trash")
	node1 := createTestNode(t, CreateNodeParams{ID: "trash_list_1", OwnerID: user.ID, Name: "first_to_trash", NodeType: "file"})
	node2 := createTestNode(t, CreateNodeParams{ID: "trash_list_2", OwnerID: user.ID, Name: "second_to_trash", NodeType: "file"})

	_, err := testStore.MoveNodeToTrash(context.Background(), node1.ID, user.ID)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	_, err = testStore.MoveNodeToTrash(context.Background(), node2.ID, user.ID)
	require.NoError(t, err)

	trashedNodes, err := testStore.ListTrash(context.Background(), user.ID, 10, 0)
	require.NoError(t, err)
	require.Len(t, trashedNodes, 2)
	require.Equal(t, "second_to_trash", trashedNodes[0].Name)
	require.Equal(t, "first_to_trash", trashedNodes[1].Name)
}

func TestIsDescendantOf(t *testing.T) {
	user := createTestUser(t, "user_descendant")
	folder1 := createTestNode(t, CreateNodeParams{ID: "desc_1", OwnerID: user.ID, Name: "F1", NodeType: "folder"})
	folder2 := createTestNode(t, CreateNodeParams{ID: "desc_2", OwnerID: user.ID, ParentID: &folder1.ID, Name: "F2", NodeType: "folder"})
	folder3 := createTestNode(t, CreateNodeParams{ID: "desc_3", OwnerID: user.ID, ParentID: &folder2.ID, Name: "F3", NodeType: "folder"})
	otherFolder := createTestNode(t, CreateNodeParams{ID: "desc_other", OwnerID: user.ID, Name: "Other", NodeType: "folder"})

	isDesc, err := testStore.IsDescendantOf(context.Background(), folder1.ID, folder3.ID)
	require.NoError(t, err)
	require.True(t, isDesc)

	isDesc, err = testStore.IsDescendantOf(context.Background(), folder3.ID, folder1.ID)
	require.NoError(t, err)
	require.False(t, isDesc)

	isDesc, err = testStore.IsDescendantOf(context.Background(), folder2.ID, folder2.ID)
	require.NoError(t, err)
	require.True(t, isDesc)

	isDesc, err = testStore.IsDescendantOf(context.Background(), otherFolder.ID, folder2.ID)
	require.NoError(t, err)
	require.False(t, isDesc)
}

func TestGetUserByRefreshToken(t *testing.T) {
	user := createTestUser(t, "user_by_refresh_token")
	token := "valid_refresh_token"
	sessionParams := CreateSessionParams{
		ID:           uuid.New(),
		UserID:       user.ID,
		RefreshToken: token,
		UserAgent:    "test",
		ClientIP:     "1.1.1.1",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	err := testStore.CreateSession(context.Background(), sessionParams)
	require.NoError(t, err)

	foundUser, err := testStore.GetUserByRefreshToken(context.Background(), token)
	require.NoError(t, err)
	require.NotNil(t, foundUser)
	require.Equal(t, user.ID, foundUser.ID)

	foundUser, err = testStore.GetUserByRefreshToken(context.Background(), "invalid_token")
	require.NoError(t, err)
	require.Nil(t, foundUser)

	expiredToken := "expired_token"
	expiredSessionParams := CreateSessionParams{
		ID:           uuid.New(),
		UserID:       user.ID,
		RefreshToken: expiredToken,
		UserAgent:    "test",
		ClientIP:     "1.1.1.1",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	err = testStore.CreateSession(context.Background(), expiredSessionParams)
	require.NoError(t, err)

	foundUser, err = testStore.GetUserByRefreshToken(context.Background(), expiredToken)
	require.NoError(t, err)
	require.Nil(t, foundUser)
}

func TestListSessionsForUser(t *testing.T) {
	user := createTestUser(t, "user_list_sessions")

	for i := 0; i < 2; i++ {
		err := testStore.CreateSession(context.Background(), CreateSessionParams{
			ID:           uuid.New(),
			UserID:       user.ID,
			RefreshToken: fmt.Sprintf("list_token_%d", i),
			UserAgent:    "active",
			ClientIP:     "1.1.1.1",
			ExpiresAt:    time.Now().Add(time.Hour),
		})
		require.NoError(t, err)
	}
	err := testStore.CreateSession(context.Background(), CreateSessionParams{
		ID:           uuid.New(),
		UserID:       user.ID,
		RefreshToken: "expired_list_token",
		UserAgent:    "expired",
		ClientIP:     "1.1.1.1",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)

	sessions, err := testStore.ListSessionsForUser(context.Background(), user.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
}

func TestDeleteSessionByID(t *testing.T) {
	user := createTestUser(t, "user_delete_session_by_id")
	otherUser := createTestUser(t, "other_user_delete_session")
	sessionIDToDelete := uuid.New()
	sessionIDToKeep := uuid.New()
	otherUserSessionID := uuid.New()

	err := testStore.CreateSession(context.Background(), CreateSessionParams{ID: sessionIDToDelete, UserID: user.ID, RefreshToken: "delete_me", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	err = testStore.CreateSession(context.Background(), CreateSessionParams{ID: sessionIDToKeep, UserID: user.ID, RefreshToken: "keep_me", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	err = testStore.CreateSession(context.Background(), CreateSessionParams{ID: otherUserSessionID, UserID: otherUser.ID, RefreshToken: "other_user_session", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)

	err = testStore.DeleteSessionByID(context.Background(), otherUserSessionID, user.ID)
	require.NoError(t, err)

	err = testStore.DeleteSessionByID(context.Background(), sessionIDToDelete, user.ID)
	require.NoError(t, err)

	sessions, err := testStore.ListSessionsForUser(context.Background(), user.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, sessionIDToKeep, sessions[0].ID)

	otherSessions, err := testStore.ListSessionsForUser(context.Background(), otherUser.ID)
	require.NoError(t, err)
	require.Len(t, otherSessions, 1)
}

func TestDeleteAllSessionsForUser(t *testing.T) {
	user1 := createTestUser(t, "user_delete_all_1")
	user2 := createTestUser(t, "user_delete_all_2")

	for i := 0; i < 3; i++ {
		err := testStore.CreateSession(context.Background(), CreateSessionParams{ID: uuid.New(), UserID: user1.ID, RefreshToken: fmt.Sprintf("u1_token_%d", i), ExpiresAt: time.Now().Add(time.Hour)})
		require.NoError(t, err)
	}
	err := testStore.CreateSession(context.Background(), CreateSessionParams{ID: uuid.New(), UserID: user2.ID, RefreshToken: "u2_token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)

	err = testStore.DeleteAllSessionsForUser(context.Background(), user1.ID)
	require.NoError(t, err)

	user1Sessions, err := testStore.ListSessionsForUser(context.Background(), user1.ID)
	require.NoError(t, err)
	require.Len(t, user1Sessions, 0)

	user2Sessions, err := testStore.ListSessionsForUser(context.Background(), user2.ID)
	require.NoError(t, err)
	require.Len(t, user2Sessions, 1)
}

func TestDeleteSessionByRefreshToken(t *testing.T) {
	user := createTestUser(t, "user_delete_by_token")
	tokenToDelete := "token_to_delete_by_refresh"
	tokenToKeep := "token_to_keep_by_refresh"

	err := testStore.CreateSession(context.Background(), CreateSessionParams{ID: uuid.New(), UserID: user.ID, RefreshToken: tokenToDelete, ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	err = testStore.CreateSession(context.Background(), CreateSessionParams{ID: uuid.New(), UserID: user.ID, RefreshToken: tokenToKeep, ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)

	err = testStore.DeleteSessionByRefreshToken(context.Background(), tokenToDelete)
	require.NoError(t, err)

	sessions, err := testStore.ListSessionsForUser(context.Background(), user.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	foundUser, err := testStore.GetUserByRefreshToken(context.Background(), tokenToDelete)
	require.NoError(t, err)
	require.Nil(t, foundUser)

	foundUser, err = testStore.GetUserByRefreshToken(context.Background(), tokenToKeep)
	require.NoError(t, err)
	require.NotNil(t, foundUser)
}

func TestUpdateUserPassword(t *testing.T) {
	user := createTestUser(t, "user_pass_update")
	newPassword := "newSecurePassword123"
	newPasswordHash, err := auth.HashPassword(newPassword)
	require.NoError(t, err)

	err = testStore.UpdateUserPassword(context.Background(), user.ID, newPasswordHash)
	require.NoError(t, err)

	updatedUser, err := testStore.GetUserByUsername(context.Background(), user.Username)
	require.NoError(t, err)
	require.NotNil(t, updatedUser)
	require.Equal(t, newPasswordHash, updatedUser.PasswordHash)
	require.True(t, auth.CheckPasswordHash(newPassword, updatedUser.PasswordHash))
	require.False(t, auth.CheckPasswordHash("oldPassword", updatedUser.PasswordHash))
}

func TestCheckWritePermission(t *testing.T) {
	owner := createTestUser(t, "perm_owner")
	writer := createTestUser(t, "perm_writer")
	reader := createTestUser(t, "perm_reader")
	other := createTestUser(t, "perm_other")

	rootFolder := createTestNode(t, CreateNodeParams{ID: "perm_root", OwnerID: owner.ID, Name: "Root Folder", NodeType: "folder"})
	writeFolder := createTestNode(t, CreateNodeParams{ID: "perm_write", OwnerID: owner.ID, Name: "Write Folder", ParentID: &rootFolder.ID, NodeType: "folder"})
	readFolder := createTestNode(t, CreateNodeParams{ID: "perm_read", OwnerID: owner.ID, Name: "Read Folder", ParentID: &writeFolder.ID, NodeType: "folder"})

	createTestShare(t, ShareNodeParams{NodeID: writeFolder.ID, SharerID: owner.ID, RecipientID: writer.ID, Permissions: "write"})
	createTestShare(t, ShareNodeParams{NodeID: readFolder.ID, SharerID: owner.ID, RecipientID: reader.ID, Permissions: "read"})

	t.Run("owner has write permission everywhere", func(t *testing.T) {
		hasPerm, err := testStore.CheckWritePermission(context.Background(), owner.ID, nil)
		require.NoError(t, err)
		require.True(t, hasPerm)
		hasPerm, err = testStore.CheckWritePermission(context.Background(), owner.ID, &rootFolder.ID)
		require.NoError(t, err)
		require.True(t, hasPerm)
		hasPerm, err = testStore.CheckWritePermission(context.Background(), owner.ID, &writeFolder.ID)
		require.NoError(t, err)
		require.True(t, hasPerm)
		hasPerm, err = testStore.CheckWritePermission(context.Background(), owner.ID, &readFolder.ID)
		require.NoError(t, err)
		require.True(t, hasPerm)
	})

	t.Run("writer has write permission in shared folder and its children", func(t *testing.T) {
		hasPerm, err := testStore.CheckWritePermission(context.Background(), writer.ID, &writeFolder.ID)
		require.NoError(t, err)
		require.True(t, hasPerm)
		hasPerm, err = testStore.CheckWritePermission(context.Background(), writer.ID, &readFolder.ID)
		require.NoError(t, err)
		require.True(t, hasPerm)
	})

	t.Run("writer does not have permission outside shared scope", func(t *testing.T) {
		hasPerm, err := testStore.CheckWritePermission(context.Background(), writer.ID, &rootFolder.ID)
		require.NoError(t, err)
		require.False(t, hasPerm)
	})

	t.Run("reader has no write permission", func(t *testing.T) {
		hasPerm, err := testStore.CheckWritePermission(context.Background(), reader.ID, &readFolder.ID)
		require.NoError(t, err)
		require.False(t, hasPerm)
		hasPerm, err = testStore.CheckWritePermission(context.Background(), reader.ID, &writeFolder.ID)
		require.NoError(t, err)
		require.False(t, hasPerm)
	})

	t.Run("other user has no permissions", func(t *testing.T) {
		hasPerm, err := testStore.CheckWritePermission(context.Background(), other.ID, &readFolder.ID)
		require.NoError(t, err)
		require.False(t, hasPerm)
	})
}

func TestGetUserByID(t *testing.T) {
	user := createTestUser(t, "get_by_id_user")

	foundUser, err := testStore.GetUserByID(context.Background(), user.ID)
	require.NoError(t, err)

	require.NotNil(t, foundUser)
	require.Equal(t, user.ID, foundUser.ID)
	require.Equal(t, user.Username, foundUser.Username)

	notFoundUser, err := testStore.GetUserByID(context.Background(), 999999)
	require.NoError(t, err)
	require.Nil(t, notFoundUser)
}
