package plugin

import (
	"time"

	"github.com/filecoin-project/curio/deps/config"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("curio/alertplugins")

type Plugin interface {
	SendAlert(data *AlertPayload) error
}

type AlertPayload struct {
	Summary  string
	Severity string
	Source   string
	Details  map[string]interface{}
	Time     time.Time
}

func LoadAlertPlugins(cfg config.CurioAlertingConfig) []Plugin {
	var plugins []Plugin
	if cfg.PagerDuty.Enable {
		plugins = append(plugins, NewPagerDuty(cfg.PagerDuty))
	}
	if cfg.PrometheusAlertManager.Enable {
		plugins = append(plugins, NewPrometheusAlertManager(cfg.PrometheusAlertManager))
	}
	return plugins
}
