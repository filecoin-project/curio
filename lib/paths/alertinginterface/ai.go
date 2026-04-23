package alertinginterface

type AlertingInterface interface {
	AddAlertType(name, id string) AlertType
	Raise(alert AlertType, metadata map[string]any)
	IsRaised(alert AlertType) bool
	Resolve(alert AlertType, metadata map[string]string)
}
type AlertType struct {
	System, Subsystem string
}
