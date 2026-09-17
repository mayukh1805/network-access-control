package reconcile

import (
	"fmt"
	"os/exec"
	"strings"

	"nac/internal/enforce"
)

func PrinterAccessExists(ip string) (bool, error) {
	cmd := exec.Command(
		"sudo", "ip", "netns", "exec", "gw",
		"nft", "list", "set", "inet", "nac", "printer_allowed",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to inspect printer access: %s: %w", output, err)
	}

	return strings.Contains(string(output), ip), nil
}

func EnsurePrinterAccess(ip string, shouldAllow bool) error {
	exists, err := PrinterAccessExists(ip)
	if err != nil {
		return err
	}

	if shouldAllow && !exists {
		fmt.Println("Reconciliation: restoring printer access for", ip)
		return enforce.AllowPrinter(ip)
	}

	if !shouldAllow && exists {
		fmt.Println("Reconciliation: removing unauthorized printer access for", ip)
		return enforce.RevokePrinter(ip)
	}

	fmt.Println("Reconciliation: firewall state matches desired state")
	return nil
}
