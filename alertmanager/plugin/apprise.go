package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
)

type Apprise struct {
	cfg config.AppriseConfig
}

func NewApprise(cfg config.AppriseConfig) Plugin {
	return &Apprise{cfg: cfg}
}

// appriseNotifyType maps a curio alert severity onto one of Apprise's notification types.
func appriseNotifyType(severity string) string {
	switch strings.ToLower(severity) {
	case "critical", "error":
		return "failure"
	case "warning":
		return "warning"
	case "info":
		return "info"
	default:
		return "warning"
	}
}

// SendAlert posts to an Apprise API server (https://github.com/caronc/apprise-api), either its
// stateless /notify endpoint (NotifyURLs required) or a stateful /notify/<key> endpoint (NotifyURLs
// left empty).
func (p *Apprise) SendAlert(data *AlertPayload) error {
	if len(data.Details) == 0 {
		return nil
	}

	var body strings.Builder
	for key, value := range data.Details {
		fmt.Fprintf(&body, "**%s**\n%v\n\n", key, value)
	}

	payload := map[string]any{
		"title":  data.Summary,
		"body":   strings.TrimSpace(body.String()),
		"type":   appriseNotifyType(data.Severity),
		"format": "markdown",
	}
	if len(p.cfg.NotifyURLs) > 0 {
		payload["urls"] = strings.Join(p.cfg.NotifyURLs, ",")
	}
	if p.cfg.Tag != "" {
		payload["tag"] = p.cfg.Tag
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return xerrors.Errorf("error marshaling JSON: %w", err)
	}

	req, err := http.NewRequest("POST", p.cfg.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: time.Second * 15,
	}
	iter, _, err := lo.AttemptWithDelay(5, time.Second,
		func(index int, duration time.Duration) error {
			resp, err := client.Do(req)
			if err != nil {
				time.Sleep(time.Duration(2*index) * duration) // Exponential backoff
				return err
			}
			defer func() { _ = resp.Body.Close() }()

			switch resp.StatusCode {
			case http.StatusOK:
				return nil
			case http.StatusNoContent:
				// 204 means the config key has no persisted URLs - a config error, not success.
				time.Sleep(time.Duration(2*index) * duration) // Exponential backoff
				return xerrors.Errorf("no Apprise configuration found for the given key (APIURL / config key is likely wrong)")
			default:
				errBody, rerr := io.ReadAll(resp.Body)
				if rerr != nil {
					time.Sleep(time.Duration(2*index) * duration) // Exponential backoff
					return rerr
				}
				time.Sleep(time.Duration(2*index) * duration) // Exponential backoff
				return fmt.Errorf("unexpected HTTP response: %s: %s", resp.Status, string(errBody))
			}
		})
	if err != nil {
		return fmt.Errorf("after %d retries, last error: %w", iter, err)
	}
	return nil
}
