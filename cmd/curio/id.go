package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/reqcontext"

	"github.com/filecoin-project/lotus/chain/types"
)

type idOutput struct {
	PeerID      string       `json:"peer_id"`
	LocalListen string       `json:"local_listen,omitempty"`
	RunningOn   string       `json:"running_on,omitempty"`
	UpdatedAt   *time.Time   `json:"updated_at,omitempty"`
	Miners      []minerState `json:"miners,omitempty"`
}

type minerState struct {
	Address       string   `json:"address"`
	OnChainPeerID string   `json:"on_chain_peer_id,omitempty"`
	OnChainAddrs  []string `json:"on_chain_addrs,omitempty"`
	Synced        bool     `json:"synced"`
	Error         string   `json:"error,omitempty"`
}

var idCmd = &cli.Command{
	Name:  "id",
	Usage: "print libp2p identity and compare with on-chain state",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "json",
			Usage: "output in json format",
		},
		&cli.BoolFlag{
			Name:  "check",
			Usage: "exit with code 1 if local and on-chain peer IDs mismatch",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		out, err := getIdentity(ctx, dep)
		if err != nil {
			return err
		}

		if cctx.Bool("json") {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		// text output
		fmt.Printf("PeerID:       %s\n", out.PeerID)
		if out.LocalListen != "" {
			fmt.Printf("LocalListen:  %s\n", out.LocalListen)
		}
		if out.RunningOn != "" {
			fmt.Printf("RunningOn:    %s\n", out.RunningOn)
		}
		if out.UpdatedAt != nil {
			fmt.Printf("UpdatedAt:    %s\n", out.UpdatedAt.Format(time.RFC3339))
		}

		if len(out.Miners) > 0 {
			fmt.Println("\nMiners:")
			for _, m := range out.Miners {
				status := "✓ synced"
				if !m.Synced {
					status = "✗ mismatch"
				}
				if m.Error != "" {
					status = "? " + m.Error
				}
				fmt.Printf("  %s:\n", m.Address)
				fmt.Printf("    on-chain peer:  %s\n", m.OnChainPeerID)
				if len(m.OnChainAddrs) > 0 {
					fmt.Printf("    on-chain addrs: %v\n", m.OnChainAddrs)
				}
				fmt.Printf("    status:         %s\n", status)
			}
		}

		if cctx.Bool("check") {
			for _, m := range out.Miners {
				if m.Error != "" || !m.Synced {
					return cli.Exit("peer ID mismatch detected for "+m.Address, 1)
				}
			}
			for _, m := range out.Miners {
				if m.Error != "" {
					return cli.Exit("miner error detected: "+m.Error+" for "+m.Address, 1)
				}
			}
		}

		return nil
	},
}

func getIdentity(ctx context.Context, dep *deps.Deps) (*idOutput, error) {
	var peerID string
	var localListen sql.NullString
	var runningOn sql.NullString
	var updatedAt sql.NullTime

	err := dep.DB.QueryRow(ctx, `SELECT peer_id, local_listen, running_on, updated_at FROM libp2p`).Scan(
		&peerID, &localListen, &runningOn, &updatedAt)
	if err != nil {
		return nil, xerrors.Errorf("querying libp2p table: %w", err)
	}

	out := &idOutput{
		PeerID: peerID,
	}
	if localListen.Valid {
		out.LocalListen = localListen.String
	}
	if runningOn.Valid {
		out.RunningOn = runningOn.String
	}
	if updatedAt.Valid {
		out.UpdatedAt = &updatedAt.Time
	}

	// get configured miners
	miners := dep.Maddrs.Get()
	for maddr := range miners {
		ms := minerState{
			Address: address.Address(maddr).String(),
		}

		mi, err := dep.Chain.StateMinerInfo(ctx, address.Address(maddr), types.EmptyTSK)
		if err != nil {
			ms.Error = err.Error()
			out.Miners = append(out.Miners, ms)
			continue
		}

		if mi.PeerId != nil {
			ms.OnChainPeerID = mi.PeerId.String()
		}
		for _, addr := range mi.Multiaddrs {
			ms.OnChainAddrs = append(ms.OnChainAddrs, string(addr))
		}

		ms.Synced = ms.OnChainPeerID == peerID
		out.Miners = append(out.Miners, ms)
	}

	return out, nil
}
