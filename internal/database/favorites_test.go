package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddFavorite(t *testing.T) {
	userID := createTestUserForNodes(t, "user_fav_add")
	otherUserID := createTestUserForNodes(t, "other_user_fav_add")
	node := createTestNode(t, CreateNodeParams{ID: "fav_node_1", OwnerID: userID, Name: "My Fav File", NodeType: "file"})
	sharedFolder := createTestNode(t, CreateNodeParams{ID: "fav_shared_folder", OwnerID: otherUserID, Name: "Shared Folder", NodeType: "folder"})
	nodeInSharedFolder := createTestNode(t, CreateNodeParams{ID: "fav_node_in_shared", OwnerID: otherUserID, ParentID: &sharedFolder.ID, Name: "File in Shared", NodeType: "file"})

	err := testStore.AddFavorite(context.Background(), userID, node.ID)
	require.NoError(t, err)

	err = testStore.AddFavorite(context.Background(), userID, node.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrFavoriteAlreadyExists)

	err = testStore.AddFavorite(context.Background(), userID, "non_existent_node")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNodeNotFound)

	createTestShare(t, ShareNodeParams{NodeID: sharedFolder.ID, SharerID: otherUserID, RecipientID: userID, Permissions: "read"})
	err = testStore.AddFavorite(context.Background(), userID, nodeInSharedFolder.ID)
	require.NoError(t, err, "Should be able to favorite an accessible shared node")
}

func TestRemoveFavorite(t *testing.T) {
	userID := createTestUserForNodes(t, "user_fav_remove")
	node := createTestNode(t, CreateNodeParams{ID: "fav_node_2", OwnerID: userID, Name: "File to Unfavorite", NodeType: "file"})

	err := testStore.AddFavorite(context.Background(), userID, node.ID)
	require.NoError(t, err)

	success, err := testStore.RemoveFavorite(context.Background(), userID, node.ID)
	require.NoError(t, err)
	require.True(t, success)

	var count int
	err = testStore.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_favorites WHERE user_id=$1 AND node_id=$2`, userID, node.ID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count)

	success, err = testStore.RemoveFavorite(context.Background(), userID, node.ID)
	require.NoError(t, err)
	require.False(t, success)
}

func TestListFavorites(t *testing.T) {
	userID := createTestUserForNodes(t, "user_fav_list")
	otherUserID := createTestUserForNodes(t, "other_user_fav_list")

	node1 := createTestNode(t, CreateNodeParams{ID: "fav_list_1", OwnerID: userID, Name: "A_My File", NodeType: "file"})
	node2_shared := createTestNode(t, CreateNodeParams{ID: "fav_list_2", OwnerID: otherUserID, Name: "B_Shared File", NodeType: "file"})
	node3_trashed := createTestNode(t, CreateNodeParams{ID: "fav_list_3", OwnerID: userID, Name: "C_Trashed Fav", NodeType: "file"})

	createTestShare(t, ShareNodeParams{NodeID: node2_shared.ID, SharerID: otherUserID, RecipientID: userID, Permissions: "read"})

	err := testStore.AddFavorite(context.Background(), userID, node1.ID)
	require.NoError(t, err)
	err = testStore.AddFavorite(context.Background(), userID, node2_shared.ID)
	require.NoError(t, err)
	err = testStore.AddFavorite(context.Background(), userID, node3_trashed.ID)
	require.NoError(t, err)

	_, err = testStore.MoveNodeToTrash(context.Background(), node3_trashed.ID, userID)
	require.NoError(t, err)

	favorites, err := testStore.ListFavorites(context.Background(), userID, 100, 0)
	require.NoError(t, err)

	require.Len(t, favorites, 2)
	require.Equal(t, "A_My File", favorites[0].Name)
	require.Equal(t, "B_Shared File", favorites[1].Name)
}
