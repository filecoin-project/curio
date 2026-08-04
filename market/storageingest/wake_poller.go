package storageingest

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// WakeAllDealPollers queries harmony_machines for all active nodes and sends
// a POST to /market/wake-poller on each, triggering an immediate poll cycle.
// Errors on individual nodes are logged but do not fail the call.
func WakeAllDealPollers(ctx context.Context, db *harmonydb.DB) {
	var hosts []struct {
		HostAndPort string `db:"host_and_port"`
	}
	err := db.Select(ctx, &hosts, `SELECT host_and_port FROM harmony_machines`)
	if err != nil {
		log.Warnf("wake deal pollers: failed to list machines: %s", err)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	for _, h := range hosts {
		url := fmt.Sprintf("http://%s/market/wake-poller", h.HostAndPort)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			log.Warnf("wake deal pollers: bad request for %s: %s", h.HostAndPort, err)
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Debugf("wake deal pollers: %s unreachable: %s", h.HostAndPort, err)
			continue
		}
		err = resp.Body.Close()
		if err != nil {
			log.Warnf("wake deal pollers: failed to close response body for %s: %s", h.HostAndPort, err)
		}
	}
}
