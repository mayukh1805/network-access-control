package reconcile

import (
	"encoding/json"
	"fmt"
	"os/exec"

	"nac/internal/enforce"
)

type nftSet struct {
	Elem []json.RawMessage `json:"elem"`
}

type nftObject struct {
	Set *nftSet `json:"set"`
}

type nftResponse struct {
	Nftables []nftObject `json:"nftables"`
}

func PrinterAccessExists(ip string) (bool, error) {
	cmd := exec.Command(
		"sudo", "ip", "netns", "exec", "gw",
		"nft", "-j", "list", "set", "inet", "nac", "printer_allowed",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf(
			"failed to inspect printer access: %s: %w",
			output,
			err,
		)
	}

	var response nftResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return false, fmt.Errorf(
			"failed to parse nftables JSON: %w",
			err,
		)
	}

	for _, object := range response.Nftables {
		if object.Set == nil {
			continue
		}

		for _, rawElem := range object.Set.Elem {
			var elem string
			if err := json.Unmarshal(rawElem, &elem); err == nil {
				if elem == ip {
					return true, nil
				}
				continue
			}

			// Handle element objects used by some nftables JSON output.
			var elemObject struct {
				Elem string `json:"elem"`
			}

			if err := json.Unmarshal(rawElem, &elemObject); err == nil {
				if elemObject.Elem == ip {
					return true, nil
				}
			}
		}
	}

	return false, nil
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
