package config

import (
	"context"
	"reflect"

	"github.com/BurntSushi/toml"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// ForEachConfig loads every stored config layer into T and calls cb once per
// layer. T is usually a minimal struct containing only the fields the caller
// needs; BurntSushi TOML ignores unmatched TOML keys and leaves fields absent
// from a layer at their zero value, so callbacks must handle zero values or
// filter by layer name when only some layers are relevant.
func ForEachConfig[T any](ctx context.Context, db *harmonydb.DB, cb func(name string, v T) error) error {
	confs, err := loadConfigs(ctx, db)
	if err != nil {
		return err
	}

	return forEachConfig(confs, cb)
}

// forEachConfig decodes config TOML values from a title-indexed map.
func forEachConfig[T any](confs map[string]string, cb func(name string, v T) error) error {
	for name, tomlStr := range confs {
		var info T

		if err := prepareCurioAddressesForDecode(tomlStr, &info); err != nil {
			return xerrors.Errorf("preparing CurioAddresses for layer %s: %w", name, err)
		}

		if err := toml.Unmarshal([]byte(tomlStr), &info); err != nil {
			return xerrors.Errorf("unmarshaling %s config: %w", name, err)
		}

		if err := cb(name, info); err != nil {
			return xerrors.Errorf("cb: %w", err)
		}
	}

	return nil
}

func prepareCurioAddressesForDecode[T any](tomlStr string, info *T) error {
	// Minimal config structs may include an Addresses []CurioAddresses field.
	// TOML decoding creates new slice elements as zero-value structs, but
	// CurioAddresses contains types.FIL values through BalanceManager that
	// need default initialization. Pre-size and default-fill that slice so
	// decoding writes into initialized CurioAddresses values.
	addressesField := findCurioAddressesField(info)
	if !addressesField.IsValid() {
		return nil
	}

	var lengthDetector struct {
		Addresses []struct{} `toml:"Addresses"`
	}
	_, err := toml.Decode(tomlStr, &lengthDetector)
	if err != nil {
		return xerrors.Errorf("detecting Addresses length: %w", err)
	}

	if addressesField.Len() >= len(lengthDetector.Addresses) {
		return nil
	}

	preAllocated := make([]CurioAddresses, len(lengthDetector.Addresses))
	for i := range preAllocated {
		preAllocated[i] = CurioAddresses{
			PreCommitControl:      []string{},
			CommitControl:         []string{},
			DealPublishControl:    []string{},
			TerminateControl:      []string{},
			DisableOwnerFallback:  false,
			DisableWorkerFallback: false,
			MinerAddresses:        []string{},
			BalanceManager:        DefaultBalanceManager(),
		}
	}
	addressesField.Set(reflect.ValueOf(preAllocated))
	return nil
}

func findCurioAddressesField[T any](info *T) reflect.Value {
	v := reflect.ValueOf(info).Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Type.Kind() == reflect.Slice && field.Type.Elem() == reflect.TypeFor[CurioAddresses]() {
			return v.Field(i)
		}
	}

	return reflect.Value{}
}

// loadConfigs returns a map of all stored config layers keyed by layer title.
func loadConfigs(ctx context.Context, db *harmonydb.DB) (map[string]string, error) {
	rows, err := db.Query(ctx, `SELECT title, config FROM harmony_config`)
	if err != nil {
		return nil, xerrors.Errorf("getting db configs: %w", err)
	}

	defer rows.Close()

	configs := make(map[string]string)
	for rows.Next() {
		var title, config string
		if err := rows.Scan(&title, &config); err != nil {
			return nil, xerrors.Errorf("scanning db configs: %w", err)
		}
		configs[title] = config
	}

	return configs, nil
}
