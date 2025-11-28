package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/samber/lo"

	"github.com/filecoin-project/curio/deps/config"
)

type PrometheusAlertManager struct {
	cfg config.PrometheusAlertManagerConfig
}

func NewPrometheusAlertManager(cfg config.PrometheusAlertManagerConfig) Plugin {
	return &PrometheusAlertManager{
		cfg: cfg,
	}
}

// SendAlert sends an alert to Prometheus AlertManager with the provided payload data.
// API reference: https://raw.githubusercontent.com/prometheus/alertmanager/main/api/v2/openapi.yaml
func (p *PrometheusAlertManager) SendAlert(data *AlertPayload) error {
	if len(data.Details) == 0 {
		return nil
	}

	type amPayload struct {
		StartsAt    time.Time              `json:"startsAt"`
		EndsAt      *time.Time             `json:"EndsAt,omitempty"`
		Annotations map[string]interface{} `json:"annotations"`
		Labels      map[string]string      `json:"labels"`

		// is a unique back-link which identifies the causing entity of this alert
		//GeneratorURL string            `json:"generatorURL"`
	}

	var alerts []*amPayload
	for k, v := range data.Details {
		alerts = append(alerts, &amPayload{
			StartsAt: data.Time,
			Labels: map[string]string{
				"alertName": k,
				"severity":  data.Severity,
				"instance":  data.Source,
			},
			Annotations: map[string]interface{}{
				"summary": data.Summary,
				"details": v,
			},
		})
	}

	jsonData, err := json.Marshal(alerts)
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}
	req, err := http.NewRequest("POST", p.cfg.AlertManagerURL.Get(), bytes.NewBuffer(jsonData))
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
			case http.StatusBadRequest, http.StatusInternalServerError:
				errBody, err := io.ReadAll(resp.Body)
				if err != nil {
					time.Sleep(time.Duration(2*index) * duration) // Exponential backoff
					return err
				}
				return fmt.Errorf("error: %s", string(errBody))
			default:
				time.Sleep(time.Duration(2*index) * duration) // Exponential backoff
				return fmt.Errorf("unexpected HTTP response: %s", resp.Status)
			}
		})
	if err != nil {
		return fmt.Errorf("after %d retries,last error: %w", iter, err)
	}
	return nil
}
