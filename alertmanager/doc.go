/*
Package alertmanager provides a framework for monitoring and alerting within the Curio project. It supports dynamic plugin integration for alert notifications, allowing for flexible and extensible alerting mechanisms.

Implementing a New Plugin:

1. Define a struct that implements the Plugin interface, which includes the SendAlert method for dispatching alerts.
2. Implement the SendAlert method to handle the alert logic specific to your plugin.
3. Provide a constructor function for your plugin to facilitate its configuration and initialization.
4. Register your plugin in the LoadAlertPlugins function, which dynamically loads plugins based on the CurioAlertingConfig.

Plugin Configuration:

Plugins are configured through the config.CurioAlertingConfig struct. Each plugin can have its own configuration section within this struct, enabling or disabling the plugin and setting plugin-specific parameters.

Example:

```go
type MyPlugin struct{}

	func (p *MyPlugin) SendAlert(data *plugin.AlertPayload) error {
		// Plugin-specific alert sending logic
		return nil
	}

	func NewMyPlugin() *MyPlugin {
		return &MyPlugin{}
	}

	func LoadAlertPlugins(cfg config.CurioAlertingConfig) []plugin.Plugin {
		var plugins []plugin.Plugin
		if cfg.MyPlugin.Enabled {
			plugins = append(plugins, NewMyPlugin())
		}
		return plugins
	}

```
This package leverages the CurioAlertingConfig for plugin configuration,
enabling a modular approach to adding or removing alerting capabilities as required.
*/
package alertmanager
