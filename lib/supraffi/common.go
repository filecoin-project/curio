package supraffi

import "time"

// HealthInfo represents NVMe device health information in a more Go-friendly format
type HealthInfo struct {
	// Critical warning flags
	CriticalWarning byte

	// Temperature information in Celsius
	Temperature        float64
	TemperatureSensors []float64
	WarningTempTime    time.Duration
	CriticalTempTime   time.Duration

	// Reliability metrics
	AvailableSpare          uint8
	AvailableSpareThreshold uint8
	PercentageUsed          uint8

	// Usage statistics
	DataUnitsRead      uint64 // in 512-byte units
	DataUnitsWritten   uint64 // in 512-byte units
	HostReadCommands   uint64
	HostWriteCommands  uint64
	ControllerBusyTime time.Duration

	// Power and error statistics
	PowerCycles     uint64
	PowerOnHours    time.Duration
	UnsafeShutdowns uint64
	MediaErrors     uint64
	ErrorLogEntries uint64
}

// Helper methods for interpreting critical warning flags
const (
	WarningSpareSpace       = 1 << 0
	WarningTemperature      = 1 << 1
	WarningReliability      = 1 << 2
	WarningReadOnly         = 1 << 3
	WarningVolatileMemory   = 1 << 4
	WarningPersistentMemory = 1 << 5
)

// HasWarning checks if a specific warning flag is set
func (h *HealthInfo) HasWarning(flag byte) bool {
	return (h.CriticalWarning & flag) != 0
}

// GetWarnings returns a slice of active warning descriptions
func (h *HealthInfo) GetWarnings() []string {
	var warnings []string

	if h.HasWarning(WarningSpareSpace) {
		warnings = append(warnings, "available spare space has fallen below threshold")
	}
	if h.HasWarning(WarningTemperature) {
		warnings = append(warnings, "temperature is above critical threshold")
	}
	if h.HasWarning(WarningReliability) {
		warnings = append(warnings, "device reliability has been degraded")
	}
	if h.HasWarning(WarningReadOnly) {
		warnings = append(warnings, "media has been placed in read only mode")
	}
	if h.HasWarning(WarningVolatileMemory) {
		warnings = append(warnings, "volatile memory backup device has failed")
	}
	if h.HasWarning(WarningPersistentMemory) {
		warnings = append(warnings, "persistent memory region has become read-only")
	}

	return warnings
}
