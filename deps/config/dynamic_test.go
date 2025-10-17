package config

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/filecoin-project/lotus/chain/types"
)

type Input struct {
	Bar int
	Foo struct {
		*Dynamic[int]
	}
}

func TestDynamic(t *testing.T) {
	input := &Input{
		Bar: 10,
		Foo: struct {
			*Dynamic[int]
		}{
			Dynamic: NewDynamic(20),
		},
	}
	var notified atomic.Bool
	input.Foo.OnChange(func() {
		notified.Store(true)
	})
	res, err := CopyWithOriginalDynamics(input) // test that copy succeeds
	assert.NoError(t, err)
	assert.Equal(t, res.Bar, 10)          // spot-test the copy
	assert.True(t, cmp.Equal(res, input)) // test the Equal() function used elsewhere.
	input.Foo.Set(30)
	assert.Equal(t, 30, res.Foo.Get()) // test the Set() and Get() functions
	assert.Eventually(t, func() bool { // test the OnChange() function
		return notified.Load()
	}, 10*time.Second, 100*time.Millisecond)
}

func TestDynamicUnmarshalTOML(t *testing.T) {
	type TestConfig struct {
		Name  string
		Value int
	}
	type Wrapper struct {
		Config *Dynamic[TestConfig]
	}

	// Create a config and marshal it using TransparentMarshal
	w1 := Wrapper{Config: NewDynamic(TestConfig{Name: "test", Value: 42})}
	data, err := TransparentMarshal(w1)
	assert.NoError(t, err)
	t.Logf("Generated TOML (%d bytes):\n%s", len(data), string(data))

	// Verify the config value before marshaling
	assert.Equal(t, "test", w1.Config.Get().Name, "Original config should have correct name")
	assert.Equal(t, 42, w1.Config.Get().Value, "Original config should have correct value")

	// Unmarshal it back using TransparentUnmarshal
	var w2 Wrapper
	w2.Config = NewDynamic(TestConfig{})
	err = TransparentUnmarshal(data, &w2)
	assert.NoError(t, err)

	result := w2.Config.Get()
	assert.Equal(t, "test", result.Name)
	assert.Equal(t, 42, result.Value)
}

func TestDynamicWithBigInt(t *testing.T) {
	type ConfigWithFIL struct {
		Fee types.FIL
	}

	// Test that BigInt values compare correctly
	d1 := NewDynamic(ConfigWithFIL{
		Fee: types.MustParseFIL("5 FIL"),
	})

	d2 := NewDynamic(ConfigWithFIL{
		Fee: types.MustParseFIL("5 FIL"),
	})

	d3 := NewDynamic(ConfigWithFIL{
		Fee: types.MustParseFIL("10 FIL"),
	})

	// Test Equal method works with BigInt
	assert.True(t, d1.Equal(d2), "Equal FIL values should be equal")
	assert.False(t, d1.Equal(d3), "Different FIL values should not be equal")

	// Test that cmp.Equal works with bigIntComparer
	assert.True(t, cmp.Equal(d1.Get(), d2.Get(), BigIntComparer), "cmp.Equal should work with bigIntComparer")
	assert.False(t, cmp.Equal(d1.Get(), d3.Get(), BigIntComparer), "cmp.Equal should detect differences")
}

func TestDynamicChangeNotificationWithBigInt(t *testing.T) {
	type ConfigWithFIL struct {
		Fee types.FIL
	}

	d := NewDynamic(ConfigWithFIL{
		Fee: types.MustParseFIL("5 FIL"),
	})

	var notified atomic.Bool
	d.OnChange(func() {
		notified.Store(true)
	})

	// Use public API to change value
	d.Set(ConfigWithFIL{Fee: types.MustParseFIL("10 FIL")})

	// Verify notification was triggered
	assert.Eventually(t, func() bool {
		return notified.Load()
	}, 2*time.Second, 100*time.Millisecond, "OnChange should be called when BigInt value changes")

	// Reset and test that same value doesn't trigger notification
	notified.Store(false)
	d.Set(ConfigWithFIL{Fee: types.MustParseFIL("10 FIL")}) // Same value as current

	// Give it a moment to potentially trigger (it shouldn't)
	time.Sleep(200 * time.Millisecond)
	assert.False(t, notified.Load(), "OnChange should not be called when BigInt value stays the same")
}

func TestDynamicMarshalSlice(t *testing.T) {
	// Test that Dynamic wrapping a slice can be marshaled to TOML
	type Address struct {
		Name string
		URL  string
	}

	type ConfigWithSlice struct {
		Addresses *Dynamic[[]Address]
	}

	cfg := ConfigWithSlice{
		Addresses: NewDynamic([]Address{
			{Name: "addr1", URL: "http://example.com"},
			{Name: "addr2", URL: "http://example.org"},
		}),
	}

	// Test that the full struct can be marshaled to TOML using TransparentMarshal
	data, err := TransparentMarshal(cfg)
	assert.NoError(t, err, "Should be able to marshal config with Dynamic slice to TOML")

	// Test round-trip: unmarshal back using TransparentUnmarshal
	var cfg2 ConfigWithSlice
	cfg2.Addresses = NewDynamic([]Address{})
	err = TransparentUnmarshal(data, &cfg2)
	assert.NoError(t, err, "Should be able to unmarshal config with Dynamic slice from TOML")

	// Verify the data is correct
	assert.Len(t, cfg2.Addresses.Get(), 2)
	assert.Equal(t, "addr1", cfg2.Addresses.Get()[0].Name)
	assert.Equal(t, "http://example.com", cfg2.Addresses.Get()[0].URL)
	assert.Equal(t, "addr2", cfg2.Addresses.Get()[1].Name)
}

func TestDefaultCurioConfigMarshal(t *testing.T) {
	// Test that the default config with Dynamic fields can be marshaled
	cfg := DefaultCurioConfig()

	// This should not panic or error using TransparentMarshal
	data, err := TransparentMarshal(cfg)
	assert.NoError(t, err, "Should be able to marshal DefaultCurioConfig to TOML")
	assert.NotEmpty(t, data)
	t.Logf("Successfully marshaled config to %d bytes of TOML", len(data))
}

func TestCurioConfigRoundTrip(t *testing.T) {
	// Test full marshal/unmarshal round-trip with CurioConfig
	cfg1 := DefaultCurioConfig()

	// Modify some Dynamic values to test they persist
	cfg1.Ingest.MaxQueueDownload.Set(16)
	cfg1.Ingest.MaxMarketRunningPipelines.Set(32)

	// Marshal to TOML using TransparentMarshal
	data, err := TransparentMarshal(cfg1)
	assert.NoError(t, err, "Should be able to marshal config")
	t.Logf("Marshaled %d bytes", len(data))

	// Unmarshal back to a new config (starting with defaults to initialize FIL types properly)
	cfg2 := DefaultCurioConfig()
	err = TransparentUnmarshal(data, cfg2)
	assert.NoError(t, err, "Should be able to unmarshal config back")

	// Verify Dynamic values were preserved
	assert.Equal(t, 16, cfg2.Ingest.MaxQueueDownload.Get(), "MaxQueueDownload should be preserved")
	assert.Equal(t, 32, cfg2.Ingest.MaxMarketRunningPipelines.Get(), "MaxMarketRunningPipelines should be preserved")

	// Verify the Addresses Dynamic slice was preserved
	assert.Equal(t, len(cfg1.Addresses.Get()), len(cfg2.Addresses.Get()), "Addresses slice length should match")

	// Verify static fields were preserved
	assert.Equal(t, cfg1.Subsystems.GuiAddress, cfg2.Subsystems.GuiAddress)
}
