package libp2p

import (
	"context"
	"fmt"
	"strings"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/core/peer"
	basichost "github.com/libp2p/go-libp2p/p2p/host/basic"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoremem"
	"github.com/multiformats/go-multiaddr"
	mamask "github.com/whyrusleeping/multiaddr-filter"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var log = logging.Logger("curio-libp2p")

func NewLibp2pHost(ctx context.Context, db *harmonydb.DB, cfg *config.CurioConfig) ([]host.Host, error) {
	lcfg, err := getCfg(ctx, db, cfg.Market.StorageMarketConfig.MK12.Miners)
	if err != nil {
		return nil, err
	}

	var ret []host.Host

	for miner, c := range lcfg {
		pstore, err := pstoremem.NewPeerstore()
		if err != nil {
			return nil, fmt.Errorf("creating peer store: %w", err)
		}

		pubK := c.priv.GetPublic()
		id, err := peer.IDFromPublicKey(pubK)
		if err != nil {
			return nil, fmt.Errorf("getting peer ID: %w", err)
		}

		err = pstore.AddPrivKey(id, c.priv)
		if err != nil {
			return nil, fmt.Errorf("adding private key to peerstore: %w", err)
		}
		err = pstore.AddPubKey(id, pubK)
		if err != nil {
			return nil, fmt.Errorf("adding public key to peerstore: %w", err)
		}

		addrFactory, err := MakeAddrsFactory(c.AnnounceAddr, c.NoAnnounceAddr)
		if err != nil {
			return nil, fmt.Errorf("creating address factory: %w", err)
		}

		opts := []libp2p.Option{
			libp2p.DefaultTransports,
			libp2p.ListenAddrs(c.ListenAddr...),
			libp2p.AddrsFactory(addrFactory),
			libp2p.Peerstore(pstore),
			libp2p.UserAgent("curio-" + build.UserVersion()),
			libp2p.Ping(true),
			libp2p.DisableRelay(),
			libp2p.EnableNATService(),
			libp2p.BandwidthReporter(metrics.NewBandwidthCounter()),
		}

		h, err := libp2p.New(opts...)
		if err != nil {
			return nil, xerrors.Errorf("creating libp2p host: %w", err)
		}

		// Start listening
		err = h.Network().Listen(c.ListenAddr...)
		if err != nil {
			return nil, xerrors.Errorf("failed to listen on addresses: %w", err)
		}

		log.Infof("Libp2p started listening for miner %s", miner.String())
		ret = append(ret, h)
	}

	return ret, err

}

type libp2pCfg struct {
	priv           crypto.PrivKey
	ListenAddr     []multiaddr.Multiaddr
	AnnounceAddr   []multiaddr.Multiaddr
	NoAnnounceAddr []multiaddr.Multiaddr
}

func getCfg(ctx context.Context, db *harmonydb.DB, miners []string) (map[address.Address]*libp2pCfg, error) {
	mm := make(map[int64]address.Address)
	var ms []int64

	for _, miner := range miners {
		maddr, err := address.NewFromString(miner)
		if err != nil {
			return nil, err
		}
		mid, err := address.IDFromAddress(maddr)
		if err != nil {
			return nil, err
		}
		mm[int64(mid)] = maddr
		ms = append(ms, int64(mid))
	}

	var cfgs []struct {
		SpID           int64  `db:"sp_id"`
		Key            []byte `db:"priv_key"`
		ListenAddr     string `db:"listen_address"`
		AnnounceAddr   string `db:"announce_address"`
		NoAnnounceAddr string `db:"no_announce_address"`
	}
	err := db.Select(ctx, &cfgs, `SELECT sp_id, priv_key, listen_address, announce_address FROM libp2p_keys WHERE sp_id = ANY($1)`, ms)
	if err != nil {
		return nil, xerrors.Errorf("getting libp2p details from DB: %w", err)
	}

	if len(cfgs) != len(miners) {
		return nil, fmt.Errorf("mismatched number of miners and libp2p configurations")
	}

	ret := make(map[address.Address]*libp2pCfg)

	for _, cfg := range cfgs {
		p, err := crypto.UnmarshalPrivateKey(cfg.Key)
		if err != nil {
			return nil, xerrors.Errorf("unmarshaling private key: %w", err)
		}

		ret[mm[cfg.SpID]] = &libp2pCfg{
			priv: p,
		}

		la := strings.Split(cfg.ListenAddr, ",")
		for _, l := range la {
			listenAddr, err := multiaddr.NewMultiaddr(l)
			if err != nil {
				return nil, xerrors.Errorf("parsing listen address: %w", err)
			}
			ret[mm[cfg.SpID]].ListenAddr = append(ret[mm[cfg.SpID]].ListenAddr, listenAddr)
		}

		aa := strings.Split(cfg.AnnounceAddr, ",")
		for _, a := range aa {
			announceAddr, err := multiaddr.NewMultiaddr(a)
			if err != nil {
				return nil, xerrors.Errorf("parsing announce address: %w", err)
			}
			ret[mm[cfg.SpID]].AnnounceAddr = append(ret[mm[cfg.SpID]].AnnounceAddr, announceAddr)
		}

		naa := strings.Split(cfg.NoAnnounceAddr, ",")
		for _, na := range naa {
			noAnnounceAddr, err := multiaddr.NewMultiaddr(na)
			if err != nil {
				return nil, xerrors.Errorf("parsing no announce address: %w", err)
			}
			ret[mm[cfg.SpID]].NoAnnounceAddr = append(ret[mm[cfg.SpID]].NoAnnounceAddr, noAnnounceAddr)
		}
	}

	return ret, nil
}

func MakeAddrsFactory(announceAddrs, noAnnounce []multiaddr.Multiaddr) (basichost.AddrsFactory, error) {
	filters := multiaddr.NewFilters()
	noAnnAddrs := map[string]bool{}
	for _, addr := range noAnnounce {
		f, err := mamask.NewMask(addr.String())
		if err == nil {
			filters.AddFilter(*f, multiaddr.ActionDeny)
			continue
		}
		noAnnAddrs[string(addr.Bytes())] = true
	}

	return func(allAddrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
		var addrs []multiaddr.Multiaddr
		if len(announceAddrs) > 0 {
			addrs = announceAddrs
		} else {
			addrs = allAddrs
		}

		var out []multiaddr.Multiaddr
		for _, maddr := range addrs {
			// check for exact matches
			ok := noAnnAddrs[string(maddr.Bytes())]
			// check for /ipcidr matches
			if !ok && !filters.AddrBlocked(maddr) {
				out = append(out, maddr)
			}
		}
		return out
	}, nil
}
