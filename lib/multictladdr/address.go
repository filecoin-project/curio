package multictladdr

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps/config"
)

type AddressConfig struct {
	PreCommitControl   []address.Address
	CommitControl      []address.Address
	TerminateControl   []address.Address
	DealPublishControl []address.Address

	DisableOwnerFallback  bool
	DisableWorkerFallback bool
}

func AddressSelector(addrConf *config.Dynamic[[]config.CurioAddresses]) func() (*MultiAddressSelector, error) {
	return func() (*MultiAddressSelector, error) {
		as := &MultiAddressSelector{
			MinerMap: make(map[address.Address]AddressConfig),
		}
		makeMinerMap := func() error {
			as.mmLock.Lock()
			defer as.mmLock.Unlock()
			if addrConf == nil {
				return nil
			}

			for _, addrConf := range addrConf.Get() {
				for _, minerID := range addrConf.MinerAddresses {
					tmp := AddressConfig{
						DisableOwnerFallback:  addrConf.DisableOwnerFallback,
						DisableWorkerFallback: addrConf.DisableWorkerFallback,
					}

					for _, s := range addrConf.PreCommitControl {
						addr, err := address.NewFromString(s)
						if err != nil {
							return xerrors.Errorf("parsing precommit control address: %w", err)
						}

						tmp.PreCommitControl = append(tmp.PreCommitControl, addr)
					}

					for _, s := range addrConf.CommitControl {
						addr, err := address.NewFromString(s)
						if err != nil {
							return xerrors.Errorf("parsing commit control address: %w", err)
						}

						tmp.CommitControl = append(tmp.CommitControl, addr)
					}

					for _, s := range addrConf.DealPublishControl {
						addr, err := address.NewFromString(s)
						if err != nil {
							return xerrors.Errorf("parsing deal publish control address: %w", err)
						}

						tmp.DealPublishControl = append(tmp.DealPublishControl, addr)
					}

					for _, s := range addrConf.TerminateControl {
						addr, err := address.NewFromString(s)
						if err != nil {
							return xerrors.Errorf("parsing terminate control address: %w", err)
						}

						tmp.TerminateControl = append(tmp.TerminateControl, addr)
					}
					a, err := address.NewFromString(minerID)
					if err != nil {
						return xerrors.Errorf("parsing miner address %s: %w", minerID, err)
					}
					as.MinerMap[a] = tmp
				}
			}
			return nil
		}
		err := makeMinerMap()
		if err != nil {
			return nil, err
		}
		addrConf.OnChange(func() {
			err := makeMinerMap()
			if err != nil {
				log.Errorf("error making miner map: %s", err)
			}
		})
		return as, nil
	}
}
