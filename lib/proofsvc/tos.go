// SPDX-License-Identifier: CCL-1.0

package proofsvc

import (
	_ "embed"
)

//go:embed tos/provider.md
var providerTos string

//go:embed tos/client.md
var clientTos string

//go:embed tos/privacy.md
var privacyTos string

type Tos struct {
	Provider string `json:"provider"`
	Client   string `json:"client"`
}

func GetTos() Tos {
	// append privacy tos to provider and client tos
	providerTos := providerTos + "\n\n" + privacyTos
	clientTos := clientTos + "\n\n" + privacyTos

	return Tos{
		Provider: providerTos,
		Client:   clientTos,
	}
}
