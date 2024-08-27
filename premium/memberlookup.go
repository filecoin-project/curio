package premium

import "github.com/filecoin-project/lotus/chain/wallet"

type Premium struct {
}

func New() *Premium {
	return &Premium{}
}

type Memberships struct {
	MinerIDs []string
	Level    int
	URL      string
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

func (p *Premium) AddMembership(level int, wallet string) (bool, error) {
	// TODO: Implement
	return false, nil
}

func (p *Premium) PayMembershipDues(wallet *wallet.MultiWallet) (bool, error) {
	// TODO: Implement
	return false, nil
}
