package config

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
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
