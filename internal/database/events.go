package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (s *PostgresStore) LogEvent(ctx context.Context, userID int64, eventType string, payload interface{}) error {
	eventMsg := map[string]interface{}{
		"event_type": eventType,
		"payload":    payload,
	}
	eventBytes, err := json.Marshal(eventMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal event payload: %w", err)
	}

	query := `INSERT INTO event_journal (user_id, event_type, payload) VALUES ($1, $2, $3)`
	_, err = s.pool.Exec(ctx, query, userID, eventType, eventBytes)
	if err != nil {
		return err
	}

	s.wsHub.PublishEvent(userID, eventBytes)

	return nil
}

type Event struct {
	ID        int64           `json:"id"`
	EventType string          `json:"event_type"`
	EventTime time.Time       `json:"event_time"`
	Payload   json.RawMessage `json:"payload"`
}

func (s *PostgresStore) GetEventsSince(ctx context.Context, userID int64, sinceID int64) ([]Event, error) {
	query := `
		SELECT id, event_type, event_time, payload
		FROM event_journal
		WHERE user_id = $1 AND id > $2
		ORDER BY id ASC
		LIMIT 100
	`
	rows, err := s.pool.Query(ctx, query, userID, sinceID)
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
