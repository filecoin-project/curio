package ffiselect

import (
	"github.com/filecoin-project/curio/lib/ffiselect/ffidirect"
	"reflect"

	"github.com/samber/lo"
	"golang.org/x/xerrors"
)

func callTest(logctx []any, fn string, rawargs ...interface{}) ([]interface{}, error) {
	args := lo.Map(rawargs, func(arg any, i int) reflect.Value {
		return reflect.ValueOf(arg)
	})

	resAry := reflect.ValueOf(ffidirect.FFI{}).MethodByName(fn).Call(args)
	res := lo.Map(resAry, func(res reflect.Value, i int) any {
		return res.Interface()
	})

	if res[len(res)-1].(ffidirect.ErrorString) != "" {
		return nil, xerrors.Errorf("callTest error: %s", res[len(res)-1].(ffidirect.ErrorString))
	}

	return res, nil
}
