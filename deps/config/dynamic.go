package config

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	logging "github.com/ipfs/go-log/v2"
)

var logger = logging.Logger("config-dynamic")
var DynamicMx sync.RWMutex

type Dynamic[T any] struct {
	Value T
}

func NewDynamic[T any](value T) *Dynamic[T] {
	return &Dynamic[T]{Value: value}
}

func (d *Dynamic[T]) Set(value T) {
	DynamicMx.Lock()
	defer DynamicMx.Unlock()
	d.Value = value
}

func (d *Dynamic[T]) Get() T {
	DynamicMx.RLock()
	defer DynamicMx.RUnlock()
	return d.Value
}

func (d *Dynamic[T]) UnmarshalText(text []byte) error {
	DynamicMx.Lock()
	defer DynamicMx.Unlock()
	return toml.Unmarshal(text, d.Value)
}

type cfgRoot struct {
	db       *harmonydb.DB
	layers   []string
	treeCopy *CurioConfig
}

func EnableChangeDetection(db *harmonydb.DB, obj *CurioConfig, layers []string) error {
	r := &cfgRoot{db: db, treeCopy: obj, layers: layers}
	err := r.copyWithOriginalDynamics(obj)
	if err != nil {
		return err
	}
	go r.changeMonitor()
	return nil
}

// copyWithOriginalDynamics copies the original dynamics from the original object to the new object.
func (r *cfgRoot) copyWithOriginalDynamics(orig *CurioConfig) error {
	typ := reflect.TypeOf(orig)
	if typ.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %s", typ.Kind())
	}
	result := reflect.New(typ)
	// recursively walk the struct tree, and copy the dynamics from the original object to the new object.
	var walker func(orig, result reflect.Value)
	walker = func(orig, result reflect.Value) {
		for i := 0; i < orig.NumField(); i++ {
			field := orig.Field(i)
			if field.Kind() == reflect.Struct {
				walker(field, result.Field(i))
			} else if field.Kind() == reflect.Ptr {
				walker(field.Elem(), result.Field(i).Elem())
			} else if field.Kind() == reflect.Interface {
				walker(field.Elem(), result.Field(i).Elem())
			} else {
				result.Field(i).Set(field)
			}
		}
	}
	walker(reflect.ValueOf(orig), result)
	r.treeCopy = result.Interface().(*CurioConfig)
	return nil
}

func (r *cfgRoot) changeMonitor() {
	lastTimestamp := time.Now().Add(-30 * time.Second) // plenty of time for start-up

	for {
		time.Sleep(30 * time.Second)
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
			ApplyLayers(context.Background(), r.treeCopy, configs)
		}()
		DynamicMx.Lock()
	}
}
