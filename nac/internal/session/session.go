package session

import (
	"context"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
)

type Session struct {
	ID        int
	UserID    int
	DeviceID  int
	IP        netip.Addr
	Status    string
	ExpiresAt time.Time
}

func GetActiveSession(ctx context.Context, conn *pgx.Conn, userID int) (Session, error) {
	var s Session

	err := conn.QueryRow(
		ctx,
		`SELECT id, user_id, device_id, ip_address, status, expires_at
		 FROM sessions
		 WHERE user_id = $1
		   AND status = 'active'
		   AND expires_at > CURRENT_TIMESTAMP
		 ORDER BY id DESC
		 LIMIT 1`,
		userID,
	).Scan(
		&s.ID,
		&s.UserID,
		&s.DeviceID,
		&s.IP,
		&s.Status,
		&s.ExpiresAt,
	)

	return s, err
}
