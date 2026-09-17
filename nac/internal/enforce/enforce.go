package enforce

import (
	"fmt"
	"os/exec"
)

func AllowPrinter(ip string) error {
	cmd := exec.Command(
		"sudo",
		"ip",
		"netns",
		"exec",
		"gw",
		"nft",
		"add",
		"element",
		"inet",
		"nac",
		"printer_allowed",
		"{",
		ip,
		"}",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to allow printer access: %s: %w", output, err)
	}

	return nil
}

func RevokePrinter(ip string) error {
	cmd := exec.Command(
		"sudo",
		"ip",
		"netns",
		"exec",
		"gw",
		"nft",
		"delete",
		"element",
		"inet",
		"nac",
		"printer_allowed",
		"{", ip,
		"}",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to revoke printer access: %s: %w", output, err)
	}

	return nil
}
