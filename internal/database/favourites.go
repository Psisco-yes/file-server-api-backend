package database

import (
	"context"
	"errors"
	"serwer-plikow/internal/models"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrFavoriteAlreadyExists = errors.New("this node is already in favorites")

func (s *PostgresStore) AddFavorite(ctx context.Context, userID int64, nodeID string) error {
	node, err := s.GetNodeIfAccessible(ctx, nodeID, userID)
	if err != nil {
		return err
	}
	if node == nil {
		return ErrNodeNotFound
	}

	query := `INSERT INTO user_favorites (user_id, node_id) VALUES ($1, $2)`
	_, err = s.pool.Exec(ctx, query, userID, nodeID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return ErrFavoriteAlreadyExists
		}
		return err
	}

	return nil
}

func (s *PostgresStore) RemoveFavorite(ctx context.Context, userID int64, nodeID string) (bool, error) {
	query := `DELETE FROM user_favorites WHERE user_id = $1 AND node_id = $2`
	res, err := s.pool.Exec(ctx, query, userID, nodeID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

func (s *PostgresStore) ListFavorites(ctx context.Context, userID int64) ([]models.Node, error) {
	query := `
		SELECT 
			n.id, n.owner_id, n.parent_id, n.name, n.node_type, 
			n.size_bytes, n.mime_type, n.created_at, n.modified_at
		FROM nodes n
		JOIN user_favorites f ON n.id = f.node_id
		WHERE f.user_id = $1 AND n.deleted_at IS NULL
		ORDER BY n.name
	`
	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var node models.Node
		err := rows.Scan(
			&node.ID, &node.OwnerID, &node.ParentID, &node.Name, &node.NodeType,
			&node.SizeBytes, &node.MimeType, &node.CreatedAt, &node.ModifiedAt,
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
