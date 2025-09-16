package database

import (
	"context"
	"errors"
	"fmt"
	"serwer-plikow/internal/models"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrDuplicateNodeName = errors.New("a node with the same name already exists in this folder")

type CreateNodeParams struct {
	ID        string
	OwnerID   int64
	ParentID  *string
	Name      string
	NodeType  string
	SizeBytes *int64
	MimeType  *string
}

func (s *PostgresStore) CreateNode(ctx context.Context, arg CreateNodeParams) (*models.Node, error) {
	query := `
		INSERT INTO nodes (id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at, deleted_at, original_parent_id
	`
	now := time.Now()

	row := s.pool.QueryRow(ctx, query,
		arg.ID,
		arg.OwnerID,
		arg.ParentID,
		arg.Name,
		arg.NodeType,
		arg.SizeBytes,
		arg.MimeType,
		now,
		now,
	)

	var node models.Node
	err := row.Scan(
		&node.ID,
		&node.OwnerID,
		&node.ParentID,
		&node.Name,
		&node.NodeType,
		&node.SizeBytes,
		&node.MimeType,
		&node.CreatedAt,
		&node.ModifiedAt,
		&node.DeletedAt,
		&node.OriginalParentID,
	)
	if err != nil {
		return nil, err
	}

	return &node, nil
}

func (s *PostgresStore) GetNodesByParentID(ctx context.Context, ownerID int64, parentID *string) ([]models.Node, error) {
	var query string
	var rows pgx.Rows
	var err error

	if parentID == nil {
		query = `SELECT id, name, node_type, size_bytes, mime_type, created_at, modified_at 
				 FROM nodes 
				 WHERE owner_id = $1 AND parent_id IS NULL AND deleted_at IS NULL
				 ORDER BY node_type DESC, name`
		rows, err = s.pool.Query(ctx, query, ownerID)
	} else {
		query = `SELECT id, name, node_type, size_bytes, mime_type, created_at, modified_at 
				 FROM nodes 
				 WHERE owner_id = $1 AND parent_id = $2 AND deleted_at IS NULL
				 ORDER BY node_type DESC, name`
		rows, err = s.pool.Query(ctx, query, ownerID, *parentID)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var node models.Node
		err := rows.Scan(
			&node.ID,
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

func (s *PostgresStore) NodeExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM nodes WHERE id = $1)"
	err := s.pool.QueryRow(ctx, query, id).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (s *PostgresStore) GetNodeByID(ctx context.Context, id string, ownerID int64) (*models.Node, error) {
	query := `
		SELECT id, owner_id, parent_id, name, node_type, size_bytes, mime_type
		FROM nodes
		WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL
	`
	var node models.Node

	err := s.pool.QueryRow(ctx, query, id, ownerID).Scan(
		&node.ID,
		&node.OwnerID,
		&node.ParentID,
		&node.Name,
		&node.NodeType,
		&node.SizeBytes,
		&node.MimeType,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &node, nil
}

func (s *PostgresStore) MoveNodeToTrash(ctx context.Context, id string, ownerID int64) (bool, error) {
	query := `
		WITH RECURSIVE nodes_to_delete AS (
			SELECT n.id
			FROM nodes n
			WHERE n.id = $1 AND n.owner_id = $2 AND n.deleted_at IS NULL
			
			UNION ALL
			
			SELECT n.id
			FROM nodes n
			INNER JOIN nodes_to_delete ntd ON n.parent_id = ntd.id
		)
		UPDATE nodes
		SET 
			deleted_at = $3,
			original_parent_id = parent_id,
			parent_id = NULL
		WHERE id IN (SELECT id FROM nodes_to_delete)
	`

	now := time.Now()
	res, err := s.pool.Exec(ctx, query, id, ownerID, now)
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, nil
}

func (s *PostgresStore) PurgeTrash(ctx context.Context, ownerID int64) ([]string, error) {
	query := `
		DELETE FROM nodes
		WHERE owner_id = $1 AND deleted_at IS NOT NULL
		RETURNING id, node_type
	`

	rows, err := s.pool.Query(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deletedFileIDs []string
	for rows.Next() {
		var id string
		var nodeType string
		if err := rows.Scan(&id, &nodeType); err != nil {
			return nil, err
		}
		if nodeType == "file" {
			deletedFileIDs = append(deletedFileIDs, id)
		}
	}

	return deletedFileIDs, nil
}

func (s *PostgresStore) RenameNode(ctx context.Context, id string, ownerID int64, newName string) (bool, error) {
	query := `
		UPDATE nodes
		SET name = $1, modified_at = $2
		WHERE id = $3 AND owner_id = $4 AND deleted_at IS NULL
	`
	now := time.Now()
	res, err := s.pool.Exec(ctx, query, newName, now, id, ownerID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, ErrDuplicateNodeName
		}
		return false, err
	}

	return res.RowsAffected() > 0, nil
}

func (s *PostgresStore) MoveNode(ctx context.Context, id string, ownerID int64, newParentID *string) (bool, error) {
	query := `
		UPDATE nodes
		SET parent_id = $1, modified_at = $2
		WHERE id = $3 AND owner_id = $4 AND deleted_at IS NULL
	`
	now := time.Now()
	res, err := s.pool.Exec(ctx, query, newParentID, now, id, ownerID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return false, fmt.Errorf("target folder does not exist")
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, ErrDuplicateNodeName
		}
		return false, err
	}

	return res.RowsAffected() > 0, nil
}

func (s *PostgresStore) ListTrash(ctx context.Context, ownerID int64) ([]models.Node, error) {
	query := `
		SELECT id, name, node_type, size_bytes, mime_type, created_at, modified_at, deleted_at
		FROM nodes
		WHERE owner_id = $1 AND deleted_at IS NOT NULL
		ORDER BY deleted_at DESC
	`
	rows, err := s.pool.Query(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var node models.Node
		err := rows.Scan(
			&node.ID,
			&node.Name,
			&node.NodeType,
			&node.SizeBytes,
			&node.MimeType,
			&node.CreatedAt,
			&node.ModifiedAt,
			&node.DeletedAt,
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

// TODO: Ta funkcja nie obsługuje rekurencyjnego przywracania! Przywraca tylko jeden node.
func (s *PostgresStore) RestoreNode(ctx context.Context, id string, ownerID int64) (bool, error) {
	query := `
		UPDATE nodes
		SET 
			deleted_at = NULL,
			parent_id = original_parent_id,
			original_parent_id = NULL
		WHERE id = $1 AND owner_id = $2 AND deleted_at IS NOT NULL
	`
	res, err := s.pool.Exec(ctx, query, id, ownerID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, ErrDuplicateNodeName
		}
		return false, err
	}

	return res.RowsAffected() > 0, nil
}

func (s *PostgresStore) GetNodeIfAccessible(ctx context.Context, nodeID string, userID int64) (*models.Node, error) {
	query := `
		SELECT id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at
		FROM nodes
		WHERE id = $1 AND deleted_at IS NULL
	`
	var node models.Node
	err := s.pool.QueryRow(ctx, query, nodeID).Scan(
		&node.ID, &node.OwnerID, &node.ParentID, &node.Name, &node.NodeType,
		&node.SizeBytes, &node.MimeType, &node.CreatedAt, &node.ModifiedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if node.OwnerID == userID {
		return &node, nil
	}

	hasAccess, err := s.HasAccessToNode(ctx, nodeID, userID)
	if err != nil {
		return nil, err
	}
	if hasAccess {
		return &node, nil
	}

	return nil, nil
}
