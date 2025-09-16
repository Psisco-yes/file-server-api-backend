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

	// Test 1: Użytkownik dodaje do ulubionych własny plik
	err := testStore.AddFavorite(context.Background(), userID, node.ID)
	require.NoError(t, err)

	// Test 2: Próba ponownego dodania tego samego pliku
	err = testStore.AddFavorite(context.Background(), userID, node.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrFavoriteAlreadyExists)

	// Test 3: Próba dodania do ulubionych nieistniejącego pliku
	err = testStore.AddFavorite(context.Background(), userID, "non_existent_node")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNodeNotFound) // GetNodeIfAccessible zwraca nil, co jest mapowane na ErrNodeNotFound

	// Test 4: Użytkownik dodaje do ulubionych plik, do którego ma dostęp przez udostępnienie
	createTestShare(t, ShareNodeParams{NodeID: sharedFolder.ID, SharerID: otherUserID, RecipientID: userID, Permissions: "read"})
	err = testStore.AddFavorite(context.Background(), userID, nodeInSharedFolder.ID)
	require.NoError(t, err, "Should be able to favorite an accessible shared node")
}

func TestRemoveFavorite(t *testing.T) {
	userID := createTestUserForNodes(t, "user_fav_remove")
	node := createTestNode(t, CreateNodeParams{ID: "fav_node_2", OwnerID: userID, Name: "File to Unfavorite", NodeType: "file"})

	// Dodaj do ulubionych
	err := testStore.AddFavorite(context.Background(), userID, node.ID)
	require.NoError(t, err)

	// Act: Usuń z ulubionych
	success, err := testStore.RemoveFavorite(context.Background(), userID, node.ID)
	require.NoError(t, err)
	require.True(t, success)

	// Weryfikacja: Sprawdź, czy wpis został usunięty
	var count int
	err = testStore.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_favorites WHERE user_id=$1 AND node_id=$2`, userID, node.ID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count)

	// Próba usunięcia czegoś, co nie jest w ulubionych
	success, err = testStore.RemoveFavorite(context.Background(), userID, node.ID)
	require.NoError(t, err)
	require.False(t, success)
}

func TestListFavorites(t *testing.T) {
	userID := createTestUserForNodes(t, "user_fav_list")
	otherUserID := createTestUserForNodes(t, "other_user_fav_list")

	// Węzły do dodania do ulubionych
	node1 := createTestNode(t, CreateNodeParams{ID: "fav_list_1", OwnerID: userID, Name: "A_My File", NodeType: "file"})
	node2_shared := createTestNode(t, CreateNodeParams{ID: "fav_list_2", OwnerID: otherUserID, Name: "B_Shared File", NodeType: "file"})
	node3_trashed := createTestNode(t, CreateNodeParams{ID: "fav_list_3", OwnerID: userID, Name: "C_Trashed Fav", NodeType: "file"})

	// Udostępnij drugi plik
	createTestShare(t, ShareNodeParams{NodeID: node2_shared.ID, SharerID: otherUserID, RecipientID: userID, Permissions: "read"})

	// Dodaj do ulubionych
	err := testStore.AddFavorite(context.Background(), userID, node1.ID)
	require.NoError(t, err)
	err = testStore.AddFavorite(context.Background(), userID, node2_shared.ID)
	require.NoError(t, err)
	err = testStore.AddFavorite(context.Background(), userID, node3_trashed.ID)
	require.NoError(t, err)

	// Przenieś trzeci plik do kosza
	_, err = testStore.MoveNodeToTrash(context.Background(), node3_trashed.ID, userID)
	require.NoError(t, err)

	// Act: Pobierz listę ulubionych
	favorites, err := testStore.ListFavorites(context.Background(), userID)
	require.NoError(t, err)

	// Assert: Powinny być 2 pliki (własny i udostępniony, bez tego z kosza)
	require.Len(t, favorites, 2)
	// Sprawdź sortowanie alfabetyczne
	require.Equal(t, "A_My File", favorites[0].Name)
	require.Equal(t, "B_Shared File", favorites[1].Name)
}
