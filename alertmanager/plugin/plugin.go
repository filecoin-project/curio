package plugin

import (
	"time"

	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/deps/config"
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

var TestPlugins []Plugin

func LoadAlertPlugins(cfg config.CurioAlertingConfig) *config.Dynamic[[]Plugin] {
	pluginsDynamic := config.NewDynamic([]Plugin{})
	collectPlugins := func() []Plugin {
		var plugins []Plugin
		if cfg.PagerDuty.Enable.Get() {
			plugins = append(plugins, NewPagerDuty(cfg.PagerDuty))
		}
		if cfg.PrometheusAlertManager.Enable.Get() {
			plugins = append(plugins, NewPrometheusAlertManager(cfg.PrometheusAlertManager))
		}
		if cfg.SlackWebhook.Enable.Get() {
			plugins = append(plugins, NewSlackWebhook(cfg.SlackWebhook))
		}
		if len(TestPlugins) > 0 {
			plugins = append(plugins, TestPlugins...)
		}
		return plugins
	}
	pluginsDynamic.Set(collectPlugins())
	cfg.PagerDuty.Enable.OnChange(func() {
		pluginsDynamic.Set(collectPlugins())
	})
	cfg.PrometheusAlertManager.Enable.OnChange(func() {
		pluginsDynamic.Set(collectPlugins())
	})
	cfg.SlackWebhook.Enable.OnChange(func() {
		pluginsDynamic.Set(collectPlugins())
	})

	return pluginsDynamic
}
