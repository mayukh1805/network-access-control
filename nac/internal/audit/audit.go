package audit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

type Event struct {
	UserID    *int
	DeviceID  *int
	SessionID *int
	EventType string
	Resource  *string
	Action    *string
	Decision  *string
	SourceIP  *string
	Details   map[string]any
}

func Log(ctx context.Context, conn *pgx.Conn, event Event) error {
	var detailsJSON []byte
	var err error

	if event.Details != nil {
		detailsJSON, err = json.Marshal(event.Details)
		if err != nil {
			return err
		}
	}

	_, err = conn.Exec(
		ctx,
		`INSERT INTO audit_events
			(user_id, device_id, session_id, event_type,
			 resource, action, decision, source_ip, details)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		event.UserID,
		event.DeviceID,
		event.SessionID,
		event.EventType,
		event.Resource,
		event.Action,
		event.Decision,
		event.SourceIP,
		detailsJSON,
	)

	return err
}
