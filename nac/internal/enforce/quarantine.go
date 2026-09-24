package enforce

import (
	"fmt"
	"os/exec"
	"strings"
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
	if err == nil {
		return nil
	}

	// If the quarantine set or element does not exist, there is
	// nothing to remove. Treat that as a successful no-op.
	if strings.Contains(string(output), "No such file or directory") {
		fmt.Printf("Quarantine already absent for %s\n", ip)
		return nil
	}

	return fmt.Errorf(
		"failed to remove quarantine for %s: %s: %w",
		ip,
		output,
		err,
	)
}
