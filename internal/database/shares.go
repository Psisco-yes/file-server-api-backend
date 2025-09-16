package database

import (
	"context"
	"errors"
	"serwer-plikow/internal/models"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrNodeNotFound = errors.New("node not found or user is not the owner")
var ErrShareAlreadyExists = errors.New("this node is already shared with the recipient")
var ErrRecipientNotFound = errors.New("recipient user not found")

type ShareNodeParams struct {
	NodeID      string
	SharerID    int64
	RecipientID int64
	Permissions string
}

func (s *PostgresStore) ShareNode(ctx context.Context, arg ShareNodeParams) (*models.Share, error) {
	query := `
		INSERT INTO shares (node_id, sharer_id, recipient_id, permissions)
		VALUES ($1, $2, $3, $4)
		RETURNING id, node_id, sharer_id, recipient_id, permissions, shared_at
	`
	row := s.pool.QueryRow(ctx, query, arg.NodeID, arg.SharerID, arg.RecipientID, arg.Permissions)

	var share models.Share
	var err = row.Scan(
		&share.ID,
		&share.NodeID,
		&share.SharerID,
		&share.RecipientID,
		&share.Permissions,
		&share.SharedAt,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrShareAlreadyExists
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return nil, ErrRecipientNotFound
		}
		return nil, err
	}

	return &share, nil
}

type SharingUser struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

func (s *PostgresStore) GetSharingUsers(ctx context.Context, recipientID int64) ([]SharingUser, error) {
	query := `
		SELECT DISTINCT ON (u.id)
			u.id,
			u.username,
			u.display_name
		FROM shares s
		JOIN users u ON s.sharer_id = u.id
		WHERE s.recipient_id = $1
		ORDER BY u.id
	`
	rows, err := s.pool.Query(ctx, query, recipientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []SharingUser
	for rows.Next() {
		var user SharingUser
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName); err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if users == nil {
		return []SharingUser{}, nil
	}

	return users, nil
}

func (s *PostgresStore) ListDirectlySharedNodes(ctx context.Context, recipientID int64, sharerID int64) ([]models.Node, error) {
	query := `
		SELECT 
			n.id, 
			n.owner_id, 
			n.parent_id, 
			n.name, 
			n.node_type, 
			n.size_bytes, 
			n.mime_type,
			n.created_at,
			n.modified_at
		FROM nodes n
		JOIN shares s ON n.id = s.node_id
		WHERE s.recipient_id = $1 AND s.sharer_id = $2 AND n.deleted_at IS NULL
		ORDER BY n.node_type DESC, n.name
	`

	rows, err := s.pool.Query(ctx, query, recipientID, sharerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var node models.Node
		err := rows.Scan(
			&node.ID,
			&node.OwnerID,
			&node.ParentID,
			&node.Name,
			&node.NodeType,
			&node.SizeBytes,
			&node.MimeType,
			&node.CreatedAt,
			&node.ModifiedAt,
		)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if nodes == nil {
		return []models.Node{}, nil
	}

	return nodes, nil
}

func (s *PostgresStore) HasAccessToNode(ctx context.Context, nodeID string, recipientID int64) (bool, error) {
	query := `
		WITH RECURSIVE node_parents AS (
			-- Zaczynamy od node'a, o który pytamy
			SELECT id, parent_id
			FROM nodes
			WHERE id = $1

			UNION ALL

			SELECT n.id, n.parent_id
			FROM nodes n
			JOIN node_parents np ON n.id = np.parent_id
		)
		SELECT EXISTS (
			SELECT 1
			FROM shares s
			WHERE s.recipient_id = $2 AND s.node_id IN (SELECT id FROM node_parents)
		);
	`
	var hasAccess bool
	err := s.pool.QueryRow(ctx, query, nodeID, recipientID).Scan(&hasAccess)
	return hasAccess, err
}

type OutgoingShare struct {
	models.Share
	NodeName          string `json:"node_name"`
	NodeType          string `json:"node_type"`
	RecipientUsername string `json:"recipient_username"`
}

func (s *PostgresStore) GetOutgoingShares(ctx context.Context, sharerID int64) ([]OutgoingShare, error) {
	query := `
		SELECT 
			s.id, s.node_id, s.sharer_id, s.recipient_id, s.permissions, s.shared_at,
			n.name AS node_name,
			n.node_type AS node_type,
			u.username AS recipient_username
		FROM shares s
		JOIN nodes n ON s.node_id = n.id
		JOIN users u ON s.recipient_id = u.id
		WHERE s.sharer_id = $1
		ORDER BY s.shared_at DESC
	`
	rows, err := s.pool.Query(ctx, query, sharerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []OutgoingShare
	for rows.Next() {
		var share OutgoingShare
		err := rows.Scan(
			&share.ID, &share.NodeID, &share.SharerID, &share.RecipientID, &share.Permissions, &share.SharedAt,
			&share.NodeName, &share.NodeType, &share.RecipientUsername,
		)
		if err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if shares == nil {
		return []OutgoingShare{}, nil
	}

	return shares, nil
}

func (s *PostgresStore) DeleteShare(ctx context.Context, shareID int64, sharerID int64) (bool, error) {
	query := `
		DELETE FROM shares
		WHERE id = $1 AND sharer_id = $2
	`
	res, err := s.pool.Exec(ctx, query, shareID, sharerID)
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, nil
}
