package config

// MaxBoomConfig is the subset of CurioConfig exposed in the MaxBoom configuration editor.
type MaxBoomConfig struct {
	Subsystems MaxBoomSubsystemsConfig
	HTTP       HTTPConfig
	Apis       ApisConfig
	Alerting   CurioAlertingConfig
}

// MaxBoomSubsystemsConfig holds subsystem settings relevant to MaxBoom / PDP nodes.
type MaxBoomSubsystemsConfig struct {
	// EnableWebGui enables the web GUI on this node. (Default: true for maxboom)
	EnableWebGui bool

	// The address that should listen for Web GUI requests. It should be in form "x.x.x.x:1234"
	GuiAddress string

	// Enable handling for PDP deals and proving on this node.
	EnablePDP bool

	// DataPath is the root directory scanned for writable storage locations.
	// When empty, MAXBOOM_DATA or the repo path is used.
	DataPath string
}

// DefaultMaxBoomUIConfig returns defaults for the MaxBoom config editor (no generated secrets).
func DefaultMaxBoomUIConfig() *MaxBoomConfig {
	curio := DefaultCurioConfig()
	return &MaxBoomConfig{
		Subsystems: MaxBoomSubsystemsConfig{
			EnablePDP:    true,
			EnableWebGui: true,
			GuiAddress:   "127.0.0.1:4701",
		},
		HTTP:     curio.HTTP,
		Apis:     ApisConfig{ChainBackend: ChainBackendLantern},
		Alerting: curio.Alerting,
	}
}

// MaxBoomConfigFromCurio projects a CurioConfig onto the MaxBoom editor schema.
func MaxBoomConfigFromCurio(c *CurioConfig) *MaxBoomConfig {
	if c == nil {
		return DefaultMaxBoomUIConfig()
	}
	return &MaxBoomConfig{
		Subsystems: MaxBoomSubsystemsConfig{
			EnableWebGui: c.Subsystems.EnableWebGui,
			GuiAddress:   c.Subsystems.GuiAddress,
			EnablePDP:    c.Subsystems.EnablePDP,
			DataPath:     c.Subsystems.DataPath,
		},
		HTTP:     c.HTTP,
		Apis:     c.Apis,
		Alerting: c.Alerting,
	}
}

// ApplyMaxBoomConfigToCurio copies MaxBoom editor fields onto a CurioConfig.
func ApplyMaxBoomConfigToCurio(dst *CurioConfig, src *MaxBoomConfig) {
	if dst == nil || src == nil {
		return
	}
	dst.Subsystems.EnableWebGui = src.Subsystems.EnableWebGui
	dst.Subsystems.GuiAddress = src.Subsystems.GuiAddress
	dst.Subsystems.EnablePDP = src.Subsystems.EnablePDP
	dst.Subsystems.DataPath = src.Subsystems.DataPath
	dst.HTTP = src.HTTP
	dst.Apis = src.Apis
	dst.Alerting = src.Alerting
}
