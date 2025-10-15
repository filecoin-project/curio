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

	DisableOwnerFallback  *config.Dynamic[bool]
	DisableWorkerFallback *config.Dynamic[bool]
}

func AddressSelector(addrConf []config.CurioAddresses) func() (*MultiAddressSelector, error) {
	return func() (*MultiAddressSelector, error) {
		as := &MultiAddressSelector{
			MinerMap: make(map[address.Address]AddressConfig),
		}
		if addrConf == nil {
			return as, nil
		}

		for _, addrConf := range addrConf {
			forMinerID := func() error {
				for _, minerID := range addrConf.MinerAddresses.Get() {
					tmp := AddressConfig{
						DisableOwnerFallback:  addrConf.DisableOwnerFallback,
						DisableWorkerFallback: addrConf.DisableWorkerFallback,
					}

					fixPCC := func() error {
						tmp.PreCommitControl = []address.Address{}
						for _, s := range addrConf.PreCommitControl.Get() {
							addr, err := address.NewFromString(s)
							if err != nil {
								return xerrors.Errorf("parsing precommit control address: %w", err)
							}

							tmp.PreCommitControl = append(tmp.PreCommitControl, addr)
						}
						return nil
					}
					if err := fixPCC(); err != nil {
						return err
					}
					addrConf.PreCommitControl.OnChange(func() {
						as.mmLock.Lock()
						defer as.mmLock.Unlock()
						if err := fixPCC(); err != nil {
							log.Errorf("error fixing precommit control: %s", err)
						}
					})

					for _, s := range addrConf.CommitControl.Get() {
						addr, err := address.NewFromString(s)
						if err != nil {
							return xerrors.Errorf("parsing commit control address: %w", err)
						}

						tmp.CommitControl = append(tmp.CommitControl, addr)
					}

					for _, s := range addrConf.DealPublishControl.Get() {
						addr, err := address.NewFromString(s)
						if err != nil {
							return xerrors.Errorf("parsing deal publish control address: %w", err)
						}

						tmp.DealPublishControl = append(tmp.DealPublishControl, addr)
					}

					for _, s := range addrConf.TerminateControl.Get() {
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
				return nil
			}
			if err := forMinerID(); err != nil {
				return nil, err
			}
			addrConf.MinerAddresses.OnChange(func() {
				if err := forMinerID(); err != nil {
					log.Errorf("error forMinerID: %s", err)
				}
			})
		}
		return as, nil
	}
}
