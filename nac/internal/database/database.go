package database

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type User struct {
	ID       int
	Username string
	Role     string
}

type Device struct {
	ID       int
	Hostname string
	MAC      string
	Status   string
	UserID   int
}

func GetUser(ctx context.Context, conn *pgx.Conn, username string) (User, error) {
	var user User

	err := conn.QueryRow(
		ctx,
		`SELECT u.id, u.username, r.name
		 FROM users u
		 LEFT JOIN roles r ON u.role_id = r.id
		 WHERE u.username = $1`,
		username,
	).Scan(&user.ID, &user.Username, &user.Role)

	return user, err
}

func GetDevice(ctx context.Context, conn *pgx.Conn, deviceID int) (Device, error) {
	var device Device

	err := conn.QueryRow(
		ctx,
		`SELECT id, hostname, mac_address, status, user_id
		 FROM devices
		 WHERE id = $1`,
		deviceID,
	).Scan(
		&device.ID,
		&device.Hostname,
		&device.MAC,
		&device.Status,
		&device.UserID,
	)

	return device, err
}
func GetUserByID(ctx context.Context, conn *pgx.Conn, userID int) (User, error) {
	var user User

	err := conn.QueryRow(ctx,
		`SELECT u.id, u.username, r.name
		 FROM users u
		 LEFT JOIN roles r ON u.role_id = r.id
		 WHERE u.id = $1`,
		userID).
		Scan(&user.ID, &user.Username, &user.Role)

	return user, err
}
