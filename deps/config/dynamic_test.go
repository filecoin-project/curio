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

func TestDynamicUnmarshalText(t *testing.T) {
	type TestConfig struct {
		Name  string
		Value int
	}

	d := NewDynamic(TestConfig{})
	tomlData := []byte(`
Name = "test"
Value = 42
`)

	err := d.UnmarshalText(tomlData)
	assert.NoError(t, err)

	result := d.Get()
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
	assert.True(t, cmp.Equal(d1.Get(), d2.Get(), bigIntComparer), "cmp.Equal should work with bigIntComparer")
	assert.False(t, cmp.Equal(d1.Get(), d3.Get(), bigIntComparer), "cmp.Equal should detect differences")
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
