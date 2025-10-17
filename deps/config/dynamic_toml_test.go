package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
