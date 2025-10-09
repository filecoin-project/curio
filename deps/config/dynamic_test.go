package config

import (
	"testing"

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
	res, err := CopyWithOriginalDynamics(input)
	assert.NoError(t, err)
	assert.Equal(t, res.Bar, 10)
	input.Foo.Set(30)
	assert.Equal(t, 30, res.Foo.Dynamic.Get())
}
