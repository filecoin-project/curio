package alertmanager

import (
	"os"
	"strings"
)

const unknownAlertSource = "unknown-host"

// AlertSource returns the configured cluster identifier, falling back to the
// hostname of the node executing the alert delivery.
func AlertSource(clusterName string) string {
	if name := strings.TrimSpace(clusterName); name != "" {
		return name
	}

	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return unknownAlertSource
	}
	return hostname
}
