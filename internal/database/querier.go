package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"serwer-plikow/internal/models"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
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

func (q *Queries) ListRichDirectlySharedNodes(ctx context.Context, recipientID int64, sharerID int64, limit int, offset int, sortBy string, sortOrder string) ([]*models.RichNode, error) {
	orderByClause := buildOrderByClause(sortBy, sortOrder)

	query := fmt.Sprintf(`
		SELECT %s
		FROM nodes n
		JOIN users u ON n.owner_id = u.id
		JOIN shares s ON n.id = s.node_id
		LEFT JOIN user_favorites fav ON n.id = fav.node_id AND fav.user_id = $1
		WHERE s.recipient_id = $1 AND s.sharer_id = $2 AND n.deleted_at IS NULL
		%s
		LIMIT $3 OFFSET $4
	`, richNodeFields, orderByClause)

	rows, err := q.db.Query(ctx, query, recipientID, sharerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*models.RichNode
	for rows.Next() {
		node, err := scanRichNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	if nodes == nil {
		return []*models.RichNode{}, nil
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

func (q *Queries) DeleteShare(ctx context.Context, shareID int64, sharerID int64) (bool, error) {
	query := `DELETE FROM shares WHERE id = $1 AND sharer_id = $2`
	res, err := q.db.Exec(ctx, query, shareID, sharerID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
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

func (q *Queries) GetNodesByParentIDSimple(ctx context.Context, ownerID int64, parentID *string) ([]models.Node, error) {
	query := `
		SELECT id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at
		FROM nodes 
		WHERE owner_id = $1 AND parent_id = $2 AND deleted_at IS NULL
		ORDER BY node_type DESC, name`

	rows, err := q.db.Query(ctx, query, ownerID, parentID)
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
	findQuery := `
		WITH RECURSIVE nodes_to_process AS (
			SELECT n.id
			FROM nodes n
			WHERE n.id = $1 AND n.owner_id = $2 AND n.deleted_at IS NULL
			
			UNION ALL
			
			SELECT n.id
			FROM nodes n
			INNER JOIN nodes_to_process ntp ON n.parent_id = ntp.id
		)
		SELECT id FROM nodes_to_process
	`
	rows, err := q.db.Query(ctx, findQuery, id, ownerID)
	if err != nil {
		return false, err
	}
	nodeIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, err
	}

	if len(nodeIDs) == 0 {
		return false, nil
	}

	now := time.Now()

	updateQuery := `
		UPDATE nodes
		SET deleted_at = $1, original_parent_id = parent_id
		WHERE id = ANY($2)
	`
	_, err = q.db.Exec(ctx, updateQuery, now, nodeIDs)
	if err != nil {
		return false, err
	}

	deleteSharesQuery := `DELETE FROM shares WHERE node_id = ANY($1)`
	_, err = q.db.Exec(ctx, deleteSharesQuery, nodeIDs)
	if err != nil {
		return false, err
	}

	return true, nil
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

func (q *Queries) RestoreNode(ctx context.Context, nodeID string, ownerID int64, renameOnConflict bool) ([]string, int64, error) {
	var originalName string
	var originalParentID *string
	err := q.db.QueryRow(ctx, "SELECT name, original_parent_id FROM nodes WHERE id = $1 AND owner_id = $2 AND deleted_at IS NOT NULL", nodeID, ownerID).Scan(&originalName, &originalParentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, ErrNodeNotFound
		}
		return nil, 0, err
	}

	var conflictExists bool
	if originalParentID == nil {
		checkConflictQuery := `SELECT EXISTS(SELECT 1 FROM nodes WHERE owner_id = $1 AND parent_id IS NULL AND name = $2 AND deleted_at IS NULL)`
		err = q.db.QueryRow(ctx, checkConflictQuery, ownerID, originalName).Scan(&conflictExists)
	} else {
		checkConflictQuery := `SELECT EXISTS(SELECT 1 FROM nodes WHERE owner_id = $1 AND parent_id = $2 AND name = $3 AND deleted_at IS NULL)`
		err = q.db.QueryRow(ctx, checkConflictQuery, ownerID, *originalParentID, originalName).Scan(&conflictExists)
	}
	if err != nil {
		return nil, 0, err
	}

	finalName := originalName
	if conflictExists {
		if !renameOnConflict {
			return nil, 0, ErrDuplicateNodeName
		}
		finalName, err = q.findAvailableName(ctx, ownerID, originalParentID, originalName)
		if err != nil {
			return nil, 0, err
		}
	}

	query := `
		WITH RECURSIVE nodes_to_restore AS (
			SELECT id FROM nodes WHERE id = $2 AND owner_id = $1 AND deleted_at IS NOT NULL
			UNION ALL
			SELECT n.id FROM nodes n JOIN nodes_to_restore ntr ON n.original_parent_id = ntr.id
		)
		UPDATE nodes
		SET
			deleted_at = NULL,
			parent_id = original_parent_id,
			original_parent_id = NULL,
			name = CASE WHEN id = $2 THEN $3 ELSE name END
		WHERE id IN (SELECT id FROM nodes_to_restore)
		RETURNING id
	`

	rows, err := q.db.Query(ctx, query, ownerID, nodeID, finalName)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var restoredIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		restoredIDs = append(restoredIDs, id)
	}

	if restoredIDs == nil {
		return []string{}, 0, ErrNodeNotFound
	}
	return restoredIDs, int64(len(restoredIDs)), nil
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
	if potentialParentId == "" {
		return false, nil
	}

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

func (q *Queries) DeleteSessionByID(ctx context.Context, sessionID uuid.UUID, userID int64) (bool, error) {
	query := `DELETE FROM sessions WHERE id = $1 AND user_id = $2`
	res, err := q.db.Exec(ctx, query, sessionID, userID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
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

func (q *Queries) GetSubtree(ctx context.Context, nodeID string) ([]models.Node, error) {
	query := `
		WITH RECURSIVE node_subtree AS (
			SELECT *
			FROM nodes
			WHERE id = $1

			UNION ALL

			SELECT n.*
			FROM nodes n
			JOIN node_subtree st ON n.parent_id = st.id
		)
		SELECT * FROM node_subtree WHERE id != $1 AND deleted_at IS NULL;
	`

	rows, err := q.db.Query(ctx, query, nodeID)
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
			&node.DeletedAt, &node.OriginalParentID,
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

func (q *Queries) SearchNodes(ctx context.Context, userID int64, query string, limit int, offset int) ([]models.Node, error) {
	searchQuery := "%" + query + "%"

	sql := `
		SELECT id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at
		FROM nodes
		WHERE owner_id = $1 AND name ILIKE $2 AND deleted_at IS NULL

		UNION

		SELECT n.id, n.owner_id, n.parent_id, n.name, n.node_type, n.size_bytes, n.mime_type, n.created_at, n.modified_at
		FROM nodes n
		WHERE n.name ILIKE $2 AND n.deleted_at IS NULL AND EXISTS (
			WITH RECURSIVE node_parents AS (
				SELECT id, parent_id FROM nodes WHERE id = n.id
				UNION ALL
				SELECT np_n.id, np_n.parent_id FROM nodes np_n JOIN node_parents np ON np_n.id = np.parent_id
			)
			SELECT 1
			FROM shares s
			WHERE s.recipient_id = $1 AND s.node_id IN (SELECT id FROM node_parents)
		)
		ORDER BY name
		LIMIT $3 OFFSET $4;
	`
	rows, err := q.db.Query(ctx, sql, userID, searchQuery, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var node models.Node
		if err := rows.Scan(
			&node.ID, &node.OwnerID, &node.ParentID, &node.Name, &node.NodeType,
			&node.SizeBytes, &node.MimeType, &node.CreatedAt, &node.ModifiedAt,
		); err != nil {
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

type UpdateUserParams struct {
	ID          int64
	DisplayName *string
}

func (q *Queries) UpdateUser(ctx context.Context, arg UpdateUserParams) error {
	if arg.DisplayName != nil {
		query := `UPDATE users SET display_name = $1 WHERE id = $2`
		_, err := q.db.Exec(ctx, query, *arg.DisplayName, arg.ID)
		return err
	}
	return nil
}

func (q *Queries) GetNodePath(ctx context.Context, nodeID string) ([]models.Node, error) {
	query := `
		WITH RECURSIVE node_path AS (
			SELECT * FROM nodes WHERE id = (SELECT parent_id FROM nodes WHERE id = $1)
			UNION ALL
			SELECT n.* FROM nodes n JOIN node_path np ON n.id = np.parent_id
		)
		SELECT id, owner_id, parent_id, name, node_type, size_bytes, mime_type, created_at, modified_at 
		FROM node_path
		WHERE deleted_at IS NULL;
	`
	rows, err := q.db.Query(ctx, query, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pathNodes []models.Node
	for rows.Next() {
		var node models.Node
		if err := rows.Scan(
			&node.ID, &node.OwnerID, &node.ParentID, &node.Name, &node.NodeType,
			&node.SizeBytes, &node.MimeType, &node.CreatedAt, &node.ModifiedAt,
		); err != nil {
			return nil, err
		}
		pathNodes = append(pathNodes, node)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(pathNodes)-1; i < j; i, j = i+1, j-1 {
		pathNodes[i], pathNodes[j] = pathNodes[j], pathNodes[i]
	}

	if pathNodes == nil {
		return []models.Node{}, nil
	}
	return pathNodes, nil
}

func (q *Queries) GetSharesForNode(ctx context.Context, nodeID string, ownerID int64) ([]OutgoingShare, error) {
	query := `
		SELECT 
			s.id, s.node_id, s.sharer_id, s.recipient_id, s.permissions, s.shared_at,
			n.name AS node_name,
			n.node_type AS node_type,
			u.username AS recipient_username
		FROM shares s
		JOIN nodes n ON s.node_id = n.id
		JOIN users u ON s.recipient_id = u.id
		WHERE s.node_id = $1 AND s.sharer_id = $2
		ORDER BY u.username
	`
	rows, err := q.db.Query(ctx, query, nodeID, ownerID)
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

func (q *Queries) GetLatestEventID(ctx context.Context, userID int64) (int64, error) {
	query := `SELECT MAX(id) FROM event_journal WHERE user_id = $1`

	var latestID pgtype.Int8
	err := q.db.QueryRow(ctx, query, userID).Scan(&latestID)
	if err != nil {
		return 0, err
	}

	if !latestID.Valid {
		return 0, nil
	}

	return latestID.Int64, nil
}

const richNodeFields = `
    n.id, n.parent_id, n.original_parent_id, n.name, n.node_type, n.size_bytes, n.mime_type, n.created_at, n.modified_at,
    n.owner_id, u.username, u.display_name,
    (CASE WHEN fav.user_id IS NOT NULL THEN TRUE ELSE FALSE END) as is_favorited,
    EXISTS (SELECT 1 FROM shares s WHERE s.node_id = n.id) as is_shared
`

func scanRichNode(rows pgx.Rows) (*models.RichNode, error) {
	var node models.RichNode
	err := rows.Scan(
		&node.ID, &node.ParentID, &node.OriginalParentID, &node.Name, &node.NodeType, &node.SizeBytes, &node.MimeType, &node.CreatedAt, &node.ModifiedAt,
		&node.Owner.ID, &node.Owner.Username, &node.Owner.DisplayName,
		&node.IsFavorited,
		&node.IsShared,
	)
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func (q *Queries) GetRichNodeIfAccessible(ctx context.Context, nodeID string, userID int64) (*models.RichNode, error) {
	accessibleNode, err := q.GetNodeIfAccessible(ctx, nodeID, userID)
	if err != nil || accessibleNode == nil {
		return nil, err
	}

	query := fmt.Sprintf(`
        SELECT %s
        FROM nodes n
        JOIN users u ON n.owner_id = u.id
        LEFT JOIN user_favorites fav ON n.id = fav.node_id AND fav.user_id = $2
        WHERE n.id = $1 AND n.deleted_at IS NULL
    `, richNodeFields)

	rows, err := q.db.Query(ctx, query, nodeID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		return scanRichNode(rows)
	}

	return nil, nil
}

func (q *Queries) GetRichNodesByParentID(ctx context.Context, ownerID int64, requesterID int64, parentID *string, limit int, offset int, sortBy string, sortOrder string) ([]*models.RichNode, error) {
	var query string
	var args []interface{}

	orderByClause := buildOrderByClause(sortBy, sortOrder)

	baseQuery := fmt.Sprintf(`
		SELECT %s
		FROM nodes n
		JOIN users u ON n.owner_id = u.id
		LEFT JOIN user_favorites fav ON n.id = fav.node_id AND fav.user_id = $1
	`, richNodeFields)

	args = append(args, requesterID)

	if parentID == nil {
		query = fmt.Sprintf(`%s WHERE n.owner_id = $2 AND n.parent_id IS NULL AND n.deleted_at IS NULL
                             %s LIMIT $3 OFFSET $4`, baseQuery, orderByClause)
		args = append(args, ownerID, limit, offset)
	} else {
		query = fmt.Sprintf(`%s WHERE n.owner_id = $2 AND n.parent_id = $3 AND n.deleted_at IS NULL
                             %s LIMIT $4 OFFSET $5`, baseQuery, orderByClause)
		args = append(args, ownerID, *parentID, limit, offset)
	}

	rows, err := q.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*models.RichNode
	for rows.Next() {
		node, err := scanRichNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	if nodes == nil {
		return []*models.RichNode{}, nil
	}

	return nodes, nil
}

func (q *Queries) GetRichNodePath(ctx context.Context, nodeID string, requesterID int64) ([]*models.RichNode, error) {
	query := fmt.Sprintf(`
		WITH RECURSIVE node_path AS (
			SELECT * FROM nodes WHERE id = (SELECT parent_id FROM nodes WHERE id = $1)
			UNION ALL
			SELECT n.* FROM nodes n JOIN node_path np ON n.id = np.parent_id
		)
		SELECT %s
		FROM node_path n
		JOIN users u ON n.owner_id = u.id
		LEFT JOIN user_favorites fav ON n.id = fav.node_id AND fav.user_id = $2
		WHERE n.deleted_at IS NULL;
	`, richNodeFields)

	rows, err := q.db.Query(ctx, query, nodeID, requesterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pathNodes []*models.RichNode
	for rows.Next() {
		node, err := scanRichNode(rows)
		if err != nil {
			return nil, err
		}
		pathNodes = append(pathNodes, node)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(pathNodes)-1; i < j; i, j = i+1, j-1 {
		pathNodes[i], pathNodes[j] = pathNodes[j], pathNodes[i]
	}

	if pathNodes == nil {
		return []*models.RichNode{}, nil
	}
	return pathNodes, nil
}

func (q *Queries) GetRichFavorites(ctx context.Context, requesterID int64, limit int, offset int, sortBy string, sortOrder string) ([]*models.RichNode, error) {
	orderByClause := buildOrderByClause(sortBy, sortOrder)

	query := fmt.Sprintf(`
		SELECT %s
		FROM nodes n
		JOIN users u ON n.owner_id = u.id
		JOIN user_favorites fav ON n.id = fav.node_id AND fav.user_id = $1
		WHERE n.deleted_at IS NULL
		%s LIMIT $2 OFFSET $3
	`, richNodeFields, orderByClause)

	rows, err := q.db.Query(ctx, query, requesterID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*models.RichNode
	for rows.Next() {
		node, err := scanRichNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	if nodes == nil {
		return []*models.RichNode{}, nil
	}

	return nodes, nil
}

func (q *Queries) GetRichTrash(ctx context.Context, ownerID int64, limit int, offset int, sortBy string, sortOrder string) ([]*models.RichNode, error) {
	orderByClause := buildOrderByClause(sortBy, sortOrder)

	query := fmt.Sprintf(`
		SELECT %s
		FROM nodes n
		JOIN users u ON n.owner_id = u.id
		LEFT JOIN user_favorites fav ON n.id = fav.node_id AND fav.user_id = $1
		WHERE n.owner_id = $1 AND n.deleted_at IS NOT NULL
		%s LIMIT $2 OFFSET $3
	`, richNodeFields, orderByClause)

	rows, err := q.db.Query(ctx, query, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*models.RichNode
	for rows.Next() {
		node, err := scanRichNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	if nodes == nil {
		return []*models.RichNode{}, nil
	}

	return nodes, nil
}

func (q *Queries) SearchRichNodes(ctx context.Context, requesterID int64, searchQuery string, limit int, offset int, sortBy string, sortOrder string) ([]*models.RichNode, error) {
	likeQuery := "%" + searchQuery + "%"
	orderByClause := buildOrderByClause(sortBy, sortOrder)

	query := fmt.Sprintf(`
		WITH accessible_nodes AS (
			SELECT id FROM nodes WHERE owner_id = $1 AND deleted_at IS NULL
			UNION
			SELECT n.id FROM nodes n
			INNER JOIN (
				WITH RECURSIVE shared_subtree AS (
					SELECT id FROM nodes WHERE id IN (SELECT node_id FROM shares WHERE recipient_id = $1)
					UNION ALL
					SELECT c.id FROM nodes c JOIN shared_subtree ss ON c.parent_id = ss.id
				)
				SELECT id FROM shared_subtree
			) AS shared_nodes ON n.id = shared_nodes.id
			WHERE n.deleted_at IS NULL
		)
		SELECT %s
		FROM nodes n
		JOIN users u ON n.owner_id = u.id
		LEFT JOIN user_favorites fav ON n.id = fav.node_id AND fav.user_id = $1
		WHERE n.id IN (SELECT id FROM accessible_nodes) AND n.name ILIKE $2
		%s
		LIMIT $3 OFFSET $4
	`, richNodeFields, orderByClause)

	rows, err := q.db.Query(ctx, query, requesterID, likeQuery, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*models.RichNode
	for rows.Next() {
		node, err := scanRichNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	if nodes == nil {
		return []*models.RichNode{}, nil
	}

	return nodes, nil
}

func buildOrderByClause(sortBy, sortOrder string) string {
	var column string
	switch sortBy {
	case "name":
		column = "n.name"
	case "size":
		column = "n.size_bytes"
	case "modifiedAt":
		column = "n.modified_at"
	default:
		return "ORDER BY n.node_type DESC, n.name ASC"
	}

	order := "ASC"
	if strings.ToUpper(sortOrder) == "DESC" {
		order = "DESC"
	}

	if column == "n.size_bytes" {
		return fmt.Sprintf("ORDER BY n.node_type DESC, %s %s NULLS LAST", column, order)
	}

	return fmt.Sprintf("ORDER BY %s %s", column, order)
}

func (q *Queries) PurgeSingleNode(ctx context.Context, ownerID int64, nodeID string) (int, []string, int64, error) {
	query := `
		WITH RECURSIVE nodes_to_find AS (
			SELECT id, original_parent_id 
			FROM nodes
			WHERE id = $2 AND owner_id = $1 AND deleted_at IS NOT NULL

			UNION ALL

			SELECT n.id, n.original_parent_id
			FROM nodes n
			JOIN nodes_to_find ntf ON n.original_parent_id = ntf.id
		),
		deleted_summary AS (
			DELETE FROM nodes WHERE id IN (SELECT id FROM nodes_to_find)
			RETURNING id, node_type, size_bytes
		)
		SELECT 
			(SELECT COUNT(*) FROM deleted_summary),
			(SELECT array_agg(id) FROM deleted_summary WHERE node_type = 'file'),
			COALESCE((SELECT sum(size_bytes) FROM deleted_summary WHERE node_type = 'file'), 0)
	`

	var totalNodesDeleted int
	var deletedFileIDs []string
	var totalSizeFreed int64

	err := q.db.QueryRow(ctx, query, ownerID, nodeID).Scan(&totalNodesDeleted, &deletedFileIDs, &totalSizeFreed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, []string{}, 0, nil
		}
		return 0, nil, 0, err
	}

	if deletedFileIDs == nil {
		deletedFileIDs = []string{}
	}

	return totalNodesDeleted, deletedFileIDs, totalSizeFreed, nil
}

func (q *Queries) findAvailableName(ctx context.Context, ownerID int64, parentID *string, originalName string) (string, error) {
	baseName := originalName
	extension := ""
	if dotIndex := strings.LastIndex(originalName, "."); dotIndex != -1 {
		baseName = originalName[:dotIndex]
		extension = originalName[dotIndex:]
	}

	for i := 1; i < 100; i++ {
		newName := fmt.Sprintf("%s (%d)%s", baseName, i, extension)

		var exists bool
		query := `SELECT EXISTS(SELECT 1 FROM nodes WHERE owner_id = $1 AND parent_id IS NOT DISTINCT FROM $2 AND name = $3 AND deleted_at IS NULL)`
		err := q.db.QueryRow(ctx, query, ownerID, parentID, newName).Scan(&exists)
		if err != nil {
			return "", err
		}

		if !exists {
			return newName, nil
		}
	}
	return "", fmt.Errorf("could not find an available name for %s after 99 attempts", originalName)
}

type CreateUploadParams struct {
	ID             uuid.UUID
	NodeID         string
	OwnerID        int64
	ParentID       *string
	Name           string
	MimeType       string
	TotalSizeBytes int64
}

func (q *Queries) CreateUpload(ctx context.Context, arg CreateUploadParams) error {
	query := `
		INSERT INTO uploads (id, node_id, owner_id, parent_id, name, mime_type, total_size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := q.db.Exec(ctx, query, arg.ID, arg.NodeID, arg.OwnerID, arg.ParentID, arg.Name, arg.MimeType, arg.TotalSizeBytes)
	return err
}

type Upload struct {
	ID             uuid.UUID
	NodeID         string
	OwnerID        int64
	ParentID       *string
	Name           string
	MimeType       string
	TotalSizeBytes int64
	UploadedBytes  int64
	CreatedAt      time.Time
}

func (q *Queries) GetUploadByID(ctx context.Context, uploadID uuid.UUID) (*Upload, error) {
	query := `SELECT id, node_id, owner_id, parent_id, name, mime_type, total_size_bytes, uploaded_bytes, created_at FROM uploads WHERE id = $1`
	var upload Upload
	err := q.db.QueryRow(ctx, query, uploadID).Scan(
		&upload.ID, &upload.NodeID, &upload.OwnerID, &upload.ParentID, &upload.Name,
		&upload.MimeType, &upload.TotalSizeBytes, &upload.UploadedBytes, &upload.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &upload, nil
}

func (q *Queries) UpdateUploadProgress(ctx context.Context, uploadID uuid.UUID, uploadedBytes int64) error {
	query := `UPDATE uploads SET uploaded_bytes = $1 WHERE id = $2`
	_, err := q.db.Exec(ctx, query, uploadedBytes, uploadID)
	return err
}

func (q *Queries) DeleteUpload(ctx context.Context, uploadID uuid.UUID) error {
	query := `DELETE FROM uploads WHERE id = $1`
	_, err := q.db.Exec(ctx, query, uploadID)
	return err
}

func (q *Queries) GetRichOutgoingSharedNodes(ctx context.Context, sharerID int64, limit int, offset int, sortBy string, sortOrder string) ([]*models.RichNode, error) {
	orderByClause := buildOrderByClause(sortBy, sortOrder)

	query := fmt.Sprintf(`
		SELECT %s
		FROM nodes n
		JOIN users u ON n.owner_id = u.id
		LEFT JOIN user_favorites fav ON n.id = fav.node_id AND fav.user_id = $1
		WHERE n.id IN (
			SELECT DISTINCT node_id FROM shares WHERE sharer_id = $1
		) AND n.deleted_at IS NULL
		%s
		LIMIT $2 OFFSET $3
	`, richNodeFields, orderByClause)

	rows, err := q.db.Query(ctx, query, sharerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*models.RichNode
	for rows.Next() {
		node, err := scanRichNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	if nodes == nil {
		return []*models.RichNode{}, nil
	}

	return nodes, nil
}
