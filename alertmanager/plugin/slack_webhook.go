package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
)

type SlackWebhook struct {
	cfg config.SlackWebhookConfig
}

func NewSlackWebhook(cfg config.SlackWebhookConfig) Plugin {
	return &SlackWebhook{
		cfg: cfg,
	}
}

// SendAlert sends an alert to SlackWebHook with the provided payload data.
// It creates a payload struct with the provided data.
// It creates an HTTP POST request with the SlackWebHook  URL as the endpoint and the marshaled JSON data as the request body.
// It sends the request using an HTTP client with a maximum of 5 retries for network errors with exponential backoff before each retry.
// It handles different HTTP response status codes and returns an error based on the status code().
// If all retries fail, it returns an error indicating the last network error encountered.
func (s *SlackWebhook) SendAlert(data *AlertPayload) error {

	type TextBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}

	type Block struct {
		Type string     `json:"type"`
		Text *TextBlock `json:"text,omitempty"`
	}

	type Payload struct {
		Blocks []Block `json:"blocks"`
	}

	// Initialize the payload with the alert and first divider
	payload := Payload{
		Blocks: []Block{
			{
				Type: "section",
				Text: &TextBlock{
					Type: "mrkdwn",
					Text: ":alert: " + data.Summary,
				},
			},
			{
				Type: "divider",
			},
		},
	}

	// Iterate through the map to construct the remaining blocks
	for key, value := range data.Details {
		payload.Blocks = append(payload.Blocks,
			Block{
				Type: "header",
				Text: &TextBlock{
					Type: "plain_text",
					Text: key,
				},
			},
			Block{
				Type: "section",
				Text: &TextBlock{
					Type: "plain_text",
					Text: fmt.Sprintf("%v", value),
				},
			},
			Block{
				Type: "divider",
			},
		)
	}

	// Marshal the payload to JSON
	jsonData, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return xerrors.Errorf("Error marshaling JSON: %w", err)
	}

	req, err := http.NewRequest("POST", s.cfg.WebHookURL, bytes.NewBuffer(jsonData))
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
				log.Debug("Accepted: The event has been accepted by Slack Webhook.")
				return nil
			case 400:
				bd, rerr := io.ReadAll(resp.Body)
				if rerr != nil {
					return xerrors.Errorf("Bad request: invalid payload. Failed to read the body: %w", rerr)
				}
				switch string(bd) {
				case "invalid_payload":
					return xerrors.Errorf("Bad request: the data sent in your request cannot be understood as presented; verify your content body matches your content type and is structurally valid.")
				case "user_not_found":
					return xerrors.Errorf("Bad request: the user used in your request does not actually exist.")
				default:
					return xerrors.Errorf("Bad request: payload JSON is invalid %s", string(bd))
				}
			case 403:
				bd, rerr := io.ReadAll(resp.Body)
				if rerr != nil {
					return xerrors.Errorf("Bad request: invalid payload. Failed to read the body: %w", rerr)
				}
				switch string(bd) {
				case "action_prohibited":
					return xerrors.Errorf("Forbidden: the team associated with your request has some kind of restriction on the webhook posting in this context.")
				default:
					return xerrors.Errorf("Unexpected 403 error: %s", string(bd))
				}
			case 404:
				return xerrors.Errorf("Not Found: the channel associated with your request does not exist.")
			case 410:
				return xerrors.Errorf("Gone: the channel has been archived and doesn't accept further messages, even from your incoming webhook.")
			case 500:
				bd, rerr := io.ReadAll(resp.Body)
				if rerr != nil {
					return xerrors.Errorf("Bad request: invalid payload. Failed to read the body: %w", rerr)
				}
				switch string(bd) {
				case "rollup_error":
					return xerrors.Errorf("Server error: something strange and unusual happened that was likely not your fault at all.")
				default:
					return xerrors.Errorf("Unexpected 500 error: %s", string(bd))
				}
			default:
				log.Errorw("Response status:", resp.Status)
				return xerrors.Errorf("Unexpected HTTP response: %s", resp.Status)
			}
		})
	if err != nil {
		return fmt.Errorf("after %d retries,last error: %w", iter, err)
	}
	return nil
}
