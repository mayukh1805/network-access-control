package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"nac/internal/database"
	"nac/internal/enforce"
	"nac/internal/policy"
	"nac/internal/posture"
	"nac/internal/reconcile"
	"nac/internal/session"
)

func main() {
	ctx := context.Background()

	dbURL := os.Getenv("NAC_DATABASE_URL")
	if dbURL == "" {
		fmt.Println("NAC_DATABASE_URL is not set")
		os.Exit(1)
	}

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		fmt.Println("Database connection failed:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	user, err := database.GetUser(ctx, conn, "avi")
	if err != nil {
		fmt.Println("User lookup failed:", err)
		os.Exit(1)
	}

	activeSession, err := session.GetActiveSession(ctx, conn, user.ID)
	if err != nil {
		if err == pgx.ErrNoRows {
			fmt.Println("No active session: revoking printer access")
			if err := enforce.RevokePrinter("10.77.10.100"); err != nil {
				fmt.Println("Revocation failed:", err)
				os.Exit(1)
			}
			return
		}

		fmt.Println("Session lookup failed:", err)
		os.Exit(1)
	}

	ip := activeSession.IP.String()

	device, err := database.GetDevice(ctx, conn, activeSession.DeviceID)
	if err != nil {
		fmt.Println("Device lookup failed:", err)
		os.Exit(1)
	}

	devicePosture := posture.CheckDeviceStatus(device.Status)

	fmt.Printf(
		"Device=%s MAC=%s Status=%s Posture=%s\n",
		device.Hostname,
		device.MAC,
		device.Status,
		devicePosture,
	)

	if devicePosture == posture.Unhealthy {
		fmt.Println("Unhealthy device: activating quarantine")

		if err := enforce.Quarantine(ip); err != nil {
			fmt.Println("Quarantine failed:", err)
			os.Exit(1)
		}

		if err := enforce.RevokePrinter(ip); err != nil {
			fmt.Println("Printer revocation failed:", err)
			os.Exit(1)
		}

		fmt.Println("Device quarantined:", ip)
		return
	}

	if err := enforce.RemoveQuarantine(ip); err != nil {
		fmt.Println("Quarantine removal failed:", err)
		os.Exit(1)
	}

	fmt.Printf(
		"Active session: ID=%d IP=%s Expires=%s\n",
		activeSession.ID,
		ip,
		activeSession.ExpiresAt.Format("2006-01-02 15:04:05"),
	)

	resource := "printer"
	action := "print"

	decision := policy.Evaluate(user.Role, resource, action)
	shouldAllow := decision == policy.Allow

	if err := reconcile.EnsurePrinterAccess(ip, shouldAllow); err != nil {
		fmt.Println("Reconciliation failed:", err)
		os.Exit(1)
	}

	fmt.Printf(
		"User=%s Role=%s Resource=%s Action=%s Decision=%s\n",
		user.Username,
		user.Role,
		resource,
		action,
		decision,
	)
}
