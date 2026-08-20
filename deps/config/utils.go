package config

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
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

		sanitized, err := StripEmptyDynamicTables(tomlStr, DefaultCurioConfig())
		if err != nil {
			return xerrors.Errorf("sanitizing layer %s: %w", name, err)
		}

		if err := prepareCurioAddressesForDecode(sanitized, &info); err != nil {
			return xerrors.Errorf("preparing CurioAddresses for layer %s: %w", name, err)
		}

		if err := toml.Unmarshal([]byte(sanitized), &info); err != nil {
			return xerrors.Errorf("unmarshaling %s config: %w", name, err)
		}

		if err := cb(name, info); err != nil {
			return xerrors.Errorf("cb: %w", err)
		}
	}

	return nil
}

// RepairStoredEmptyDynamicTables finds layers whose TOML still contains empty
// Dynamic[T] wrapper tables (from older GUI/raw encodes), removes those tables
// while preserving comments, and writes the repaired text back. Prior text is
// snapshotted to harmony_config_history. Returns the titles that were updated.
func RepairStoredEmptyDynamicTables(ctx context.Context, db *harmonydb.DB) ([]string, error) {
	if db == nil || db.ReadOnly() {
		return nil, nil
	}

	confs, err := loadConfigs(ctx, db)
	if err != nil {
		return nil, err
	}

	sample := DefaultCurioConfig()
	var repaired []string
	for title, text := range confs {
		fixed, changed, err := RepairEmptyDynamicTablesText(text, sample)
		if err != nil {
			return repaired, xerrors.Errorf("repairing layer %s: %w", title, err)
		}
		if !changed {
			continue
		}

		probe := DefaultCurioConfig()
		if _, err := LoadConfigWithUpgrades(fixed, probe); err != nil {
			return repaired, xerrors.Errorf("repaired layer %s still unloadable: %w", title, err)
		}

		_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			if _, err := tx.Exec(`INSERT INTO harmony_config_history (title, config) VALUES ($1, $2)`, title, text); err != nil {
				return false, xerrors.Errorf("history snapshot for %s: %w", title, err)
			}
			if _, err := tx.Exec(`UPDATE harmony_config SET config=$1 WHERE title=$2`, fixed, title); err != nil {
				return false, xerrors.Errorf("updating layer %s: %w", title, err)
			}
			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return repaired, err
		}

		repaired = append(repaired, title)
		logger.Infof("repaired empty Dynamic wrapper tables in config layer %q", title)
	}

	return repaired, nil
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

type minerLookUpApi interface {
	StateMinerInfo(ctx context.Context, actor address.Address, tsk types.TipSetKey) (api.MinerInfo, error)
}

func GetAddressesFromConfig(ctx context.Context, db *harmonydb.DB, api minerLookUpApi) ([]address.Address, []address.Address, error) {
	// MachineDetails represents the structure of data received from the SQL query.
	type machineDetail struct {
		Layers sql.NullString
	}
	var machineDetails []machineDetail

	// Get all layers in use
	err := db.Select(ctx, &machineDetails, `
				SELECT d.layers
				FROM harmony_machines m
				LEFT JOIN harmony_machine_details d ON m.id = d.machine_id;`)
	if err != nil {
		return nil, nil, xerrors.Errorf("getting config layers for all machines: %w", err)
	}

	// UniqueLayers takes an array of MachineDetails and returns a slice of unique layers.

	layerMap := make(map[string]bool)
	var uniqueLayers []string

	// Get unique layers in use
	for _, machine := range machineDetails {
		if !machine.Layers.Valid {
			continue
		}
		// Split the Layers field into individual layers
		layers := strings.SplitSeq(machine.Layers.String, ",")
		for layer := range layers {
			layer = strings.TrimSpace(layer)
			if _, exists := layerMap[layer]; !exists && layer != "" {
				layerMap[layer] = true
				uniqueLayers = append(uniqueLayers, layer)
			}
		}
	}

	addrMap := make(map[string]struct{})
	minerMap := make(map[string]struct{})

	if len(uniqueLayers) > 0 {
		type minimalAddressInfo struct {
			Addresses []CurioAddresses `toml:"Addresses"`
		}

		err = ForEachConfig[minimalAddressInfo](ctx, db, func(name string, info minimalAddressInfo) error {
			if !slices.Contains(uniqueLayers, name) {
				return nil
			}

			for i := range info.Addresses {
				prec := info.Addresses[i].PreCommitControl
				com := info.Addresses[i].CommitControl
				term := info.Addresses[i].TerminateControl
				miners := info.Addresses[i].MinerAddresses
				dpc := info.Addresses[i].DealPublishControl
				for j := range prec {
					if prec[j] != "" {
						addrMap[prec[j]] = struct{}{}
					}
				}
				for j := range com {
					if com[j] != "" {
						addrMap[com[j]] = struct{}{}
					}
				}
				for j := range term {
					if term[j] != "" {
						addrMap[term[j]] = struct{}{}
					}
				}
				for j := range miners {
					if miners[j] != "" {
						minerMap[miners[j]] = struct{}{}
					}
				}
				for j := range dpc {
					if dpc[j] != "" {
						addrMap[dpc[j]] = struct{}{}
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}

	var wallets, minerAddrs []address.Address

	// Get control and wallet addresses from chain
	for m := range minerMap {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return nil, nil, err
		}
		info, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return nil, nil, err
		}
		minerAddrs = append(minerAddrs, maddr)
		addrMap[info.Worker.String()] = struct{}{}
		for _, w := range info.ControlAddresses {
			if _, ok := addrMap[w.String()]; !ok {
				addrMap[w.String()] = struct{}{}
			}
		}
	}

	for w := range addrMap {
		waddr, err := address.NewFromString(w)
		if err != nil {
			return nil, nil, err
		}
		wallets = append(wallets, waddr)
	}

	return wallets, minerAddrs, nil
}
