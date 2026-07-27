//go:build skiff

package alertmanager

// buildAlertFuncs returns the Curio-PDP alert set: common alerts only
// (no PoRep window/winning post or missing-sector checks).
func buildAlertFuncs() map[AlertName]AlertFunc {
	return commonAlertFuncs()
}
