package database

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func ExpireSessions(ctx context.Context, conn *pgx.Conn) ([]int, error) {
	rows, err := conn.Query(
		ctx,
		`UPDATE sessions
		 SET status = 'expired'
		 WHERE status = 'active'
		   AND expires_at <= CURRENT_TIMESTAMP
		 RETURNING id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expiredIDs []int

	for rows.Next() {
		var id int

		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		expiredIDs = append(expiredIDs, id)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return expiredIDs, nil
}
