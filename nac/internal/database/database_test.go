package database

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestGetUser(t *testing.T) {
	dbURL := os.Getenv("NAC_DATABASE_URL")
	if dbURL == "" {
		t.Fatal("NAC_DATABASE_URL is not set")
	}

	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	user, err := GetUser(ctx, conn, "avi")
	if err != nil {
		t.Fatal(err)
	}

	if user.Username != "avi" {
		t.Fatalf("expected username avi, got %s", user.Username)
	}

	if user.Role != "Employee" {
		t.Fatalf("expected role Employee, got %s", user.Role)
	}
}
