package repo

import "github.com/filecoin-project/lotus/node/repo"

type curio struct{}

var Curio curio

func (curio) Type() string {
	return "Curio"
}

func (curio) Config() interface{} {
	return &struct{}{}
}

func (curio) APIFlags() []string {
	return []string{"curio-api-url"}
}

func (curio) RepoFlags() []string {
	return []string{"curio-repo"}
}

func (curio) APIInfoEnvVars() (primary string, fallbacks []string, deprecated []string) {
	return "CURIO_API_INFO", nil, nil
}

var _ repo.RepoType = curio{}
