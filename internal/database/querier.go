package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"serwer-plikow/internal/models"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

type Queries struct {
	db DBTX
}

func New(db DBTX) *Queries {
	return &Queries{db: db}
}

func (q *Queries) LogEvent(ctx context.Context, userID int64, eventType string, payload interface{}) error {
	eventMsg := map[string]interface{}{
		"event_type": eventType,
		"payload":    payload,
	}
	eventBytes, err := json.Marshal(eventMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal event payload: %w", err)
	}

	query := `INSERT INTO event_journal (user_id, event_type, payload) VALUES ($1, $2, $3)`
	_, err = q.db.Exec(ctx, query, userID, eventType, eventBytes)
	if err != nil {
		return err
	}

	return nil
}

type Event struct {
	ID        int64           `json:"id"`
	EventType string          `json:"event_type"`
	EventTime time.Time       `json:"event_time"`
	Payload   json.RawMessage `json:"payload"`
}

func (q *Queries) GetEventsSince(ctx context.Context, userID int64, sinceID int64) ([]Event, error) {
	query := `
		SELECT id, event_type, event_time, payload
		FROM event_journal
		WHERE user_id = $1 AND id > $2
		ORDER BY id ASC
		LIMIT 100
	`
	rows, err := q.db.Query(ctx, query, userID, sinceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		err := rows.Scan(
			&event.ID,
			&event.EventType,
			&event.EventTime,
			&event.Payload,
		)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if events == nil {
		return []Event{}, nil
	}

	return events, nil
}

var ErrFavoriteAlreadyExists = errors.New("this node is already in favorites")

func (q *Queries) AddFavorite(ctx context.Context, userID int64, nodeID string) error {
	node, err := q.GetNodeIfAccessible(ctx, nodeID, userID)
	if err != nil {
		return err
	}
	if node == nil {
		return ErrNodeNotFound
	}

	query := `INSERT INTO user_favorites (user_id, node_id) VALUES ($1, $2)`
	_, err = q.db.Exec(ctx, query, userID, nodeID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrFavoriteAlreadyExists
		}
		return err
	}

	return nil
}

func (q *Queries) RemoveFavorite(ctx context.Context, userID int64, nodeID string) (bool, error) {
	query := `DELETE FROM user_favorites WHERE user_id = $1 AND node_id = $2`
	res, err := q.db.Exec(ctx, query, userID, nodeID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

func (q *Queries) ListFavorites(ctx context.Context, userID int64, limit int, offset int) ([]models.Node, error) {
	query := `
		SELECT 
			n.id, n.owner_id, n.parent_id, n.name, n.node_type, 
			n.size_bytes, n.mime_type, n.created_at, n.modified_at
		FROM nodes n
		JOIN user_favorites f ON n.id = f.node_id
		WHERE f.user_id = $1 AND n.deleted_at IS NULL
		ORDER BY n.name LIMIT $2 OFFSET $3
	`
	rows, err := q.db.Query(ctx, query, userID, limit, offset)
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

var ErrNodeNotFound = errors.New("node not found or user is not the owner")
var ErrShareAlreadyExists = errors.New("this node is already shared with the recipient")
var ErrRecipientNotFound = errors.New("recipient user not found")

type ShareNodeParams struct {
	NodeID      string
	SharerID    int64
	RecipientID int64
	Permissions string
}

func (q *Queries) ShareNode(ctx context.Context, arg ShareNodeParams) (*models.Share, error) {
	query := `
		INSERT INTO shares (node_id, sharer_id, recipient_id, permissions)
		VALUES ($1, $2, $3, $4)
		RETURNING id, node_id, sharer_id, recipient_id, permissions, shared_at
	`
	row := q.db.QueryRow(ctx, query, arg.NodeID, arg.SharerID, arg.RecipientID, arg.Permissions)

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

func (q *Queries) GetSharingUsers(ctx context.Context, recipientID int64, limit int, offset int) ([]SharingUser, error) {
	query := `
		SELECT DISTINCT ON (u.id)
			u.id,
			u.username,
			u.display_name
		FROM shares s
		JOIN users u ON s.sharer_id = u.id
		WHERE s.recipient_id = $1
		ORDER BY u.id LIMIT $2 OFFSET $3
	`
	rows, err := q.db.Query(ctx, query, recipientID, limit, offset)
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

func (q *Queries) ListDirectlySharedNodes(ctx context.Context, recipientID int64, sharerID int64, limit int, offset int) ([]models.Node, error) {
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
		ORDER BY n.node_type DESC, n.name LIMIT $3 OFFSET $4
	`

	rows, err := q.db.Query(ctx, query, recipientID, sharerID, limit, offset)
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

func (q *Queries) HasAccessToNode(ctx context.Context, nodeID string, recipientID int64) (bool, error) {
	query := `
		WITH RECURSIVE node_parents AS (
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
	err := q.db.QueryRow(ctx, query, nodeID, recipientID).Scan(&hasAccess)
	return hasAccess, err
}

type OutgoingShare struct {
	models.Share
	NodeName          string `json:"node_name"`
	NodeType          string `json:"node_type"`
	RecipientUsername string `json:"recipient_username"`
}

func (q *Queries) GetOutgoingShares(ctx context.Context, sharerID int64, limit int, offset int) ([]OutgoingShare, error) {
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
		ORDER BY s.shared_at DESC LIMIT $2 OFFSET $3
	`
	rows, err := q.db.Query(ctx, query, sharerID, limit, offset)
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

func (q *Queries) DeleteShare(ctx context.Context, shareID int64, sharerID int64) error {
	query := `DELETE FROM shares WHERE id = $1 AND sharer_id = $2`
	_, err := q.db.Exec(ctx, query, shareID, sharerID)
	return err
}

func (q *Queries) GetShareByID(ctx context.Context, shareID int64, sharerID int64) (*models.Share, error) {
	query := `
		SELECT id, node_id, sharer_id, recipient_id, permissions, shared_at
		FROM shares
		WHERE id = $1 AND sharer_id = $2
	`
	var share models.Share
	err := q.db.QueryRow(ctx, query, shareID, sharerID).Scan(
		&share.ID,
		&share.NodeID,
		&share.SharerID,
		&share.RecipientID,
		&share.Permissions,
		&share.SharedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}

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

func (q *Queries) CreateNode(ctx context.Context, arg CreateNodeParams) (*models.Node, error) {
	query := `
		INSERT INTO nodes (id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at, deleted_at, original_parent_id
	`
	now := time.Now()

	row := q.db.QueryRow(ctx, query,
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

func (q *Queries) GetNodesByParentID(ctx context.Context, ownerID int64, parentID *string, limit int, offset int) ([]models.Node, error) {
	var query string
	var rows pgx.Rows
	var err error

	if parentID == nil {
		query = `SELECT id, name, node_type, size_bytes, mime_type, created_at, modified_at 
				 FROM nodes 
				 WHERE owner_id = $1 AND parent_id IS NULL AND deleted_at IS NULL
				 ORDER BY node_type DESC, name
				 LIMIT $2 OFFSET $3`
		rows, err = q.db.Query(ctx, query, ownerID, limit, offset)
	} else {
		query = `SELECT id, name, node_type, size_bytes, mime_type, created_at, modified_at 
				 FROM nodes 
				 WHERE owner_id = $1 AND parent_id = $2 AND deleted_at IS NULL
				 ORDER BY node_type DESC, name
				 LIMIT $3 OFFSET $4`
		rows, err = q.db.Query(ctx, query, ownerID, *parentID, limit, offset)
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

func (q *Queries) NodeExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM nodes WHERE id = $1)"
	err := q.db.QueryRow(ctx, query, id).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (q *Queries) GetNodeByID(ctx context.Context, id string, ownerID int64) (*models.Node, error) {
	query := `
		SELECT id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at
		FROM nodes
		WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL
	`
	var node models.Node

	err := q.db.QueryRow(ctx, query, id, ownerID).Scan(
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &node, nil
}

func (q *Queries) MoveNodeToTrash(ctx context.Context, id string, ownerID int64) (bool, error) {
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
	res, err := q.db.Exec(ctx, query, id, ownerID, now)
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, nil
}

func (q *Queries) UpdateUserStorage(ctx context.Context, userID int64, bytesChange int64) error {
	query := `
		UPDATE users
		SET storage_used_bytes = storage_used_bytes + $1
		WHERE id = $2
	`
	_, err := q.db.Exec(ctx, query, bytesChange, userID)
	return err
}

func (q *Queries) PurgeTrash(ctx context.Context, ownerID int64) ([]string, int64, error) {
	query := `
		WITH deleted_nodes AS (
			DELETE FROM nodes
			WHERE owner_id = $1 AND deleted_at IS NOT NULL
			RETURNING id, node_type, size_bytes
		)
		SELECT 
			id, 
			COALESCE((SELECT sum(size_bytes) FROM deleted_nodes WHERE node_type = 'file'), 0)
		FROM deleted_nodes
		WHERE node_type = 'file'
	`

	rows, err := q.db.Query(ctx, query, ownerID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var deletedFileIDs []string
	var totalSizeFreed int64 = 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id, &totalSizeFreed); err != nil {
			return nil, 0, err
		}
		deletedFileIDs = append(deletedFileIDs, id)
	}

	return deletedFileIDs, totalSizeFreed, nil
}

func (q *Queries) RenameNode(ctx context.Context, id string, ownerID int64, newName string) (bool, error) {
	query := `
		UPDATE nodes
		SET name = $1, modified_at = $2
		WHERE id = $3 AND owner_id = $4 AND deleted_at IS NULL
	`
	now := time.Now()
	res, err := q.db.Exec(ctx, query, newName, now, id, ownerID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, ErrDuplicateNodeName
		}
		return false, err
	}

	return res.RowsAffected() > 0, nil
}

func (q *Queries) MoveNode(ctx context.Context, id string, ownerID int64, newParentID *string) (bool, error) {
	query := `
		UPDATE nodes
		SET parent_id = $1, modified_at = $2
		WHERE id = $3 AND owner_id = $4 AND deleted_at IS NULL
	`
	now := time.Now()
	res, err := q.db.Exec(ctx, query, newParentID, now, id, ownerID)
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

func (q *Queries) ListTrash(ctx context.Context, ownerID int64, limit int, offset int) ([]models.Node, error) {
	query := `
		SELECT id, name, node_type, size_bytes, mime_type, created_at, modified_at, deleted_at
		FROM nodes
		WHERE owner_id = $1 AND deleted_at IS NOT NULL
		ORDER BY deleted_at DESC LIMIT $2 OFFSET $3
	`
	rows, err := q.db.Query(ctx, query, ownerID, limit, offset)
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
func (q *Queries) RestoreNode(ctx context.Context, id string, ownerID int64) (bool, error) {
	query := `
		UPDATE nodes
		SET 
			deleted_at = NULL,
			parent_id = original_parent_id,
			original_parent_id = NULL
		WHERE id = $1 AND owner_id = $2 AND deleted_at IS NOT NULL
	`
	res, err := q.db.Exec(ctx, query, id, ownerID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, ErrDuplicateNodeName
		}
		return false, err
	}

	return res.RowsAffected() > 0, nil
}

func (q *Queries) GetNodeIfAccessible(ctx context.Context, nodeID string, userID int64) (*models.Node, error) {
	query := `
		SELECT id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at
		FROM nodes
		WHERE id = $1 AND deleted_at IS NULL
	`
	var node models.Node
	err := q.db.QueryRow(ctx, query, nodeID).Scan(
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

	hasAccess, err := q.HasAccessToNode(ctx, nodeID, userID)
	if err != nil {
		return nil, err
	}
	if hasAccess {
		return &node, nil
	}

	return nil, nil
}

func (q *Queries) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	query := `
		SELECT 
			id, 
			username, 
			password_hash, 
			display_name, 
			created_at, 
			storage_quota_bytes, 
			storage_used_bytes
		FROM users
		WHERE username = $1
	`
	var user models.User

	err := q.db.QueryRow(ctx, query, username).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.DisplayName,
		&user.CreatedAt,
		&user.StorageQuotaBytes,
		&user.StorageUsedBytes,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}

func (q *Queries) IsDescendantOf(ctx context.Context, nodeId string, potentialParentId string) (bool, error) {
	if nodeId == potentialParentId {
		return true, nil
	}

	query := `
		WITH RECURSIVE node_children AS (
			SELECT id FROM nodes WHERE id = $1

			UNION ALL

			SELECT n.id
			FROM nodes n
			JOIN node_children nc ON n.parent_id = nc.id
		)
		SELECT EXISTS (
			SELECT 1
			FROM node_children
			WHERE id = $2
		);
	`
	var isDescendant bool
	err := q.db.QueryRow(ctx, query, nodeId, potentialParentId).Scan(&isDescendant)
	return isDescendant, err
}

type CreateSessionParams struct {
	ID           uuid.UUID
	UserID       int64
	RefreshToken string
	UserAgent    string
	ClientIP     string
	ExpiresAt    time.Time
}

func (q *Queries) CreateSession(ctx context.Context, arg CreateSessionParams) error {
	query := `
		INSERT INTO sessions (id, user_id, refresh_token, user_agent, client_ip, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := q.db.Exec(ctx, query, arg.ID, arg.UserID, arg.RefreshToken, arg.UserAgent, arg.ClientIP, arg.ExpiresAt)
	return err
}

func (q *Queries) GetUserByRefreshToken(ctx context.Context, refreshToken string) (*models.User, error) {
	query := `
		SELECT 
			u.id, u.username, u.password_hash, u.display_name, u.created_at, 
			u.storage_quota_bytes, u.storage_used_bytes
		FROM users u
		JOIN sessions s ON u.id = s.user_id
		WHERE s.refresh_token = $1 AND s.expires_at > NOW()
	`
	var user models.User
	err := q.db.QueryRow(ctx, query, refreshToken).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.CreatedAt,
		&user.StorageQuotaBytes, &user.StorageUsedBytes,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (q *Queries) ListSessionsForUser(ctx context.Context, userID int64) ([]models.Session, error) {
	query := `
		SELECT id, user_agent, client_ip, expires_at, created_at
		FROM sessions
		WHERE user_id = $1 AND expires_at > NOW()
		ORDER BY created_at DESC
	`
	rows, err := q.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var session models.Session
		if err := rows.Scan(
			&session.ID,
			&session.UserAgent,
			&session.ClientIP,
			&session.ExpiresAt,
			&session.CreatedAt,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if sessions == nil {
		return []models.Session{}, nil
	}

	return sessions, nil
}

func (q *Queries) DeleteSessionByID(ctx context.Context, sessionID uuid.UUID, userID int64) error {
	query := `DELETE FROM sessions WHERE id = $1 AND user_id = $2`
	_, err := q.db.Exec(ctx, query, sessionID, userID)
	return err
}

func (q *Queries) DeleteAllSessionsForUser(ctx context.Context, userID int64) error {
	query := `DELETE FROM sessions WHERE user_id = $1`
	_, err := q.db.Exec(ctx, query, userID)
	return err
}

func (q *Queries) DeleteSessionByRefreshToken(ctx context.Context, refreshToken string) error {
	query := `DELETE FROM sessions WHERE refresh_token = $1`
	_, err := q.db.Exec(ctx, query, refreshToken)
	return err
}

func (q *Queries) UpdateUserPassword(ctx context.Context, userID int64, newPasswordHash string) error {
	query := `UPDATE users SET password_hash = $1 WHERE id = $2`
	_, err := q.db.Exec(ctx, query, newPasswordHash, userID)
	return err
}

func (q *Queries) CheckWritePermission(ctx context.Context, userID int64, parentID *string) (bool, error) {
	if parentID == nil {
		return true, nil
	}

	query := `
		WITH RECURSIVE node_parents AS (
			SELECT id, parent_id, owner_id
			FROM nodes
			WHERE id = $1

			UNION ALL

			SELECT n.id, n.parent_id, n.owner_id
			FROM nodes n
			JOIN node_parents np ON n.id = np.parent_id
		)
		SELECT EXISTS (
			SELECT 1 FROM node_parents WHERE owner_id = $2
			LIMIT 1
		) OR EXISTS (
			SELECT 1
			FROM shares s
			WHERE s.recipient_id = $2 AND s.permissions = 'write' AND s.node_id IN (SELECT id FROM node_parents)
			LIMIT 1
		)
	`
	var hasPermission bool
	err := q.db.QueryRow(ctx, query, *parentID, userID).Scan(&hasPermission)
	return hasPermission, err
}

func (q *Queries) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	query := `
		SELECT 
			id, username, password_hash, display_name, created_at, 
			storage_quota_bytes, storage_used_bytes
		FROM users
		WHERE id = $1
	`
	var user models.User
	err := q.db.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.CreatedAt,
		&user.StorageQuotaBytes, &user.StorageUsedBytes,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}
