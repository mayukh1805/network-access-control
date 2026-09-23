package database

import (
	"context"
	"net/netip"

	"github.com/jackc/pgx/v5"
)

type Binding struct {
	ID        int
	DeviceID  int
	IP        netip.Addr
	Status    string
	ExpiresAt *string
}

func GetActiveBinding(ctx context.Context, conn *pgx.Conn, deviceID int, ip netip.Addr) (Binding, error) {
	var binding Binding

	err := conn.QueryRow(
		ctx,
		`SELECT id, device_id, ip_address, status, expires_at::text
		 FROM bindings
		 WHERE device_id = $1
		   AND ip_address = $2
		   AND status = 'active'
		   AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		 ORDER BY id DESC
		 LIMIT 1`,
		deviceID,
		ip,
	).Scan(
		&binding.ID,
		&binding.DeviceID,
		&binding.IP,
		&binding.Status,
		&binding.ExpiresAt,
	)

	return binding, err
}
