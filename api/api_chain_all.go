//go:build !forest
// +build !forest

package api

import "github.com/filecoin-project/lotus/api"

type Chain = api.FullNode
type ChainStruct = api.FullNodeStruct
