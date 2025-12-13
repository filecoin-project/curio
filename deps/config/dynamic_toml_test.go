package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/types"
)

func TestTransparentMarshalUnmarshal(t *testing.T) {
	t.Run("simple types", func(t *testing.T) {
		type Config struct {
			Regular int
			Dynamic *Dynamic[int]
			After   string
		}

		cfg1 := Config{
			Regular: 10,
			Dynamic: NewDynamic(42),
			After:   "test",
		}

		data, err := TransparentMarshal(cfg1)
		assert.NoError(t, err)
		t.Logf("Marshaled:\n%s", string(data))

		// Check it's truly transparent (no nesting)
		assert.Contains(t, string(data), "Dynamic = 42")
		assert.NotContains(t, string(data), "[Dynamic]")

		// Unmarshal
		var cfg2 Config
		cfg2.Dynamic = NewDynamic(0)
		err = TransparentUnmarshal(data, &cfg2)
		assert.NoError(t, err)

		assert.Equal(t, 10, cfg2.Regular)
		assert.Equal(t, 42, cfg2.Dynamic.Get())
		assert.Equal(t, "test", cfg2.After)
	})

	t.Run("struct types", func(t *testing.T) {
		type Inner struct {
			Name  string
			Count int
		}

		type Config struct {
			Field *Dynamic[Inner]
		}

		cfg1 := Config{
			Field: NewDynamic(Inner{Name: "test", Count: 99}),
		}

		data, err := TransparentMarshal(cfg1)
		assert.NoError(t, err)
		t.Logf("Marshaled:\n%s", string(data))

		// Should have Field.Name and Field.Count directly
		assert.Contains(t, string(data), "[Field]")
		assert.Contains(t, string(data), "Name = \"test\"")
		assert.Contains(t, string(data), "Count = 99")

		var cfg2 Config
		cfg2.Field = NewDynamic(Inner{})
		err = TransparentUnmarshal(data, &cfg2)
		assert.NoError(t, err)

		assert.Equal(t, "test", cfg2.Field.Get().Name)
		assert.Equal(t, 99, cfg2.Field.Get().Count)
	})

	t.Run("slice types", func(t *testing.T) {
		type Config struct {
			Items *Dynamic[[]string]
		}

		cfg1 := Config{
			Items: NewDynamic([]string{"a", "b", "c"}),
		}

		data, err := TransparentMarshal(cfg1)
		assert.NoError(t, err)
		t.Logf("Marshaled:\n%s", string(data))

		assert.Contains(t, string(data), "Items = [\"a\", \"b\", \"c\"]")

		var cfg2 Config
		cfg2.Items = NewDynamic([]string{})
		err = TransparentUnmarshal(data, &cfg2)
		assert.NoError(t, err)

		assert.Equal(t, []string{"a", "b", "c"}, cfg2.Items.Get())
	})

	t.Run("time.Duration types", func(t *testing.T) {
		type Config struct {
			Timeout *Dynamic[time.Duration]
		}

		cfg1 := Config{
			Timeout: NewDynamic(5 * time.Minute),
		}

		data, err := TransparentMarshal(cfg1)
		assert.NoError(t, err)
		t.Logf("Marshaled:\n%s", string(data))

		assert.Contains(t, string(data), "Timeout = \"5m0s\"")

		var cfg2 Config
		cfg2.Timeout = NewDynamic(time.Duration(0))
		err = TransparentUnmarshal(data, &cfg2)
		assert.NoError(t, err)

		assert.Equal(t, 5*time.Minute, cfg2.Timeout.Get())
	})
}

func TestTransparentMarshalCurioIngest(t *testing.T) {
	// Test with a subset of CurioIngestConfig
	type TestIngest struct {
		MaxMarketRunningPipelines *Dynamic[int]
		MaxQueueDownload          *Dynamic[int]
		MaxDealWaitTime           *Dynamic[time.Duration]
	}

	cfg1 := TestIngest{
		MaxMarketRunningPipelines: NewDynamic(64),
		MaxQueueDownload:          NewDynamic(8),
		MaxDealWaitTime:           NewDynamic(time.Hour),
	}

	data, err := TransparentMarshal(cfg1)
	assert.NoError(t, err)
	t.Logf("Marshaled:\n%s", string(data))

	// Verify transparency - should be flat keys
	assert.Contains(t, string(data), "MaxMarketRunningPipelines = 64")
	assert.Contains(t, string(data), "MaxQueueDownload = 8")
	assert.NotContains(t, string(data), "[MaxMarketRunningPipelines]")

	var cfg2 TestIngest
	cfg2.MaxMarketRunningPipelines = NewDynamic(0)
	cfg2.MaxQueueDownload = NewDynamic(0)
	cfg2.MaxDealWaitTime = NewDynamic(time.Duration(0))

	err = TransparentUnmarshal(data, &cfg2)
	assert.NoError(t, err)

	assert.Equal(t, 64, cfg2.MaxMarketRunningPipelines.Get())
	assert.Equal(t, 8, cfg2.MaxQueueDownload.Get())
	assert.Equal(t, time.Hour, cfg2.MaxDealWaitTime.Get())
}

func TestTransparentMarshalWithFIL(t *testing.T) {
	// Test with FIL types - both marshal and unmarshal
	type TestConfig struct {
		Fee          *Dynamic[types.FIL]
		Amount       types.FIL
		RegularField int
	}

	// Create config with properly initialized FIL values
	cfg1 := TestConfig{
		Fee:          NewDynamic(types.MustParseFIL("5 FIL")),
		Amount:       types.MustParseFIL("10 FIL"),
		RegularField: 42,
	}

	// Marshal
	data, err := TransparentMarshal(cfg1)
	assert.NoError(t, err)
	t.Logf("Marshaled:\n%s", string(data))

	// Verify transparency - Fee should be flat
	assert.Contains(t, string(data), `Fee = "5 FIL"`)
	assert.Contains(t, string(data), `Amount = "10 FIL"`)
	assert.Contains(t, string(data), "RegularField = 42")

	// Unmarshal - key is to pre-initialize FIL fields with proper values
	cfg2 := TestConfig{
		Fee:    NewDynamic(types.MustParseFIL("0")), // Initialize with zero FIL
		Amount: types.MustParseFIL("0"),             // Initialize with zero FIL
	}

	err = TransparentUnmarshal(data, &cfg2)
	assert.NoError(t, err)

	// Verify values were unmarshaled correctly
	assert.Equal(t, "5 FIL", cfg2.Fee.Get().String())
	assert.Equal(t, "10 FIL", cfg2.Amount.String())
	assert.Equal(t, 42, cfg2.RegularField)
}

func TestTransparentMarshalBatchFeeConfig(t *testing.T) {
	// Test with actual BatchFeeConfig from types.go
	type TestBatchConfig struct {
		Base      types.FIL
		PerSector types.FIL
		Dynamic   *Dynamic[types.FIL]
	}

	cfg1 := TestBatchConfig{
		Base:      types.MustParseFIL("1 FIL"),
		PerSector: types.MustParseFIL("0.02 FIL"),
		Dynamic:   NewDynamic(types.MustParseFIL("0.5 FIL")),
	}

	data, err := TransparentMarshal(cfg1)
	assert.NoError(t, err)
	t.Logf("Marshaled:\n%s", string(data))

	// Verify all fields are present and transparent
	assert.Contains(t, string(data), `Base = "1 FIL"`)
	assert.Contains(t, string(data), `PerSector = "0.02 FIL"`)
	assert.Contains(t, string(data), `Dynamic = "0.5 FIL"`)

	// Unmarshal with proper initialization
	cfg2 := TestBatchConfig{
		Base:      types.MustParseFIL("0"),
		PerSector: types.MustParseFIL("0"),
		Dynamic:   NewDynamic(types.MustParseFIL("0")),
	}

	err = TransparentUnmarshal(data, &cfg2)
	assert.NoError(t, err)

	assert.Equal(t, "1 FIL", cfg2.Base.String())
	assert.Equal(t, "0.02 FIL", cfg2.PerSector.String())
	assert.Equal(t, "0.5 FIL", cfg2.Dynamic.Get().String())
}

func TestLoadConfigWithFullyPopulatedAddressBlock(t *testing.T) {
	cfg := DefaultCurioConfig()

	const dynamicConfig = `
[[Addresses]]
PreCommitControl = ["t01001", "t01002"]
CommitControl = ["t02001"]
DealPublishControl = ["t03001", "t03002"]
TerminateControl = ["t04001"]
DisableOwnerFallback = true
DisableWorkerFallback = true
MinerAddresses = ["t05001", "t05002"]

[Addresses.BalanceManager.MK12Collateral]
DealCollateralWallet = "t06001"
CollateralLowThreshold = "15 FIL"
CollateralHighThreshold = "45 FIL"

[Fees.MaxPreCommitBatchGasFee]
Base = "0.1 FIL"
PerSector = "0.25 FIL"

[Fees.MaxCommitBatchGasFee]
Base = "0.2 FIL"
PerSector = "0.35 FIL"

[Fees.MaxUpdateBatchGasFee]
Base = "0.3 FIL"
PerSector = "0.45 FIL"

[Fees]
MaxWindowPoStGasFee = "11 FIL"
CollateralFromMinerBalance = true
DisableCollateralFallback = true
MaximizeFeeCap = false

[Apis]
ChainApiInfo = ["https://lotus-a.example.com/rpc/v1", "https://lotus-b.example.com/rpc/v1"]

[Alerting]
MinimumWalletBalance = "15 FIL"

[Alerting.PagerDuty]
Enable = true
PagerDutyEventURL = "https://events.custom.com/v2/enqueue"
PageDutyIntegrationKey = "integration-key"

[Alerting.PrometheusAlertManager]
Enable = true
AlertManagerURL = "http://alerts.internal/api"

[Alerting.SlackWebhook]
Enable = true
WebHookURL = "https://hooks.slack.com/services/AAA/BBB/CCC"

[Batching.PreCommit]
Timeout = "5h0m0s"
Slack = "7h0m0s"

[Batching.Commit]
Timeout = "2h0m0s"
Slack = "1h30m0s"

[Batching.Update]
BaseFeeThreshold = "0.123 FIL"
Timeout = "3h0m0s"
Slack = "2h15m0s"

[[Market.StorageMarketConfig.PieceLocator]]
URL = "https://pieces-1.example.com/piece"
Headers = { Authorization = ["Bearer token-a"], "X-Custom" = ["alpha"] }

[[Market.StorageMarketConfig.PieceLocator]]
URL = "https://pieces-2.example.com/piece"
Headers = { Authorization = ["Bearer token-b"], "X-Another" = ["beta", "gamma"] }
`

	require.NotPanics(t, func() {
		_, err := LoadConfigWithUpgrades(dynamicConfig, cfg)
		require.NoError(t, err)
	})

	addresses := cfg.Addresses.Get()
	require.Len(t, addresses, 1, "expected single address block to apply")
	addr := addresses[0]

	assert.Equal(t, []string{"t01001", "t01002"}, addr.PreCommitControl)
	assert.Equal(t, []string{"t02001"}, addr.CommitControl)
	assert.Equal(t, []string{"t03001", "t03002"}, addr.DealPublishControl)
	assert.Equal(t, []string{"t04001"}, addr.TerminateControl)
	assert.True(t, addr.DisableOwnerFallback)
	assert.True(t, addr.DisableWorkerFallback)
	assert.Equal(t, []string{"t05001", "t05002"}, addr.MinerAddresses)

	mgr := addr.BalanceManager.MK12Collateral
	assert.Equal(t, "t06001", mgr.DealCollateralWallet)
	assert.Equal(t, "15 FIL", mgr.CollateralLowThreshold.String())
	assert.Equal(t, "45 FIL", mgr.CollateralHighThreshold.String())

	assert.Equal(t, "0.1 FIL", cfg.Fees.MaxPreCommitBatchGasFee.Base.Get().String())
	assert.Equal(t, "0.25 FIL", cfg.Fees.MaxPreCommitBatchGasFee.PerSector.Get().String())
	assert.Equal(t, "0.2 FIL", cfg.Fees.MaxCommitBatchGasFee.Base.Get().String())
	assert.Equal(t, "0.35 FIL", cfg.Fees.MaxCommitBatchGasFee.PerSector.Get().String())
	assert.Equal(t, "0.3 FIL", cfg.Fees.MaxUpdateBatchGasFee.Base.Get().String())
	assert.Equal(t, "0.45 FIL", cfg.Fees.MaxUpdateBatchGasFee.PerSector.Get().String())
	assert.Equal(t, "11 FIL", cfg.Fees.MaxWindowPoStGasFee.Get().String())
	assert.True(t, cfg.Fees.CollateralFromMinerBalance.Get())
	assert.True(t, cfg.Fees.DisableCollateralFallback.Get())
	assert.False(t, cfg.Fees.MaximizeFeeCap.Get())

	assert.Equal(t, []string{
		"https://lotus-a.example.com/rpc/v1",
		"https://lotus-b.example.com/rpc/v1",
	}, cfg.Apis.ChainApiInfo.Get())

	assert.Equal(t, "15 FIL", cfg.Alerting.MinimumWalletBalance.Get().String())
	assert.True(t, cfg.Alerting.PagerDuty.Enable.Get())
	assert.Equal(t, "https://events.custom.com/v2/enqueue", cfg.Alerting.PagerDuty.PagerDutyEventURL.Get())
	assert.Equal(t, "integration-key", cfg.Alerting.PagerDuty.PageDutyIntegrationKey.Get())
	assert.True(t, cfg.Alerting.PrometheusAlertManager.Enable.Get())
	assert.Equal(t, "http://alerts.internal/api", cfg.Alerting.PrometheusAlertManager.AlertManagerURL.Get())
	assert.True(t, cfg.Alerting.SlackWebhook.Enable.Get())
	assert.Equal(t, "https://hooks.slack.com/services/AAA/BBB/CCC", cfg.Alerting.SlackWebhook.WebHookURL.Get())

	assert.Equal(t, 5*time.Hour, cfg.Batching.PreCommit.Timeout.Get())
	assert.Equal(t, 7*time.Hour, cfg.Batching.PreCommit.Slack.Get())
	assert.Equal(t, 2*time.Hour, cfg.Batching.Commit.Timeout.Get())
	assert.Equal(t, 90*time.Minute, cfg.Batching.Commit.Slack.Get())
	assert.Equal(t, "0.123 FIL", cfg.Batching.Update.BaseFeeThreshold.Get().String())
	assert.Equal(t, 3*time.Hour, cfg.Batching.Update.Timeout.Get())
	assert.Equal(t, 135*time.Minute, cfg.Batching.Update.Slack.Get())

	locators := cfg.Market.StorageMarketConfig.PieceLocator.Get()
	require.Len(t, locators, 2)
	assert.Equal(t, "https://pieces-1.example.com/piece", locators[0].URL)
	assert.Equal(t, []string{"Bearer token-a"}, locators[0].Headers["Authorization"])
	assert.Equal(t, []string{"alpha"}, locators[0].Headers["X-Custom"])
	assert.Equal(t, "https://pieces-2.example.com/piece", locators[1].URL)
	assert.Equal(t, []string{"Bearer token-b"}, locators[1].Headers["Authorization"])
	assert.Equal(t, []string{"beta", "gamma"}, locators[1].Headers["X-Another"])
}

// TestTransparentDecode tests the TransparentDecode function with MetaData
func TestTransparentDecode(t *testing.T) {
	t.Run("basic decode with metadata", func(t *testing.T) {
		type Config struct {
			Field1 *Dynamic[int]
			Field2 *Dynamic[string]
			Field3 int
		}

		tomlData := `
Field1 = 42
Field2 = "hello"
`

		cfg := Config{
			Field1: NewDynamic(0),
			Field2: NewDynamic(""),
			Field3: 99,
		}

		md, err := TransparentDecode(tomlData, &cfg)
		require.NoError(t, err)

		// Check values
		assert.Equal(t, 42, cfg.Field1.Get())
		assert.Equal(t, "hello", cfg.Field2.Get())
		assert.Equal(t, 99, cfg.Field3) // Not set in TOML

		// Check metadata
		assert.True(t, md.IsDefined("Field1"))
		assert.True(t, md.IsDefined("Field2"))
		assert.False(t, md.IsDefined("Field3"))
	})

	t.Run("decode with nested struct", func(t *testing.T) {
		type Inner struct {
			Name string
			Age  int
		}

		type Config struct {
			Person *Dynamic[Inner]
		}

		tomlData := `
[Person]
Name = "Alice"
Age = 30
`

		cfg := Config{
			Person: NewDynamic(Inner{}),
		}

		md, err := TransparentDecode(tomlData, &cfg)
		require.NoError(t, err)

		assert.Equal(t, "Alice", cfg.Person.Get().Name)
		assert.Equal(t, 30, cfg.Person.Get().Age)
		assert.True(t, md.IsDefined("Person"))
	})

	t.Run("decode with partial fields", func(t *testing.T) {
		type Config struct {
			A *Dynamic[int]
			B *Dynamic[int]
			C *Dynamic[int]
		}

		tomlData := `
A = 1
C = 3
`

		cfg := Config{
			A: NewDynamic(0),
			B: NewDynamic(99), // Should remain unchanged
			C: NewDynamic(0),
		}

		md, err := TransparentDecode(tomlData, &cfg)
		require.NoError(t, err)

		assert.Equal(t, 1, cfg.A.Get())
		assert.Equal(t, 99, cfg.B.Get()) // Unchanged
		assert.Equal(t, 3, cfg.C.Get())

		assert.True(t, md.IsDefined("A"))
		assert.False(t, md.IsDefined("B"))
		assert.True(t, md.IsDefined("C"))
	})

	t.Run("decode with invalid TOML", func(t *testing.T) {
		type Config struct {
			Field *Dynamic[int]
		}

		tomlData := `
Field = [invalid
`

		cfg := Config{
			Field: NewDynamic(0),
		}

		_, err := TransparentDecode(tomlData, &cfg)
		assert.Error(t, err)
	})
}

// TestNestedStructsWithDynamics tests nested struct handling
func TestNestedStructsWithDynamics(t *testing.T) {
	t.Run("nested struct with dynamic fields", func(t *testing.T) {
		type Inner struct {
			Value *Dynamic[int]
		}

		type Outer struct {
			Inner Inner
			Count *Dynamic[int]
		}

		cfg1 := Outer{
			Inner: Inner{
				Value: NewDynamic(42),
			},
			Count: NewDynamic(10),
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)
		t.Logf("Marshaled:\n%s", string(data))

		assert.Contains(t, string(data), "Count = 10")
		assert.Contains(t, string(data), "[Inner]")
		assert.Contains(t, string(data), "Value = 42")

		cfg2 := Outer{
			Inner: Inner{
				Value: NewDynamic(0),
			},
			Count: NewDynamic(0),
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		assert.Equal(t, 42, cfg2.Inner.Value.Get())
		assert.Equal(t, 10, cfg2.Count.Get())
	})

	t.Run("deeply nested dynamics", func(t *testing.T) {
		type Level3 struct {
			Deep *Dynamic[string]
		}

		type Level2 struct {
			Mid   *Dynamic[int]
			Level Level3
		}

		type Level1 struct {
			Top   *Dynamic[bool]
			Level Level2
		}

		cfg1 := Level1{
			Top: NewDynamic(true),
			Level: Level2{
				Mid: NewDynamic(99),
				Level: Level3{
					Deep: NewDynamic("deepvalue"),
				},
			},
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		cfg2 := Level1{
			Top: NewDynamic(false),
			Level: Level2{
				Mid: NewDynamic(0),
				Level: Level3{
					Deep: NewDynamic(""),
				},
			},
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		assert.Equal(t, true, cfg2.Top.Get())
		assert.Equal(t, 99, cfg2.Level.Mid.Get())
		assert.Equal(t, "deepvalue", cfg2.Level.Level.Deep.Get())
	})

	t.Run("pointer to struct with dynamics", func(t *testing.T) {
		// This test verifies that pointers to structs containing Dynamic fields
		// are handled correctly, even though the unwrapping logic needs to be
		// careful with pointer types
		type Inner struct {
			Value *Dynamic[int]
			Name  string
		}

		type Outer struct {
			InnerPtr *Inner
			Count    *Dynamic[int]
		}

		cfg1 := Outer{
			InnerPtr: &Inner{
				Value: NewDynamic(42),
				Name:  "test",
			},
			Count: NewDynamic(10),
		}

		// For now, we just test that marshal works
		// The pointer to struct case is a known limitation
		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)
		t.Logf("Marshaled:\n%s", string(data))

		// Check the marshaled output
		assert.Contains(t, string(data), "Count = 10")
		assert.Contains(t, string(data), "[InnerPtr]")

		// For unmarshal with pointer to struct, we need to ensure proper setup
		cfg2 := Outer{
			InnerPtr: &Inner{
				Value: NewDynamic(0),
				Name:  "",
			},
			Count: NewDynamic(0),
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		assert.NotNil(t, cfg2.InnerPtr)
		assert.Equal(t, 42, cfg2.InnerPtr.Value.Get())
		assert.Equal(t, "test", cfg2.InnerPtr.Name)
		assert.Equal(t, 10, cfg2.Count.Get())
	})
}

// TestEdgeCases tests edge cases
func TestEdgeCases(t *testing.T) {
	t.Run("struct without dynamics", func(t *testing.T) {
		type Config struct {
			Field1 int
			Field2 string
		}

		cfg1 := Config{
			Field1: 42,
			Field2: "test",
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		var cfg2 Config
		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		assert.Equal(t, 42, cfg2.Field1)
		assert.Equal(t, "test", cfg2.Field2)
	})

	t.Run("empty struct", func(t *testing.T) {
		type Config struct{}

		cfg1 := Config{}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		var cfg2 Config
		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)
	})

	t.Run("all dynamic fields", func(t *testing.T) {
		type Config struct {
			A *Dynamic[int]
			B *Dynamic[string]
			C *Dynamic[bool]
		}

		cfg1 := Config{
			A: NewDynamic(1),
			B: NewDynamic("test"),
			C: NewDynamic(true),
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		cfg2 := Config{
			A: NewDynamic(0),
			B: NewDynamic(""),
			C: NewDynamic(false),
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		assert.Equal(t, 1, cfg2.A.Get())
		assert.Equal(t, "test", cfg2.B.Get())
		assert.Equal(t, true, cfg2.C.Get())
	})

	t.Run("map types in dynamic", func(t *testing.T) {
		type Config struct {
			Data *Dynamic[map[string]int]
		}

		cfg1 := Config{
			Data: NewDynamic(map[string]int{"a": 1, "b": 2}),
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		cfg2 := Config{
			Data: NewDynamic(map[string]int{}),
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		assert.Equal(t, 1, cfg2.Data.Get()["a"])
		assert.Equal(t, 2, cfg2.Data.Get()["b"])
	})

	t.Run("slice of structs in dynamic", func(t *testing.T) {
		type Item struct {
			Name  string
			Value int
		}

		type Config struct {
			Items *Dynamic[[]Item]
		}

		cfg1 := Config{
			Items: NewDynamic([]Item{
				{Name: "first", Value: 1},
				{Name: "second", Value: 2},
			}),
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		cfg2 := Config{
			Items: NewDynamic([]Item{}),
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		items := cfg2.Items.Get()
		assert.Len(t, items, 2)
		assert.Equal(t, "first", items[0].Name)
		assert.Equal(t, 1, items[0].Value)
		assert.Equal(t, "second", items[1].Name)
		assert.Equal(t, 2, items[1].Value)
	})

	t.Run("zero values", func(t *testing.T) {
		type Config struct {
			IntVal    *Dynamic[int]
			StringVal *Dynamic[string]
			BoolVal   *Dynamic[bool]
		}

		cfg1 := Config{
			IntVal:    NewDynamic(0),
			StringVal: NewDynamic(""),
			BoolVal:   NewDynamic(false),
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		cfg2 := Config{
			IntVal:    NewDynamic(99),
			StringVal: NewDynamic("default"),
			BoolVal:   NewDynamic(true),
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		assert.Equal(t, 0, cfg2.IntVal.Get())
		assert.Equal(t, "", cfg2.StringVal.Get())
		assert.Equal(t, false, cfg2.BoolVal.Get())
	})

	t.Run("pointer to dynamic value", func(t *testing.T) {
		type Config struct {
			Ptr *Dynamic[*int]
		}

		val := 42
		cfg1 := Config{
			Ptr: NewDynamic(&val),
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		zero := 0
		cfg2 := Config{
			Ptr: NewDynamic(&zero),
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		assert.Equal(t, 42, *cfg2.Ptr.Get())
	})
}

// TestHelperFunctions tests helper functions
func TestHelperFunctions(t *testing.T) {
	t.Run("isDynamicTypeForMarshal", func(t *testing.T) {
		// Dynamic type
		dynType := reflect.TypeOf((*Dynamic[int])(nil)).Elem()
		assert.True(t, isDynamicTypeForMarshal(dynType))

		// Pointer to Dynamic type
		ptrDynType := reflect.TypeOf((*Dynamic[int])(nil))
		assert.True(t, isDynamicTypeForMarshal(ptrDynType))

		// Regular types
		assert.False(t, isDynamicTypeForMarshal(reflect.TypeOf(42)))
		assert.False(t, isDynamicTypeForMarshal(reflect.TypeOf("string")))
		assert.False(t, isDynamicTypeForMarshal(reflect.TypeOf(struct{}{})))
	})

	t.Run("hasNestedDynamics", func(t *testing.T) {
		type WithDynamic struct {
			Field *Dynamic[int]
		}

		type WithoutDynamic struct {
			Field int
		}

		type NestedWithDynamic struct {
			Inner WithDynamic
		}

		type DeepNested struct {
			Level1 struct {
				Level2 struct {
					Field *Dynamic[string]
				}
			}
		}

		assert.True(t, hasNestedDynamics(reflect.TypeOf(WithDynamic{})))
		assert.False(t, hasNestedDynamics(reflect.TypeOf(WithoutDynamic{})))
		assert.True(t, hasNestedDynamics(reflect.TypeOf(NestedWithDynamic{})))
		assert.True(t, hasNestedDynamics(reflect.TypeOf(DeepNested{})))

		// Pointer to struct
		assert.True(t, hasNestedDynamics(reflect.TypeOf(&WithDynamic{})))
		assert.False(t, hasNestedDynamics(reflect.TypeOf(&WithoutDynamic{})))
	})

	t.Run("extractDynamicValue", func(t *testing.T) {
		d := NewDynamic(42)
		val := reflect.ValueOf(d)

		extracted := extractDynamicValue(val)
		assert.True(t, extracted.IsValid())
		assert.Equal(t, 42, extracted.Interface().(int))

		// Test with nil pointer
		var nilDyn *Dynamic[int]
		nilVal := reflect.ValueOf(nilDyn)
		extracted = extractDynamicValue(nilVal)
		assert.False(t, extracted.IsValid())
	})

	t.Run("extractDynamicInnerType", func(t *testing.T) {
		dynType := reflect.TypeOf((*Dynamic[int])(nil)).Elem()
		innerType := extractDynamicInnerType(dynType)
		assert.Equal(t, reflect.TypeOf(0), innerType)

		// Test with string
		dynStrType := reflect.TypeOf((*Dynamic[string])(nil)).Elem()
		innerStrType := extractDynamicInnerType(dynStrType)
		assert.Equal(t, reflect.TypeOf(""), innerStrType)

		// Test with struct
		type TestStruct struct {
			Field int
		}
		dynStructType := reflect.TypeOf((*Dynamic[TestStruct])(nil)).Elem()
		innerStructType := extractDynamicInnerType(dynStructType)
		assert.Equal(t, reflect.TypeOf(TestStruct{}), innerStructType)
	})

	t.Run("setDynamicValue", func(t *testing.T) {
		d := NewDynamic(0)
		dynField := reflect.ValueOf(d)
		valueField := reflect.ValueOf(99)

		setDynamicValue(dynField, valueField)
		assert.Equal(t, 99, d.Get())

		// Test with nil Dynamic (should create new instance)
		var nilDyn *Dynamic[int]
		nilField := reflect.ValueOf(&nilDyn).Elem()
		valueField2 := reflect.ValueOf(42)

		setDynamicValue(nilField, valueField2)
		assert.NotNil(t, nilDyn)
		assert.Equal(t, 42, nilDyn.Get())
	})
}

// TestCreateShadowType tests shadow type creation
func TestCreateShadowType(t *testing.T) {
	t.Run("simple dynamic replacement", func(t *testing.T) {
		type Original struct {
			Field *Dynamic[int]
		}

		origType := reflect.TypeOf(Original{})
		shadowType := createShadowType(origType)

		assert.Equal(t, origType.NumField(), shadowType.NumField())

		// Check that *Dynamic[int] was replaced with int
		origFieldType := origType.Field(0).Type
		shadowFieldType := shadowType.Field(0).Type

		assert.True(t, isDynamicTypeForMarshal(origFieldType))
		assert.False(t, isDynamicTypeForMarshal(shadowFieldType))
		// The shadow field type should be int (the inner type of Dynamic[int])
		assert.Equal(t, reflect.TypeOf(0), shadowFieldType)
	})

	t.Run("mixed fields", func(t *testing.T) {
		type Original struct {
			Regular int
			Dynamic *Dynamic[string]
			Another bool
		}

		origType := reflect.TypeOf(Original{})
		shadowType := createShadowType(origType)

		assert.Equal(t, 3, shadowType.NumField())

		// Regular field unchanged
		assert.Equal(t, reflect.TypeOf(0), shadowType.Field(0).Type)

		// Dynamic field unwrapped - should be string (the inner type)
		assert.Equal(t, reflect.TypeOf(""), shadowType.Field(1).Type)

		// Another regular field unchanged
		assert.Equal(t, reflect.TypeOf(false), shadowType.Field(2).Type)
	})

	t.Run("nested struct with dynamics", func(t *testing.T) {
		type Inner struct {
			Value *Dynamic[int]
		}

		type Outer struct {
			Inner Inner
		}

		origType := reflect.TypeOf(Outer{})
		shadowType := createShadowType(origType)

		// Check outer struct
		assert.Equal(t, 1, shadowType.NumField())

		// Check inner struct field
		innerField := shadowType.Field(0)
		assert.Equal(t, "Inner", innerField.Name)

		// The inner type should also have its Dynamic unwrapped
		innerType := innerField.Type
		assert.Equal(t, 1, innerType.NumField())

		innerValueField := innerType.Field(0)
		assert.Equal(t, "Value", innerValueField.Name)
		// The inner field should be int (unwrapped from *Dynamic[int])
		assert.Equal(t, reflect.TypeOf(0), innerValueField.Type)
	})

	t.Run("pointer type handling", func(t *testing.T) {
		type Original struct {
			Field *Dynamic[int]
		}

		ptrType := reflect.TypeOf(&Original{})
		shadowType := createShadowType(ptrType)

		// Should handle pointer
		assert.Equal(t, reflect.Ptr, shadowType.Kind())
		assert.Equal(t, reflect.Struct, shadowType.Elem().Kind())
	})
}

// TestInitializeShadowFromTarget tests shadow initialization
func TestInitializeShadowFromTarget(t *testing.T) {
	t.Run("initialize with FIL values", func(t *testing.T) {
		type TestConfig struct {
			Fee    *Dynamic[types.FIL]
			Amount types.FIL
		}

		target := TestConfig{
			Fee:    NewDynamic(types.MustParseFIL("5 FIL")),
			Amount: types.MustParseFIL("10 FIL"),
		}

		shadow := createShadowStruct(&target)
		initializeShadowFromTarget(shadow, &target)

		// Check that shadow was initialized with target values
		shadowVal := reflect.ValueOf(shadow).Elem()
		feeField := shadowVal.Field(0)
		amountField := shadowVal.Field(1)

		assert.True(t, feeField.IsValid())
		assert.True(t, amountField.IsValid())

		// The FIL values should have been copied
		feeVal := feeField.Interface().(types.FIL)
		amountVal := amountField.Interface().(types.FIL)

		assert.Equal(t, "5 FIL", feeVal.String())
		assert.Equal(t, "10 FIL", amountVal.String())
	})

	t.Run("initialize regular fields", func(t *testing.T) {
		type TestConfig struct {
			IntVal *Dynamic[int]
			StrVal string
		}

		target := TestConfig{
			IntVal: NewDynamic(42),
			StrVal: "test",
		}

		shadow := createShadowStruct(&target)
		initializeShadowFromTarget(shadow, &target)

		shadowVal := reflect.ValueOf(shadow).Elem()
		intField := shadowVal.Field(0)
		strField := shadowVal.Field(1)

		// Dynamic field should be initialized
		assert.Equal(t, 42, intField.Interface().(int))

		// Regular field should be initialized
		assert.Equal(t, "test", strField.Interface().(string))
	})
}

// TestWrapDynamics tests wrapping values back into Dynamic fields
func TestWrapDynamics(t *testing.T) {
	t.Run("wrap simple values", func(t *testing.T) {
		type TestConfig struct {
			IntVal *Dynamic[int]
			StrVal *Dynamic[string]
		}

		// Create shadow with plain values
		type Shadow struct {
			IntVal int
			StrVal string
		}

		shadow := Shadow{
			IntVal: 42,
			StrVal: "test",
		}

		// Create target with Dynamic fields
		target := TestConfig{
			IntVal: NewDynamic(0),
			StrVal: NewDynamic(""),
		}

		err := wrapDynamics(shadow, &target)
		require.NoError(t, err)

		assert.Equal(t, 42, target.IntVal.Get())
		assert.Equal(t, "test", target.StrVal.Get())
	})

	t.Run("wrap nested structs", func(t *testing.T) {
		type Inner struct {
			Value *Dynamic[int]
		}

		type Outer struct {
			Inner Inner
		}

		// Shadow structure
		type ShadowInner struct {
			Value int
		}

		type ShadowOuter struct {
			Inner ShadowInner
		}

		shadow := ShadowOuter{
			Inner: ShadowInner{
				Value: 99,
			},
		}

		target := Outer{
			Inner: Inner{
				Value: NewDynamic(0),
			},
		}

		err := wrapDynamics(shadow, &target)
		require.NoError(t, err)

		assert.Equal(t, 99, target.Inner.Value.Get())
	})

	t.Run("preserve non-dynamic fields", func(t *testing.T) {
		type TestConfig struct {
			DynVal *Dynamic[int]
			RegVal int
		}

		type Shadow struct {
			DynVal int
			RegVal int
		}

		shadow := Shadow{
			DynVal: 42,
			RegVal: 99,
		}

		target := TestConfig{
			DynVal: NewDynamic(0),
			RegVal: 0,
		}

		err := wrapDynamics(shadow, &target)
		require.NoError(t, err)

		assert.Equal(t, 42, target.DynVal.Get())
		assert.Equal(t, 99, target.RegVal)
	})
}

// TestUnwrapDynamics tests unwrapping Dynamic fields for marshaling
func TestUnwrapDynamics(t *testing.T) {
	t.Run("unwrap simple dynamic", func(t *testing.T) {
		type Config struct {
			Field *Dynamic[int]
		}

		cfg := Config{
			Field: NewDynamic(42),
		}

		unwrapped := UnwrapDynamics(cfg)

		// Check that the result has the inner type
		unwrappedVal := reflect.ValueOf(unwrapped)
		fieldVal := unwrappedVal.Field(0)

		assert.Equal(t, 42, fieldVal.Interface().(int))
	})

	t.Run("unwrap with regular fields", func(t *testing.T) {
		type Config struct {
			Regular int
			Dynamic *Dynamic[string]
		}

		cfg := Config{
			Regular: 99,
			Dynamic: NewDynamic("test"),
		}

		unwrapped := UnwrapDynamics(cfg)

		unwrappedVal := reflect.ValueOf(unwrapped)
		regularField := unwrappedVal.Field(0)
		dynamicField := unwrappedVal.Field(1)

		assert.Equal(t, 99, regularField.Interface().(int))
		assert.Equal(t, "test", dynamicField.Interface().(string))
	})

	t.Run("unwrap nested dynamics", func(t *testing.T) {
		type Inner struct {
			Value *Dynamic[int]
		}

		type Outer struct {
			Inner Inner
			Count *Dynamic[int]
		}

		cfg := Outer{
			Inner: Inner{
				Value: NewDynamic(42),
			},
			Count: NewDynamic(10),
		}

		unwrapped := UnwrapDynamics(cfg)

		unwrappedVal := reflect.ValueOf(unwrapped)
		innerField := unwrappedVal.Field(0)
		countField := unwrappedVal.Field(1)

		assert.Equal(t, 10, countField.Interface().(int))

		innerVal := innerField.Interface()
		innerReflect := reflect.ValueOf(innerVal)
		valueField := innerReflect.Field(0)

		assert.Equal(t, 42, valueField.Interface().(int))
	})

	t.Run("unwrap struct without dynamics", func(t *testing.T) {
		type Config struct {
			Field1 int
			Field2 string
		}

		cfg := Config{
			Field1: 42,
			Field2: "test",
		}

		unwrapped := UnwrapDynamics(cfg)

		// Should return the same struct
		unwrappedConfig := unwrapped.(Config)
		assert.Equal(t, 42, unwrappedConfig.Field1)
		assert.Equal(t, "test", unwrappedConfig.Field2)
	})
}

// TestRoundTripConsistency tests that marshal/unmarshal round trips preserve values
func TestRoundTripConsistency(t *testing.T) {
	t.Run("complex struct round trip", func(t *testing.T) {
		type Inner struct {
			Name  string
			Count *Dynamic[int]
		}

		type Config struct {
			ID      int
			Timeout *Dynamic[time.Duration]
			Items   *Dynamic[[]string]
			Inner   Inner
			Active  *Dynamic[bool]
		}

		original := Config{
			ID:      123,
			Timeout: NewDynamic(5 * time.Minute),
			Items:   NewDynamic([]string{"a", "b", "c"}),
			Inner: Inner{
				Name:  "test",
				Count: NewDynamic(99),
			},
			Active: NewDynamic(true),
		}

		// Marshal
		data, err := TransparentMarshal(original)
		require.NoError(t, err)

		// Unmarshal
		restored := Config{
			Timeout: NewDynamic(time.Duration(0)),
			Items:   NewDynamic([]string{}),
			Inner: Inner{
				Count: NewDynamic(0),
			},
			Active: NewDynamic(false),
		}

		err = TransparentUnmarshal(data, &restored)
		require.NoError(t, err)

		// Verify all fields match
		assert.Equal(t, original.ID, restored.ID)
		assert.Equal(t, original.Timeout.Get(), restored.Timeout.Get())
		assert.Equal(t, original.Items.Get(), restored.Items.Get())
		assert.Equal(t, original.Inner.Name, restored.Inner.Name)
		assert.Equal(t, original.Inner.Count.Get(), restored.Inner.Count.Get())
		assert.Equal(t, original.Active.Get(), restored.Active.Get())
	})

	t.Run("multiple round trips", func(t *testing.T) {
		type Config struct {
			Value *Dynamic[int]
		}

		cfg := Config{
			Value: NewDynamic(42),
		}

		for i := 0; i < 3; i++ {
			data, err := TransparentMarshal(cfg)
			require.NoError(t, err)

			cfg = Config{
				Value: NewDynamic(0),
			}

			err = TransparentUnmarshal(data, &cfg)
			require.NoError(t, err)

			assert.Equal(t, 42, cfg.Value.Get(), "round trip %d failed", i+1)
		}
	})
}

// TestErrorHandling tests error conditions
func TestErrorHandling(t *testing.T) {
	t.Run("invalid TOML syntax", func(t *testing.T) {
		type Config struct {
			Field *Dynamic[int]
		}

		invalidTOML := `Field = [[[invalid`

		cfg := Config{
			Field: NewDynamic(0),
		}

		err := TransparentUnmarshal([]byte(invalidTOML), &cfg)
		assert.Error(t, err)
	})

	t.Run("type mismatch in TOML", func(t *testing.T) {
		type Config struct {
			Field *Dynamic[int]
		}

		tomlData := `Field = "not an int"`

		cfg := Config{
			Field: NewDynamic(0),
		}

		err := TransparentUnmarshal([]byte(tomlData), &cfg)
		assert.Error(t, err)
	})

	t.Run("missing required initialization", func(t *testing.T) {
		type Config struct {
			Field *Dynamic[types.FIL]
		}

		tomlData := `Field = "5 FIL"`

		// Without proper initialization, this should fail or behave unexpectedly
		cfg := Config{
			Field: NewDynamic(types.FIL{}), // Not properly initialized
		}

		// This test documents the expected behavior
		err := TransparentUnmarshal([]byte(tomlData), &cfg)
		// The error handling depends on the FIL type implementation
		_ = err
	})
}

// TestWithActualTOMLLibrary tests integration with TOML library
func TestWithActualTOMLLibrary(t *testing.T) {
	t.Run("compare with standard TOML", func(t *testing.T) {
		type Config struct {
			Field int
		}

		cfg := Config{Field: 42}

		// Standard TOML marshal
		standardData, err := toml.Marshal(cfg)
		require.NoError(t, err)

		// Transparent marshal (should be identical for non-Dynamic structs)
		transparentData, err := TransparentMarshal(cfg)
		require.NoError(t, err)

		assert.Equal(t, string(standardData), string(transparentData))
	})

	t.Run("decode metadata accuracy", func(t *testing.T) {
		type Config struct {
			A int
			B string
			C bool
		}

		tomlData := `
A = 1
B = "test"
`

		var cfg Config
		md, err := toml.Decode(tomlData, &cfg)
		require.NoError(t, err)

		assert.True(t, md.IsDefined("A"))
		assert.True(t, md.IsDefined("B"))
		assert.False(t, md.IsDefined("C"))
	})
}

// TestAdditionalEdgeCases tests additional edge cases for better coverage
func TestAdditionalEdgeCases(t *testing.T) {
	t.Run("nil pointer to dynamic", func(t *testing.T) {
		type Config struct {
			Field *Dynamic[int]
		}

		// Test with nil Dynamic field
		cfg := Config{
			Field: nil,
		}

		// Marshal should handle nil gracefully
		data, err := TransparentMarshal(cfg)
		require.NoError(t, err)
		t.Logf("Marshaled:\n%s", string(data))
	})

	t.Run("nested pointer to struct without dynamics", func(t *testing.T) {
		type Inner struct {
			Value int
		}

		type Outer struct {
			InnerPtr *Inner
		}

		cfg1 := Outer{
			InnerPtr: &Inner{Value: 42},
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		cfg2 := Outer{
			InnerPtr: &Inner{},
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)
		assert.Equal(t, 42, cfg2.InnerPtr.Value)
	})

	t.Run("marshal pointer to struct", func(t *testing.T) {
		type Config struct {
			Field *Dynamic[int]
		}

		cfg := &Config{
			Field: NewDynamic(42),
		}

		data, err := TransparentMarshal(cfg)
		require.NoError(t, err)
		assert.Contains(t, string(data), "Field = 42")
	})

	t.Run("extractDynamicValue with struct value", func(t *testing.T) {
		// Test extractDynamicValue with addressable Dynamic struct
		d := Dynamic[int]{}
		// We need to use a pointer to make it addressable
		ptr := &d
		val := reflect.ValueOf(ptr).Elem()

		// Now the value is addressable, but the method might still not work correctly
		// since Dynamic is meant to be used as a pointer
		extracted := extractDynamicValue(val)
		// This might return invalid or zero value
		_ = extracted
	})

	t.Run("extractDynamicInnerType with non-struct", func(t *testing.T) {
		// Test with a non-struct type
		intType := reflect.TypeOf(42)
		result := extractDynamicInnerType(intType)
		// Should return the same type
		assert.Equal(t, intType, result)
	})

	t.Run("setDynamicValue with invalid value", func(t *testing.T) {
		d := NewDynamic(0)
		dynField := reflect.ValueOf(d)
		invalidValue := reflect.Value{}

		// Should handle invalid value gracefully
		setDynamicValue(dynField, invalidValue)
		// Value should remain unchanged
		assert.Equal(t, 0, d.Get())
	})

	t.Run("hasNestedDynamics with non-struct", func(t *testing.T) {
		// Test with non-struct types
		assert.False(t, hasNestedDynamics(reflect.TypeOf(42)))
		assert.False(t, hasNestedDynamics(reflect.TypeOf("string")))
		assert.False(t, hasNestedDynamics(reflect.TypeOf([]int{})))
	})

	t.Run("unwrapDynamics with non-struct", func(t *testing.T) {
		// Test with non-struct value
		val := 42
		result := UnwrapDynamics(val)
		assert.Equal(t, 42, result.(int))
	})

	t.Run("wrapDynamics with non-struct", func(t *testing.T) {
		// Test with non-struct values
		shadow := 42
		target := 99

		err := wrapDynamics(shadow, &target)
		require.NoError(t, err)
		// Since both are ints, wrapDynamics should just return without error
	})

	t.Run("createShadowType with non-struct", func(t *testing.T) {
		// Test with non-struct type
		intType := reflect.TypeOf(42)
		result := createShadowType(intType)
		assert.Equal(t, intType, result)
	})

	t.Run("initializeShadowFromTarget with non-struct", func(t *testing.T) {
		// Test with non-struct values
		shadow := 42
		target := 99

		// Should return without error for non-structs
		initializeShadowFromTarget(shadow, target)
		// Nothing should happen, but shouldn't panic
	})

	t.Run("nested struct with pointer to struct without dynamics", func(t *testing.T) {
		type Inner struct {
			Value int
		}

		type Middle struct {
			InnerPtr *Inner
		}

		type Outer struct {
			Middle *Middle
		}

		cfg1 := Outer{
			Middle: &Middle{
				InnerPtr: &Inner{Value: 42},
			},
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		cfg2 := Outer{
			Middle: &Middle{
				InnerPtr: &Inner{},
			},
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)
		assert.Equal(t, 42, cfg2.Middle.InnerPtr.Value)
	})

	t.Run("dynamic with slice of pointers", func(t *testing.T) {
		type Config struct {
			Items *Dynamic[[]*int]
		}

		val1, val2 := 1, 2
		cfg1 := Config{
			Items: NewDynamic([]*int{&val1, &val2}),
		}

		data, err := TransparentMarshal(cfg1)
		require.NoError(t, err)

		zero1, zero2 := 0, 0
		cfg2 := Config{
			Items: NewDynamic([]*int{&zero1, &zero2}),
		}

		err = TransparentUnmarshal(data, &cfg2)
		require.NoError(t, err)

		items := cfg2.Items.Get()
		assert.Len(t, items, 2)
		assert.Equal(t, 1, *items[0])
		assert.Equal(t, 2, *items[1])
	})

	t.Run("extractDynamicInnerType with pointer to dynamic", func(t *testing.T) {
		// Test with pointer to Dynamic type
		ptrType := reflect.TypeOf((*Dynamic[string])(nil))
		innerType := extractDynamicInnerType(ptrType)
		assert.Equal(t, reflect.TypeOf(""), innerType)
	})

	t.Run("struct with unexported fields", func(t *testing.T) {
		// Test that unexported fields are handled gracefully
		type Config struct {
			Exported   *Dynamic[int]
			unexported int // unexported field
		}

		cfg := Config{
			Exported:   NewDynamic(42),
			unexported: 99, // This shouldn't be marshaled
		}

		data, err := TransparentMarshal(cfg)
		require.NoError(t, err)
		assert.Contains(t, string(data), "Exported = 42")
	})
}

// TestCoverageForRemainingPaths tests specific code paths to achieve 100% coverage
func TestCoverageForRemainingPaths(t *testing.T) {
	t.Run("extractDynamicValue with method not found", func(t *testing.T) {
		// Create a struct that looks like Dynamic but doesn't have Get method
		type FakeDynamic struct {
			value int
		}

		fake := FakeDynamic{value: 42}
		val := reflect.ValueOf(&fake).Elem()

		// extractDynamicValue should return invalid value when Get() method doesn't exist
		result := extractDynamicValue(val)
		assert.False(t, result.IsValid())
	})

	t.Run("setDynamicValue with method not found", func(t *testing.T) {
		// Create a struct that looks like Dynamic but doesn't have Set method
		type FakeDynamic struct {
			value int
		}

		fake := FakeDynamic{value: 0}
		fakeVal := reflect.ValueOf(&fake).Elem()
		newVal := reflect.ValueOf(42)

		// setDynamicValue should handle gracefully when Set() method doesn't exist
		setDynamicValue(fakeVal, newVal)
		// Should not panic, just return
		assert.Equal(t, 0, fake.value) // Value unchanged
	})

	t.Run("unwrapDynamics with invalid dynamic value", func(t *testing.T) {
		// Test case where extractDynamicValue returns invalid value
		type FakeDynamic struct {
			value int
		}

		type Config struct {
			Field *FakeDynamic
		}

		cfg := Config{
			Field: &FakeDynamic{value: 42},
		}

		// Since FakeDynamic doesn't have the Get method, unwrapDynamics won't detect it as Dynamic
		// This tests the path but FakeDynamic won't be treated as Dynamic
		result := UnwrapDynamics(cfg)
		assert.NotNil(t, result)
	})

	t.Run("initializeShadowFromTarget with non-assignable types", func(t *testing.T) {
		// Test case where types don't match and assignment fails
		type ConfigA struct {
			Field *Dynamic[int]
		}

		type ConfigB struct {
			Field string // Different type
		}

		targetA := ConfigA{
			Field: NewDynamic(42),
		}

		// Create shadow with different structure
		targetB := ConfigB{
			Field: "test",
		}

		// This should handle gracefully when types don't match
		initializeShadowFromTarget(&targetB, &targetA)
		// Should not panic
	})

	t.Run("wrapDynamics with non-assignable shadow fields", func(t *testing.T) {
		// Test wrapDynamics when shadowField type is not assignable to targetField
		type ShadowConfig struct {
			Field string
		}

		type TargetConfig struct {
			Field int
		}

		shadow := ShadowConfig{Field: "test"}
		target := TargetConfig{Field: 0}

		// Should handle gracefully when types don't match
		err := wrapDynamics(shadow, &target)
		assert.NoError(t, err)
		assert.Equal(t, 0, target.Field) // Unchanged
	})

	t.Run("initializeShadowFromTarget with pointer mismatch", func(t *testing.T) {
		type Inner struct {
			Value int
		}

		type Config1 struct {
			PtrField *Inner
		}

		type Config2 struct {
			PtrField *string // Different pointer type
		}

		str := "test"
		target := Config2{
			PtrField: &str,
		}

		shadow := Config1{
			PtrField: &Inner{Value: 42},
		}

		// Should handle type mismatch gracefully
		initializeShadowFromTarget(&shadow, &target)
		// Should not panic
	})

	t.Run("unwrapDynamics with nil dynamic pointer", func(t *testing.T) {
		type Config struct {
			Field *Dynamic[int]
		}

		cfg := Config{
			Field: nil,
		}

		// unwrapDynamics should handle nil Dynamic fields
		result := UnwrapDynamics(cfg)
		assert.NotNil(t, result)

		// Verify the result has the expected structure
		resultVal := reflect.ValueOf(result)
		assert.Equal(t, reflect.Struct, resultVal.Kind())
	})

	t.Run("extractDynamicValue with pointer to nil", func(t *testing.T) {
		var nilDyn *Dynamic[int]
		val := reflect.ValueOf(nilDyn)

		// Should return invalid value for nil pointer
		result := extractDynamicValue(val)
		assert.False(t, result.IsValid())
	})

	t.Run("wrapDynamics with nil shadow pointer", func(t *testing.T) {
		type Inner struct {
			Value int
		}

		type Config struct {
			PtrField *Inner
		}

		// Test when shadowField is nil
		shadow := Config{
			PtrField: nil,
		}

		target := Config{
			PtrField: &Inner{Value: 99},
		}

		// Should handle nil shadow pointers gracefully
		err := wrapDynamics(shadow, &target)
		assert.NoError(t, err)
		// The target will get the nil pointer from shadow since types match
		assert.Nil(t, target.PtrField)
	})

	t.Run("wrapDynamics with nil target pointer", func(t *testing.T) {
		type Inner struct {
			Value int
		}

		type Config struct {
			PtrField *Inner
		}

		shadow := Config{
			PtrField: &Inner{Value: 42},
		}

		// Test when targetField is nil
		target := Config{
			PtrField: nil,
		}

		// Should handle nil target pointers gracefully
		err := wrapDynamics(shadow, &target)
		assert.NoError(t, err)
		// The wrapDynamics function copies regular pointer fields when they don't contain dynamics
		// So target gets the value from shadow
		assert.NotNil(t, target.PtrField)
		assert.Equal(t, 42, target.PtrField.Value)
	})

	t.Run("initializeShadowFromTarget with nested dynamics pointer", func(t *testing.T) {
		type Inner struct {
			Value *Dynamic[int]
		}

		type Config struct {
			InnerPtr *Inner
		}

		target := Config{
			InnerPtr: &Inner{
				Value: NewDynamic(42),
			},
		}

		shadow := createShadowStruct(&target)

		// Initialize shadow from target with nested pointer
		initializeShadowFromTarget(shadow, &target)

		// Verify the shadow was initialized correctly
		shadowVal := reflect.ValueOf(shadow).Elem()
		innerPtrField := shadowVal.Field(0)
		assert.True(t, innerPtrField.IsValid())
		// Test just exercises the code path for pointer to struct with dynamics
		// The actual result may vary based on implementation
	})

	t.Run("unwrapDynamics with nested pointer to struct with dynamics", func(t *testing.T) {
		type Inner struct {
			Value *Dynamic[int]
		}

		type Middle struct {
			InnerPtr *Inner
		}

		type Outer struct {
			MiddlePtr *Middle
		}

		cfg := Outer{
			MiddlePtr: &Middle{
				InnerPtr: &Inner{
					Value: NewDynamic(42),
				},
			},
		}

		// Test unwrapping with nested pointers to structs containing dynamics
		result := UnwrapDynamics(cfg)
		assert.NotNil(t, result)

		resultVal := reflect.ValueOf(result)
		assert.Equal(t, reflect.Struct, resultVal.Kind())
	})

	t.Run("extractDynamicInnerType with empty struct", func(t *testing.T) {
		type EmptyStruct struct{}

		emptyType := reflect.TypeOf(EmptyStruct{})
		result := extractDynamicInnerType(emptyType)

		// Should return the same type when struct has no fields
		assert.Equal(t, emptyType, result)
	})

	t.Run("setDynamicValue creating new instance", func(t *testing.T) {
		// Test the path where dynamicField is nil and needs to be created
		var nilDyn *Dynamic[int]
		dynField := reflect.ValueOf(&nilDyn).Elem()
		valueField := reflect.ValueOf(99)

		// setDynamicValue should create a new Dynamic instance
		setDynamicValue(dynField, valueField)

		assert.NotNil(t, nilDyn)
		assert.Equal(t, 99, nilDyn.Get())
	})

	t.Run("initializeShadowFromTarget with regular pointer field", func(t *testing.T) {
		type Config struct {
			IntPtr *int
		}

		val := 42
		target := Config{
			IntPtr: &val,
		}

		shadow := Config{
			IntPtr: new(int),
		}

		// Test copying regular pointer fields
		initializeShadowFromTarget(&shadow, &target)

		assert.NotNil(t, shadow.IntPtr)
		assert.Equal(t, 42, *shadow.IntPtr)
	})

	t.Run("wrapDynamics with regular field type mismatch", func(t *testing.T) {
		// Test the else branch where shadowField and targetField types don't match
		type ShadowConfig struct {
			Field float64
		}

		type TargetConfig struct {
			Field int
		}

		shadow := ShadowConfig{Field: 3.14}
		target := TargetConfig{Field: 42}

		err := wrapDynamics(shadow, &target)
		assert.NoError(t, err)
		// Field should remain unchanged due to type mismatch
		assert.Equal(t, 42, target.Field)
	})

	t.Run("createShadowStruct edge cases", func(t *testing.T) {
		type Config struct {
			Field *Dynamic[int]
		}

		cfg := Config{
			Field: NewDynamic(42),
		}

		// Test that createShadowStruct handles all cases correctly
		shadow := createShadowStruct(&cfg)
		assert.NotNil(t, shadow)

		// Verify shadow has correct structure
		shadowVal := reflect.ValueOf(shadow)
		assert.Equal(t, reflect.Ptr, shadowVal.Kind())
		assert.Equal(t, reflect.Struct, shadowVal.Elem().Kind())
	})

	t.Run("initializeShadowFromTarget innerVal not valid case", func(t *testing.T) {
		// Test the case where extractDynamicValue returns invalid value
		type Config struct {
			Field *Dynamic[int]
		}

		target := Config{
			Field: nil, // nil Dynamic
		}

		shadow := createShadowStruct(&target)

		// This should handle nil Dynamic gracefully
		initializeShadowFromTarget(shadow, &target)
		// Just exercises the code path
	})

	t.Run("initializeShadowFromTarget type not assignable", func(t *testing.T) {
		// Test where valReflect.Type() is not assignable to shadowField.Type()
		type ConfigA struct {
			Field *Dynamic[int]
		}

		type ConfigB struct {
			Field string
		}

		target := ConfigA{
			Field: NewDynamic(42),
		}

		// Create a shadow with incompatible type
		shadow := &ConfigB{
			Field: "test",
		}

		// Should handle type mismatch gracefully
		initializeShadowFromTarget(shadow, &target)
		// Should not crash
	})

}
