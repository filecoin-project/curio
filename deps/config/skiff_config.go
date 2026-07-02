package config

// SkiffConfig is the subset of CurioConfig exposed in the Skiff configuration editor.
type SkiffConfig struct {
	Subsystems SkiffSubsystemsConfig
	HTTP       HTTPConfig
	Apis       ApisConfig
	Alerting   CurioAlertingConfig
}

// SkiffSubsystemsConfig holds subsystem settings relevant to Skiff / PDP nodes.
type SkiffSubsystemsConfig struct {
	// EnableWebGui enables the web GUI on this node. (Default: true for skiff)
	EnableWebGui bool

	// The address that should listen for Web GUI requests. It should be in form "x.x.x.x:1234"
	GuiAddress string

	// Enable handling for PDP deals and proving on this node.
	EnablePDP bool

	// DataPath is the root directory scanned for writable storage locations.
	// When empty, SKIFF_DATA or the repo path is used.
	DataPath string
}

// DefaultSkiffUIConfig returns defaults for the Skiff config editor (no generated secrets).
func DefaultSkiffUIConfig() *SkiffConfig {
	curio := DefaultCurioConfig()
	return &SkiffConfig{
		Subsystems: SkiffSubsystemsConfig{
			EnablePDP:    true,
			EnableWebGui: true,
			GuiAddress:   "127.0.0.1:4701",
		},
		HTTP:     curio.HTTP,
		Apis:     curio.Apis,
		Alerting: curio.Alerting,
	}
}

// SkiffConfigFromCurio projects a CurioConfig onto the Skiff editor schema.
func SkiffConfigFromCurio(c *CurioConfig) *SkiffConfig {
	if c == nil {
		return DefaultSkiffUIConfig()
	}
	return &SkiffConfig{
		Subsystems: SkiffSubsystemsConfig{
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

// ApplySkiffConfigToCurio copies Skiff editor fields onto a CurioConfig.
func ApplySkiffConfigToCurio(dst *CurioConfig, src *SkiffConfig) {
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
