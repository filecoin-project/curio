package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/samber/lo"
)

type PagerDuty struct {
	cfg config.PagerDutyConfig
}

func NewPagerDuty(cfg config.PagerDutyConfig) Plugin {
	return &PagerDuty{
		cfg: cfg,
	}
}

// SendAlert sends an alert to PagerDuty with the provided payload data.
// It creates a PDData struct with the provided routing key, event action and payload.
// It creates an HTTP POST request with the PagerDuty event URL as the endpoint and the marshaled JSON data as the request body.
// It sends the request using an HTTP client with a maximum of 5 retries for network errors with exponential backoff before each retry.
// It handles different HTTP response status codes and returns an error based on the status code().
// If all retries fail, it returns an error indicating the last network error encountered.
func (p *PagerDuty) SendAlert(data *AlertPayload) error {

	type pdPayload struct {
		Summary       string      `json:"summary"`
		Severity      string      `json:"severity"`
		Source        string      `json:"source"`
		Component     string      `json:"component,omitempty"`
		Group         string      `json:"group,omitempty"`
		Class         string      `json:"class,omitempty"`
		CustomDetails interface{} `json:"custom_details,omitempty"`
	}

	type pdData struct {
		RoutingKey  string     `json:"routing_key"`
		EventAction string     `json:"event_action"`
		Payload     *pdPayload `json:"payload"`
	}

	payload := &pdData{
		RoutingKey:  p.cfg.PageDutyIntegrationKey,
		EventAction: "trigger",
		Payload: &pdPayload{
			Summary:       data.Summary,
			Severity:      data.Severity,
			Source:        data.Source,
			CustomDetails: data.Details,
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	req, err := http.NewRequest("POST", p.cfg.PagerDutyEventURL, bytes.NewBuffer(jsonData))
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
			case 202:
				log.Debug("Accepted: The event has been accepted by PagerDuty.")
				return nil
			case 400:
				bd, rerr := io.ReadAll(resp.Body)
				if rerr != nil {
					return xerrors.Errorf("Bad request: payload JSON is invalid. Failed to read the body: %w", err)
				}
				return xerrors.Errorf("Bad request: payload JSON is invalid %s", string(bd))
			case 429:
				log.Debug("Too many API calls, retrying after backoff...")
				time.Sleep(time.Duration(5*index) * time.Second) // Exponential backoff
			case 500, 501, 502, 503, 504:
				log.Debug("Server error, retrying after backoff...")
				time.Sleep(time.Duration(5*index) * time.Second) // Exponential backoff
			default:
				log.Errorw("Response status:", resp.Status)
				return xerrors.Errorf("Unexpected HTTP response: %s", resp.Status)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("after %d retries,last error: %w", iter, err)
	}
	return nil
}
