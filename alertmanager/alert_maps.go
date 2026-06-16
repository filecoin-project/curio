package alertmanager

var (
	AlertFuncs      map[AlertName]AlertFunc
	PingHealthFuncs map[AlertName]AlertFunc
)

func registerAlertMaps() {
	AlertFuncs = buildAlertFuncs()
	PingHealthFuncs = buildPingHealthFuncs()
}
