//go:build !skiff

package alertmanager

// buildAlertFuncs returns the full Curio alert set: common alerts plus
// PoRep-specific checks (window post, winning post, missing sectors).
func buildAlertFuncs() map[AlertName]AlertFunc {
	m := commonAlertFuncs()
	m[Name_WindowPost] = wdPostCheck
	m[Name_WinningPost] = wnPostCheck
	m[Name_MissingSectors] = missingSectorCheck
	return m
}
