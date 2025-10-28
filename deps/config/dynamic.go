package config

import (
	"context"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var logger = logging.Logger("config-dynamic")

// BigIntComparer is used to compare big.Int values properly
var BigIntComparer = cmp.Comparer(func(x, y big.Int) bool {
	return x.Cmp(&y) == 0
})

// Dynamic is a wrapper for configuration values that can change at runtime.
// Use Get() and Set() methods to access the value with proper synchronization
// and change detection.
type Dynamic[T any] struct {
	value T
}

func NewDynamic[T any](value T) *Dynamic[T] {
	d := &Dynamic[T]{value: value}
	dynamicLocker.notifier[reflect.ValueOf(d).Pointer()] = nil
	return d
}

// OnChange registers a function to be called in a goroutine when the dynamic value changes to a new final-layered value.
// The function is called in a goroutine to avoid blocking the main thread; it should not panic.
func (d *Dynamic[T]) OnChange(fn func()) {
	p := reflect.ValueOf(d).Pointer()
	prev := dynamicLocker.notifier[p]
	if prev == nil {
		dynamicLocker.notifier[p] = fn
		return
	}
	dynamicLocker.notifier[p] = func() {
		prev()
		fn()
	}
}

// It locks dynamicLocker, unless we're already in an updating context.
func (d *Dynamic[T]) Set(value T) {
	// Check if we're already in an updating context (changeMonitor)
	dynamicLocker.RLock()
	updating := dynamicLocker.updating
	dynamicLocker.RUnlock()

	if updating {
		// We're in changeMonitor context, don't acquire the lock to avoid deadlock
		dynamicLocker.inform(reflect.ValueOf(d).Pointer(), d.value, value)
		d.value = value
	} else {
		// Normal case - acquire the lock
		dynamicLocker.Lock()
		defer dynamicLocker.Unlock()
		dynamicLocker.inform(reflect.ValueOf(d).Pointer(), d.value, value)
		d.value = value
	}
}

// It rlocks dynamicLocker.
func (d *Dynamic[T]) Get() T {
	if d == nil {
		var zero T
		return zero
	}
	dynamicLocker.RLock()
	defer dynamicLocker.RUnlock()
	return d.value
}

func (d *Dynamic[T]) GetWithoutLock() T {
	if d == nil {
		var zero T
		return zero
	}
	return d.value
}

// Equal is used by cmp.Equal for custom comparison.
// It doesn't lock dynamicLocker as this typically is used for an update test.
func (d *Dynamic[T]) Equal(other *Dynamic[T]) bool {
	return cmp.Equal(d.GetWithoutLock(), other.GetWithoutLock(), BigIntComparer, cmpopts.EquateEmpty())
}

// MarshalTOML cannot be implemented for struct types because it won't be boxed correctly.

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

		// Skip unexported fields - they can't be set via reflection
		if !resultField.CanSet() {
			continue
		}

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

		// 2. Apply layers and detect changes without deadlock
		func() {
			// Set a flag to indicate we're in change monitor context
			dynamicLocker.Lock()
			dynamicLocker.updating = true
			dynamicLocker.Unlock()

			// Apply layers - Dynamic.Set() will detect the updating flag and skip locking
			err := ApplyLayers(context.Background(), r.treeCopy, configs, r.fixupFn)
			if err != nil {
				logger.Errorf("dynamic config failed to ApplyLayers: %s", err)
				// Reset updating flag on error
				dynamicLocker.Lock()
				dynamicLocker.updating = false
				dynamicLocker.Unlock()
				return
			}

			// Process change notifications
			dynamicLocker.Lock()
			dynamicLocker.updating = false
			for k, v := range dynamicLocker.latest {
				if !cmp.Equal(v, dynamicLocker.originally[k], BigIntComparer, cmp.Reporter(&reportHandler{})) {
					if notifier := dynamicLocker.notifier[k]; notifier != nil {
						go notifier()
					}
				}
			}
			dynamicLocker.originally = make(map[uintptr]any)
			dynamicLocker.latest = make(map[uintptr]any)
			dynamicLocker.Unlock()
		}()
		time.Sleep(30 * time.Second)
	}
}

var dynamicLocker = changeNotifier{diff: diff{
	originally: make(map[uintptr]any),
	latest:     make(map[uintptr]any),
},
	notifier: make(map[uintptr]func()),
}

type changeNotifier struct {
	sync.RWMutex      // this protects the dynamic[T] reads from getting a race with the updating
	updating     bool // determines which mode we are in: updating or querying

	diff

	notifier map[uintptr]func()
}
type diff struct {
	cdmx       sync.Mutex //
	originally map[uintptr]any
	latest     map[uintptr]any
}

func (c *changeNotifier) Lock() {
	c.RWMutex.Lock()
	c.updating = true
}
func (c *changeNotifier) Unlock() {
	c.RWMutex.Unlock()
	c.cdmx.Lock()
	defer c.cdmx.Unlock()

	c.updating = false
	for k, v := range c.latest {
		if !cmp.Equal(v, c.originally[k], BigIntComparer, cmp.Reporter(&reportHandler{})) {
			if notifier := c.notifier[k]; notifier != nil {
				go notifier()
			}
		}
	}
	c.originally = make(map[uintptr]any)
	c.latest = make(map[uintptr]any)
}

type reportHandler struct {
	changes  []string
	newValue any
	oldValue any
}

func (r *reportHandler) PushStep(path cmp.PathStep) {
	r.changes = append(r.changes, path.String())
	r.newValue, r.oldValue = path.Values()
}
func (r *reportHandler) Report(result cmp.Result) {
	if !result.Equal() {
		logger.Infof("Dynamic configuration %s updated from %v to %v", strings.Join(r.changes, "."), r.oldValue, r.newValue)
	}
}
func (r *reportHandler) PopStep() {
	r.changes = r.changes[:len(r.changes)-1]
}
func (c *changeNotifier) inform(ptr uintptr, oldValue any, newValue any) {
	if !c.updating {
		return
	}
	c.cdmx.Lock()
	defer c.cdmx.Unlock()
	if _, ok := c.originally[ptr]; !ok {
		c.originally[ptr] = oldValue
	}
	c.latest[ptr] = newValue
}
