package plugins

import (
	"time"

	"github.com/filecoin-project/curio/deps/config"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("curio/alertplugins")

type AlertPayload struct {
	Severity string
	Summary  string
	Source   string
	Details  map[string]string
	Time     time.Time
}

type Plugin interface {
	SendAlert(data *AlertPayload) error
}

func LoadAlertPlugins(cfg config.CurioAlertingConfig) []Plugin {
	var plugins []Plugin
	if cfg.PagerDuty.Enable {
		plugins = append(plugins, &PagerDuty{cfg: cfg.PagerDuty})
	}
	if cfg.LarkBot.Enable {
		plugins = append(plugins, &LarkCustomBot{cfg: cfg.LarkBot})
	}
	return plugins
}
