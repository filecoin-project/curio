package pdpnode

import (
	"context"
	"fmt"
	"reflect"

	"github.com/filecoin-project/curio/cuhttp/servicedeps"
	curiodeps "github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/piecestore"
	pdpwallet "github.com/filecoin-project/curio/pdp/wallet"
)

// Attach registers PDP tasks on a curio node.
func Attach(
	ctx context.Context,
	cd *curiodeps.Deps,
	activeTasks *[]harmonytask.TaskInterface,
	sdeps *servicedeps.Deps,
	chainSched *chainsched.CurioChainSched,
) error {
	if cd.Cfg.Subsystems.EnablePDP {
		hasKey, err := pdpwallet.HasPDPKey(ctx, cd.DB)
		if err != nil {
			log.Warnf("checking PDP wallet: %s", err)
		} else if !hasKey && cd.Alert != nil {
			log.Warn("PDP signing key not configured")
			cd.Alert.AddAlert("PDP wallet not configured. Create or assign a key on the PDP page.")
		}
	}

	d := FromCurio(cd)
	sd, err := AppendTasks(ctx, d, chainSched, activeTasks)
	if err != nil {
		return err
	}
	if sd.EthSender != nil {
		sdeps.EthSender = sd.EthSender
	}
	if sd.AlertTask != nil {
		sdeps.AlertTask = sd.AlertTask
	}
	return nil
}

// FromCurio maps curio runtime deps into pdpnode deps.
//
// Fields shared by name and identical type are copied automatically via
// syncMatchingDeps. The three named exceptions are handled below.
func FromCurio(cd *curiodeps.Deps) *Deps {
	d := &Deps{}
	syncMatchingDeps(d, cd, "PieceIO", "MachineHost", "MachineID")

	// PieceIO: not stored on deps.Deps; constructed from its components.
	d.PieceIO = piecestore.New(cd.Stor, cd.LocalStore, cd.Si)
	// MachineHost: aliased as ListenAddr in deps.Deps.
	d.MachineHost = cd.ListenAddr
	// MachineID: int64 here (always set after task engine starts) vs
	// *int64 in deps.Deps (pointer allowing delayed / optional assignment).
	if cd.MachineID != nil {
		d.MachineID = *cd.MachineID
	}
	return d
}

// CurioDeps builds a curio deps struct for shared web/cuhttp integrations.
//
// Fields shared by name and identical type are copied automatically via
// syncMatchingDeps. The same three exceptions are handled below.
func (d *Deps) CurioDeps() *curiodeps.Deps {
	cd := &curiodeps.Deps{}
	syncMatchingDeps(cd, d, "PieceIO", "MachineHost", "MachineID")
	cd.ListenAddr = d.MachineHost
	cd.MachineID = &d.MachineID
	return cd
}

// syncMatchingDeps copies all exported fields from src into dst where both
// structs share the same field name with an identical reflected type. Fields
// listed in skip are excluded from the copy (use for intentional name or type
// mismatches). If a non-skipped field exists in both structs with the same
// name but different types, syncMatchingDeps panics — that indicates
// accidental drift between deps.Deps and pdpnode.Deps.
func syncMatchingDeps(dst, src any, skip ...string) {
	skipSet := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipSet[s] = true
	}

	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src)
	if sv.Kind() == reflect.Ptr {
		sv = sv.Elem()
	}
	st := sv.Type()
	dt := dv.Type()

	for i := range dt.NumField() {
		df := dt.Field(i)
		if !df.IsExported() || skipSet[df.Name] {
			continue
		}
		sf, ok := st.FieldByName(df.Name)
		if !ok {
			continue // field exists only in dst; caller handles it
		}
		if sf.Type != df.Type {
			panic(fmt.Sprintf(
				"syncMatchingDeps: field %s has type %v in %T but %v in %T — add to skip list or align the types",
				df.Name, df.Type, dst, sf.Type, src,
			))
		}
		dv.Field(i).Set(sv.FieldByName(df.Name))
	}
}
