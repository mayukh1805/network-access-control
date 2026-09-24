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
func CreateSession(
	ctx context.Context,
	conn *pgx.Conn,
	userID int,
	deviceID int,
	ip netip.Addr,
	token string,
	expiresAt time.Time,
) (Session, error) {
	var s Session

	err := conn.QueryRow(
		ctx,
		`INSERT INTO sessions
            (user_id, device_id, session_token, ip_address, expires_at, status)
         VALUES ($1, $2, $3, $4, $5, 'active')
         RETURNING id, user_id, device_id, ip_address, status, expires_at`,
		userID,
		deviceID,
		token,
		ip,
		expiresAt,
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
func GetSessionByToken(ctx context.Context, conn *pgx.Conn, token string) (Session, error) {
	var s Session

	err := conn.QueryRow(ctx,
		`SELECT id, user_id, device_id, ip_address, status, expires_at
		 FROM sessions
		 WHERE session_token = $1
		   AND status = 'active'
		   AND expires_at > CURRENT_TIMESTAMP`,
		token).
		Scan(
			&s.ID,
			&s.UserID,
			&s.DeviceID,
			&s.IP,
			&s.Status,
			&s.ExpiresAt,
		)

	return s, err
}
