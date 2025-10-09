package config

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var logger = logging.Logger("config-dynamic")
var DynamicMx sync.RWMutex

type Dynamic[T any] struct {
	value T
}

func NewDynamic[T any](value T) *Dynamic[T] {
	return &Dynamic[T]{value: value}
}

func (d *Dynamic[T]) Set(value T) {
	DynamicMx.Lock()
	defer DynamicMx.Unlock()
	d.value = value
}

func (d *Dynamic[T]) Get() T {
	DynamicMx.RLock()
	defer DynamicMx.RUnlock()
	return d.value
}

// UnmarshalText unmarshals the text into the dynamic value.
// After initial setting, future updates require a lock on the DynamicMx mutex before calling toml.Decode.
func (d *Dynamic[T]) UnmarshalText(text []byte) error {
	return toml.Unmarshal(text, d.value)
}

// For checkers.
func (d *Dynamic[T]) MarshalText() ([]byte, error) {
	return toml.Marshal(d.value)
}

type cfgRoot[T any] struct {
	db       *harmonydb.DB
	layers   []string
	treeCopy T
	fixupFn  func(string, T) error
}

func EnableChangeDetection[T any](db *harmonydb.DB, obj T, layers []string, fixupFn func(string, T) error) error {
	var err error
	r := &cfgRoot[T]{db: db, treeCopy: obj, layers: layers, fixupFn: fixupFn}
	r.treeCopy, err = CopyWithOriginalDynamics(obj)
	if err != nil {
		return err
	}
	go r.changeMonitor()
	return nil
}

// copyWithOriginalDynamics copies the original dynamics from the original object to the new object.
func CopyWithOriginalDynamics[T any](orig T) (T, error) {
	typ := reflect.TypeOf(orig)
	val := reflect.ValueOf(orig)

	// Handle pointer to struct
	if typ.Kind() == reflect.Ptr {
		if typ.Elem().Kind() != reflect.Struct {
			var zero T
			return zero, fmt.Errorf("expected pointer to struct, got pointer to %s", typ.Elem().Kind())
		}
		// Create a new instance of the struct
		result := reflect.New(typ.Elem())
		walker(val.Elem(), result.Elem())
		return result.Interface().(T), nil
	}

	// Handle direct struct
	if typ.Kind() != reflect.Struct {
		var zero T
		return zero, fmt.Errorf("expected struct or pointer to struct, got %s", typ.Kind())
	}

	result := reflect.New(typ).Elem()
	walker(val, result)
	return result.Interface().(T), nil
}

// walker recursively walks the struct tree, copying fields and preserving Dynamic pointers
func walker(orig, result reflect.Value) {
	for i := 0; i < orig.NumField(); i++ {
		field := orig.Field(i)
		resultField := result.Field(i)

		switch field.Kind() {
		case reflect.Struct:
			// Check if this struct is a Dynamic[T] - if so, copy by value
			if isDynamicType(field.Type()) {
				resultField.Set(field)
			} else {
				walker(field, resultField)
			}
		case reflect.Ptr:
			if !field.IsNil() {
				// Check if the pointed-to type is Dynamic[T]
				elemType := field.Type().Elem()
				if isDynamicType(elemType) {
					// This is *Dynamic[T] - copy the pointer to preserve sharing
					resultField.Set(field)
				} else if elemType.Kind() == reflect.Struct {
					// Regular struct pointer - recursively copy
					newPtr := reflect.New(elemType)
					walker(field.Elem(), newPtr.Elem())
					resultField.Set(newPtr)
				} else {
					// Other pointer types - shallow copy
					resultField.Set(field)
				}
			}
		default:
			resultField.Set(field)
		}
	}
}

// isDynamicType checks if a type is Dynamic[T] by checking if the name starts with "Dynamic"
func isDynamicType(t reflect.Type) bool {
	name := t.Name()
	return strings.HasPrefix(name, "Dynamic[")
}

func (r *cfgRoot[T]) changeMonitor() {
	lastTimestamp := time.Time{} // lets do a read at startup

	for {
		configCount := 0
		err := r.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM harmony_config WHERE timestamp > $1 AND title IN ($2)`, lastTimestamp, strings.Join(r.layers, ",")).Scan(&configCount)
		if err != nil {
			logger.Errorf("error selecting configs: %s", err)
			continue
		}
		if configCount == 0 {
			continue
		}
		lastTimestamp = time.Now()

		// 1. get all configs
		configs, err := GetConfigs(context.Background(), r.db, r.layers)
		if err != nil {
			logger.Errorf("error getting configs: %s", err)
			continue
		}

		// 2. lock "dynamic" mutex
		func() {
			DynamicMx.Lock()
			defer DynamicMx.Unlock()
			err := ApplyLayers(context.Background(), r.treeCopy, configs, r.fixupFn)
			if err != nil {
				logger.Errorf("dynamic config failed to ApplyLayers: %s", err)
				return
			}
		}()
		time.Sleep(30 * time.Second)
	}
}
