package premium

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/exp/maps"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/go-address"
)

var log = logging.Logger("curio/premium")

type Premium struct {
	deps *deps.Deps
}

func New(deps *deps.Deps) *Premium {
	return &Premium{
		deps: deps,
	}
}

type Memberships struct {
	MinerIDs []string
	Level    int
	URL      string
}

func GetCurrentID(cfg *config.CurioConfig) string {
	minerIDs := map[string]bool{}
	prefix := "pCu"
	for _, cfg := range cfg.Addresses {
		for _, miner := range cfg.MinerAddresses {
			address, err := address.NewFromString(miner)
			if err != nil {
				log.Errorw("error parsing miner address", "miner", miner)
				continue
			}
			id := strings.ToLower(address.String())
			fmt.Println(id)
			if strings.HasPrefix(id, "t") {
				prefix = "tCu"
			}
			minerIDs[id] = true
		}
	}
	s := maps.Keys(minerIDs)
	sort.Strings(s)
	h := sha256.Sum256([]byte(strings.Join(s, ",")))

	return prefix + base64.StdEncoding.EncodeToString(h[:18])
}

func (p *Premium) MemberLookup() []Memberships {
	var memberships []Memberships
	//TODO: Implement
	return memberships
}

func (p *Premium) ListMembershipsAvailable() map[int]string {
	var memberships = map[int]string{}
	// TODO: Implement
	return memberships
}
