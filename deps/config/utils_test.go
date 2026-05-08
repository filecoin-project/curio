package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestForEachConfigDecodesAddressesAndNestedConfig(t *testing.T) {
	type minimalInfo struct {
		Addresses []CurioAddresses `toml:"Addresses"`

		Apis struct {
			ChainApiInfo []string
		}

		Batching struct {
			PreCommit struct {
				Timeout time.Duration
			}
		}

		Market struct {
			StorageMarketConfig struct {
				IPNI struct {
					ServiceURL []string
				}
			}
		}
	}

	got := map[string]minimalInfo{}

	err := forEachConfig[minimalInfo](map[string]string{
		"base": `
[[Addresses]]
MinerAddresses = ["t01002"]
PreCommitControl = ["t3precommit"]
CommitControl = ["t3commit"]
TerminateControl = ["t3terminate"]
  [Addresses.BalanceManager]
    [Addresses.BalanceManager.MK12Collateral]
    CollateralLowThreshold = "100 FIL"

[[Addresses]]
MinerAddresses = ["t01007"]
  [Addresses.BalanceManager]
    [Addresses.BalanceManager.MK12Collateral]
    CollateralLowThreshold = "50 fil"

[Apis]
ChainApiInfo = ["chain-a", "chain-b"]

[Batching]
  [Batching.PreCommit]
  Timeout = "1h30m0s"

[Market]
  [Market.StorageMarketConfig]
    [Market.StorageMarketConfig.IPNI]
    ServiceURL = ["https://cid.contact", "https://filecoinpin.contact"]
`,
		"no-addresses": `
[Apis]
ChainApiInfo = ["chain-c"]
`,
	}, func(name string, info minimalInfo) error {
		got[name] = info
		return nil
	})
	require.NoError(t, err)

	base := got["base"]
	require.Len(t, base.Addresses, 2)

	require.Equal(t, []string{"t01002"}, base.Addresses[0].MinerAddresses)
	require.Equal(t, []string{"t3precommit"}, base.Addresses[0].PreCommitControl)
	require.Equal(t, []string{"t3commit"}, base.Addresses[0].CommitControl)
	require.Equal(t, []string{"t3terminate"}, base.Addresses[0].TerminateControl)
	require.Equal(t, "100 FIL", base.Addresses[0].BalanceManager.MK12Collateral.CollateralLowThreshold.String())
	require.Equal(t, "20 FIL", base.Addresses[0].BalanceManager.MK12Collateral.CollateralHighThreshold.String())

	require.Equal(t, []string{"t01007"}, base.Addresses[1].MinerAddresses)
	require.Equal(t, "50 FIL", base.Addresses[1].BalanceManager.MK12Collateral.CollateralLowThreshold.String())
	require.Equal(t, "20 FIL", base.Addresses[1].BalanceManager.MK12Collateral.CollateralHighThreshold.String())

	require.Equal(t, []string{"chain-a", "chain-b"}, base.Apis.ChainApiInfo)
	require.Equal(t, 90*time.Minute, base.Batching.PreCommit.Timeout)
	require.Equal(t, []string{"https://cid.contact", "https://filecoinpin.contact"}, base.Market.StorageMarketConfig.IPNI.ServiceURL)

	noAddresses := got["no-addresses"]
	require.Empty(t, noAddresses.Addresses)
	require.Equal(t, []string{"chain-c"}, noAddresses.Apis.ChainApiInfo)
}
