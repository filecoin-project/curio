package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

type Input struct {
	Bar int
	Foo struct {
		*Dynamic[int]
	}
}

func TestCopyPartial(t *testing.T) {
	input := &Input{
		Bar: 10,
		Foo: struct {
			*Dynamic[int]
		}{
			Dynamic: NewDynamic(20),
		},
	}
	res, err := CopyWithOriginalDynamics(input) // test that copy succeeds
	assert.NoError(t, err)
	assert.Equal(t, res.Bar, 10)          // spot-test
	assert.True(t, cmp.Equal(res, input)) // test the Equal() function used elsewhere.
	input.Foo.Set(30)
	assert.Equal(t, 30, res.Foo.Get()) // test the Set() and Get() functions

}
