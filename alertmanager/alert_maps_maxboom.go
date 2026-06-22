//go:build maxboom

package alertmanager

func buildAlertFuncs() map[AlertName]AlertFunc {
	return map[AlertName]AlertFunc{
		Name_BalanceCheck:          balanceCheck,
		Name_TaskFailures:          taskFailureCheck,
		Name_PDPTaskFailures:       pdpTaskFailureCheck,
		Name_PermanentStorageSpace: permanentStorageCheck,
		Name_NowCheck:              NowCheck,
		Name_ChainSync:             chainSyncCheck,
		Name_PendingMessages:       pendingMessagesCheck,
		Name_IPNISync:              ipniSyncCheck,
		Name_PDPKeyConfigured:      pdpKeyConfiguredCheck,
	}
}

func buildPingHealthFuncs() map[AlertName]AlertFunc {
	af := buildAlertFuncs()
	return map[AlertName]AlertFunc{
		Name_BalanceCheck:          af[Name_BalanceCheck],
		Name_ChainSync:             af[Name_ChainSync],
		Name_PermanentStorageSpace: af[Name_PermanentStorageSpace],
		Name_PDPTaskFailures:       af[Name_PDPTaskFailures],
		Name_IPNISync:              af[Name_IPNISync],
		Name_PDPKeyConfigured:      af[Name_PDPKeyConfigured],
	}
}
