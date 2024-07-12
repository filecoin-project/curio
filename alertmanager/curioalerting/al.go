package curioalerting

import (
	"sync"

	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/lib/paths/alertinginterface"
)

var log = logging.Logger("curio/alerting")

type AlertingSystem struct {
	sync.Mutex
	Current map[alertinginterface.AlertType]map[string]any
}

func NewAlertingSystem() *AlertingSystem {
	return &AlertingSystem{Current: map[alertinginterface.AlertType]map[string]any{}}
}

func (as *AlertingSystem) AddAlertType(name, id string) alertinginterface.AlertType {
	return alertinginterface.AlertType{
		System:    id,
		Subsystem: name,
	}
}

func (as *AlertingSystem) Raise(alert alertinginterface.AlertType, metadata map[string]interface{}) {
	as.Lock()
	defer as.Unlock()
	as.Current[alert] = metadata
	log.Errorw("Alert raised: ", "System", alert.System, "Subsystem", alert.Subsystem, "Metadata", metadata)
}

func (as *AlertingSystem) IsRaised(alert alertinginterface.AlertType) bool {
	as.Lock()
	defer as.Unlock()
	_, ok := as.Current[alert]
	return ok
}

func (as *AlertingSystem) Resolve(alert alertinginterface.AlertType, metadata map[string]string) {
	as.Lock()
	defer as.Unlock()
	delete(as.Current, alert)
}
