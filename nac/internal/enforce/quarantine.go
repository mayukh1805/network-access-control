package enforce

import (
	"fmt"
	"os/exec"
)

func Quarantine(ip string) error {
	cmd := exec.Command(
		"sudo", "ip", "netns", "exec", "gw",
		"nft", "add", "element", "inet", "nac",
		"quarantine", "{", ip, "}",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to quarantine %s: %s: %w", ip, output, err)
	}

	return nil
}

func RemoveQuarantine(ip string) error {
	cmd := exec.Command(
		"sudo", "ip", "netns", "exec", "gw",
		"nft", "delete", "element", "inet", "nac",
		"quarantine", "{", ip, "}",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Element is already absent, so desired state is already achieved.
		if string(output) != "" {
			fmt.Printf("Quarantine already absent for %s\n", ip)
			return nil
		}

		return fmt.Errorf("failed to remove quarantine for %s: %w", ip, err)
	}

	return nil
}
