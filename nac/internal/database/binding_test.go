package database

import (
	"context"
	"net/netip"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestGetActiveBinding(t *testing.T) {
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

	ip := netip.MustParseAddr("10.77.10.100")

	binding, err := GetActiveBinding(ctx, conn, 1, ip)
	if err != nil {
		t.Fatal(err)
	}

	if binding.DeviceID != 1 {
		t.Fatalf("expected device 1, got %d", binding.DeviceID)
	}

	if binding.IP != ip {
		t.Fatalf("expected IP %s, got %s", ip, binding.IP)
	}

	if binding.Status != "active" {
		t.Fatalf("expected active binding, got %s", binding.Status)
	}

	if binding.ExpiresAt == nil {
		t.Fatal("expected binding expiry")
	}

	t.Logf(
		"Active binding: ID=%d Device=%d IP=%s Expires=%s",
		binding.ID,
		binding.DeviceID,
		binding.IP,
		*binding.ExpiresAt,
	)
}

func TestGetActiveBindingRejectsWrongIP(t *testing.T) {
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

	wrongIP := netip.MustParseAddr("10.77.10.101")

	_, err = GetActiveBinding(ctx, conn, 1, wrongIP)
	if err == nil {
		t.Fatal("expected wrong IP binding to be rejected")
	}
}
