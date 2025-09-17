package database

import (
	"context"
	"serwer-plikow/internal/models"
	"testing"

	"github.com/stretchr/testify/require"
)

func createTestShare(t *testing.T, params ShareNodeParams) *models.Share {
	share, err := testStore.ShareNode(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, share)
	return share
}

func TestShareNode(t *testing.T) {
	sharerID := createTestUserForNodes(t, "sharer_user")
	recipientID := createTestUserForNodes(t, "recipient_user")
	node := createTestNode(t, CreateNodeParams{ID: "share_node_1", OwnerID: sharerID, Name: "Shared File", NodeType: "file"})

	params := ShareNodeParams{
		NodeID:      node.ID,
		SharerID:    sharerID,
		RecipientID: recipientID,
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
	recipientID := createTestUserForNodes(t, "recipient_for_list")
	sharer1ID := createTestUserForNodes(t, "sharer1_for_list")
	sharer2ID := createTestUserForNodes(t, "sharer2_for_list")
	node1 := createTestNode(t, CreateNodeParams{ID: "share_list_node1", OwnerID: sharer1ID, Name: "File 1", NodeType: "file"})
	node2 := createTestNode(t, CreateNodeParams{ID: "share_list_node2", OwnerID: sharer1ID, Name: "File 2", NodeType: "file"})
	node3 := createTestNode(t, CreateNodeParams{ID: "share_list_node3", OwnerID: sharer2ID, Name: "File 3", NodeType: "file"})

	createTestShare(t, ShareNodeParams{NodeID: node1.ID, SharerID: sharer1ID, RecipientID: recipientID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node2.ID, SharerID: sharer1ID, RecipientID: recipientID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node3.ID, SharerID: sharer2ID, RecipientID: recipientID, Permissions: "read"})

	users, err := testStore.GetSharingUsers(context.Background(), recipientID, 100, 0)
	require.NoError(t, err)
	require.Len(t, users, 2)

	userMap := make(map[int64]string)
	for _, u := range users {
		userMap[u.ID] = u.Username
	}
	require.Equal(t, "sharer1_for_list", userMap[sharer1ID])
	require.Equal(t, "sharer2_for_list", userMap[sharer2ID])

	nobodyID := createTestUserForNodes(t, "nobody")
	users, err = testStore.GetSharingUsers(context.Background(), nobodyID, 100, 0)
	require.NoError(t, err)
	require.Len(t, users, 0)
}

func TestListDirectlySharedNodes(t *testing.T) {
	recipientID := createTestUserForNodes(t, "recipient_for_direct")
	sharerID := createTestUserForNodes(t, "sharer_for_direct")
	otherSharerID := createTestUserForNodes(t, "other_sharer_for_direct")
	node1 := createTestNode(t, CreateNodeParams{ID: "direct_share_node1", OwnerID: sharerID, Name: "A_File", NodeType: "file"})
	node2 := createTestNode(t, CreateNodeParams{ID: "direct_share_node2", OwnerID: sharerID, Name: "Z_Folder", NodeType: "folder"})
	node3 := createTestNode(t, CreateNodeParams{ID: "direct_share_node3", OwnerID: otherSharerID, Name: "Other File", NodeType: "file"})

	createTestShare(t, ShareNodeParams{NodeID: node1.ID, SharerID: sharerID, RecipientID: recipientID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node2.ID, SharerID: sharerID, RecipientID: recipientID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node3.ID, SharerID: otherSharerID, RecipientID: recipientID, Permissions: "read"})

	nodes, err := testStore.ListDirectlySharedNodes(context.Background(), recipientID, sharerID, 100, 0)
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, "Z_Folder", nodes[0].Name)
	require.Equal(t, "A_File", nodes[1].Name)
}

func TestHasAccessToNode(t *testing.T) {
	sharerID := createTestUserForNodes(t, "h_sharer_for_access")
	recipientID := createTestUserForNodes(t, "h_recipient_for_access")
	folder := createTestNode(t, CreateNodeParams{ID: "h_access_folder", OwnerID: sharerID, Name: "Parent", NodeType: "folder"})
	subFolder := createTestNode(t, CreateNodeParams{ID: "h_access_subfolder", OwnerID: sharerID, ParentID: &folder.ID, Name: "Child", NodeType: "folder"})
	file := createTestNode(t, CreateNodeParams{ID: "h_access_file", OwnerID: sharerID, ParentID: &subFolder.ID, Name: "file.txt", NodeType: "file"})
	unrelatedNode := createTestNode(t, CreateNodeParams{ID: "h_access_unrelated", OwnerID: sharerID, Name: "Unrelated", NodeType: "file"})

	createTestShare(t, ShareNodeParams{NodeID: folder.ID, SharerID: sharerID, RecipientID: recipientID, Permissions: "read"})

	hasAccess, err := testStore.HasAccessToNode(context.Background(), file.ID, recipientID)
	require.NoError(t, err)
	require.True(t, hasAccess, "Should have access to child file")

	hasAccess, err = testStore.HasAccessToNode(context.Background(), subFolder.ID, recipientID)
	require.NoError(t, err)
	require.True(t, hasAccess, "Should have access to child folder")

	hasAccess, err = testStore.HasAccessToNode(context.Background(), folder.ID, recipientID)
	require.NoError(t, err)
	require.True(t, hasAccess, "Should have access to shared folder itself")

	hasAccess, err = testStore.HasAccessToNode(context.Background(), unrelatedNode.ID, recipientID)
	require.NoError(t, err)
	require.False(t, hasAccess, "Should not have access to unrelated node")

	hasAccess, err = testStore.HasAccessToNode(context.Background(), file.ID, sharerID)
	require.NoError(t, err)
	require.False(t, hasAccess, "Owner should not have access via shares table")
}

func TestGetOutgoingShares(t *testing.T) {
	sharerID := createTestUserForNodes(t, "sharer_outgoing")
	recipient1ID := createTestUserForNodes(t, "recipient1_outgoing")
	recipient2ID := createTestUserForNodes(t, "recipient2_outgoing")
	node1 := createTestNode(t, CreateNodeParams{ID: "outgoing_node1", OwnerID: sharerID, Name: "Doc", NodeType: "file"})
	node2 := createTestNode(t, CreateNodeParams{ID: "outgoing_node2", OwnerID: sharerID, Name: "Images", NodeType: "folder"})

	createTestShare(t, ShareNodeParams{NodeID: node1.ID, SharerID: sharerID, RecipientID: recipient1ID, Permissions: "read"})
	createTestShare(t, ShareNodeParams{NodeID: node2.ID, SharerID: sharerID, RecipientID: recipient2ID, Permissions: "write"})

	shares, err := testStore.GetOutgoingShares(context.Background(), sharerID, 100, 0)
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
	sharerID := createTestUserForNodes(t, "sharer_delete")
	recipientID := createTestUserForNodes(t, "recipient_delete")
	otherUserID := createTestUserForNodes(t, "other_user_delete")
	node := createTestNode(t, CreateNodeParams{ID: "delete_share_node", OwnerID: sharerID, Name: "File to unshare", NodeType: "file"})

	share := createTestShare(t, ShareNodeParams{NodeID: node.ID, SharerID: sharerID, RecipientID: recipientID, Permissions: "read"})

	foundShare, err := testStore.GetShareByID(context.Background(), share.ID, sharerID)
	require.NoError(t, err)
	require.NotNil(t, foundShare)
	require.Equal(t, share.ID, foundShare.ID)

	foundShare, err = testStore.GetShareByID(context.Background(), share.ID, otherUserID)
	require.NoError(t, err)
	require.Nil(t, foundShare)

	err = testStore.DeleteShare(context.Background(), share.ID, sharerID)
	require.NoError(t, err)

	foundShare, err = testStore.GetShareByID(context.Background(), share.ID, sharerID)
	require.NoError(t, err)
	require.Nil(t, foundShare)
}
