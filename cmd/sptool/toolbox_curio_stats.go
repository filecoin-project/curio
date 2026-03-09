package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/ipni/go-libipni/maurl"
	"github.com/multiformats/go-multiaddr"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var statsCmd = &cli.Command{
	Name:  "stats",
	Usage: "Curio Node Stats",
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return xerrors.Errorf("cant setup gateway connection: %w", err)
		}
		defer closer()

		ctx := cctx.Context

		allMiners, err := api.StateListMiners(ctx, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("listing miners: %w", err)
		}

		fmt.Println("Total miners:", len(allMiners))

		miners := make(map[address.Address]multiaddr.Multiaddr)

		for _, miner := range allMiners {
			info, err := api.StateMinerInfo(ctx, miner, types.EmptyTSK)
			if err != nil {
				continue // Api tries to decode ba stuff and results in error
			}
			if info.PeerId == nil {
				continue
			}
			if info.Multiaddrs == nil {
				continue
			}
			if len(info.Multiaddrs) == 0 {
				continue
			}
			for _, m := range info.Multiaddrs {
				ad, err := multiaddr.NewMultiaddrBytes(m)
				if err != nil {
					continue // bad addresses should simply be skipped
				}
				s := strings.Split(ad.String(), "/")
				if s[len(s)-1] == "wss" {
					if s[1] == "dns" {
						miners[miner] = ad
						break
					}
				}
			}
		}

		fmt.Println("Miners with Curio style multiAddress:", len(miners))
		for miner, ad := range miners {
			fmt.Println("Miner:Address", miner, ad.String())
		}

		var finalMiners []address.Address
		failedMiners := make(map[address.Address]error)

		for miner, addr := range miners {
			hurl, err := maurl.ToURL(addr)
			if err != nil {
				return xerrors.Errorf("failed to convert multiaddr %s to URL: %w", addr, err)
			}
			if hurl.Scheme == "ws" {
				hurl.Scheme = "http"
			}
			if hurl.Scheme == "wss" {
				hurl.Scheme = "https"
			}
			resp, err := http.Get(hurl.String())
			if err != nil {
				failedMiners[miner] = err
				continue
			}
			defer func() {
				_ = resp.Body.Close()
			}()
			if resp.StatusCode != http.StatusOK {
				failedMiners[miner] = xerrors.Errorf("http status %s", resp.Status)
				continue
			}
			r, err := io.ReadAll(resp.Body)
			if err != nil {
				return xerrors.Errorf("reading response body: %w", err)
			}
			fmt.Println("Curio response for:", miner.String(), string(r))
			if strings.Contains(string(r), "-Curio") {
				finalMiners = append(finalMiners, miner)
			}
		}

		qap := abi.NewStoragePower(0)
		rb := abi.NewStoragePower(0)
		totalQap := abi.NewStoragePower(0)
		totalrb := abi.NewStoragePower(0)

		// Get power stats for reachable Curio miners
		for _, miner := range finalMiners {
			p, err := api.StateMinerPower(ctx, miner, types.EmptyTSK)
			if err != nil {
				continue
			}
			qap = big.Add(qap, p.MinerPower.QualityAdjPower)
			rb = big.Add(rb, p.MinerPower.RawBytePower)
			if totalQap.IsZero() {
				totalQap = p.TotalPower.QualityAdjPower
			}
			if totalrb.IsZero() {
				totalrb = p.TotalPower.RawBytePower
			}
		}

		perQap := types.BigDivFloat(types.BigMul(qap, big.NewInt(100)), totalQap)
		perRb := types.BigDivFloat(types.BigMul(rb, big.NewInt(100)), totalrb)

		failedRb := big.NewInt(0)
		failedQap := big.NewInt(0)

		for miner := range failedMiners {
			p, err := api.StateMinerPower(ctx, miner, types.EmptyTSK)
			if err != nil {
				continue
			}
			// Remove miners we cannot connect to and have 0 power (probably test creations)
			if p.MinerPower.RawBytePower.IsZero() {
				delete(failedMiners, miner)
			}
			failedQap = big.Add(failedQap, p.MinerPower.QualityAdjPower)
			failedRb = big.Add(failedRb, p.MinerPower.RawBytePower)
		}

		failedQaPPer := types.BigDivFloat(types.BigMul(failedQap, big.NewInt(100)), totalQap)
		failedRbPer := types.BigDivFloat(types.BigMul(failedRb, big.NewInt(100)), totalrb)

		minerStrs := make([]string, 0, len(finalMiners))
		for _, miner := range finalMiners {
			minerStrs = append(minerStrs, miner.String())
		}
		sort.Strings(minerStrs)

		failedMinersStrs := make([]string, 0, len(failedMiners))
		for m, e := range failedMiners {
			failedMinersStrs = append(failedMinersStrs, fmt.Sprintf("%s - %s", m.String(), e.Error()))
		}
		sort.Strings(failedMinersStrs)

		summary := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(summary, "Metric\tValue")                                             //nolint:errcheck // best-effort CLI table output
		fmt.Fprintf(summary, "Curio Nodes Count\t%d\n", len(finalMiners))                  //nolint:errcheck // best-effort CLI table output
		fmt.Fprintf(summary, "Total Curio QualityAdjustedPower\t%s\n", types.DeciStr(qap)) //nolint:errcheck // best-effort CLI table output
		fmt.Fprintf(summary, "Total Curio RawBytePower\t%s\n", types.DeciStr(rb))          //nolint:errcheck // best-effort CLI table output
		fmt.Fprintf(summary, "Percentage QualityAdjustedPower\t%.4f%%\n", perQap)          //nolint:errcheck // best-effort CLI table output
		fmt.Fprintf(summary, "Percentage RawBytePower\t%.4f%%\n", perRb)                   //nolint:errcheck // best-effort CLI table output
		if len(minerStrs) == 0 {
			fmt.Fprintln(summary, "Curio Miners\t-") //nolint:errcheck // best-effort CLI table output
		} else {
			fmt.Fprintf(summary, "Curio Miners\t%s\n", strings.Join(minerStrs, ", ")) //nolint:errcheck // best-effort CLI table output
		}
		fmt.Fprintf(summary, "Unreachable Curio Miners\t%d\n", len(failedMiners))                             //nolint:errcheck // best-effort CLI table output
		fmt.Fprintf(summary, "Total Unreachable Miners QualityAdjustedPower\t%s\n", types.DeciStr(failedQap)) //nolint:errcheck // best-effort CLI table output
		fmt.Fprintf(summary, "Total Unreachable Miners RawBytePower\t%s\n", types.DeciStr(failedRb))          //nolint:errcheck // best-effort CLI table output
		fmt.Fprintf(summary, "Percentage QualityAdjustedPower For Failed Miners\t%.4f%%\n", failedQaPPer)     //nolint:errcheck // best-effort CLI table output
		fmt.Fprintf(summary, "Percentage RawBytePower For Failed Miners\t%.4f%%\n", failedRbPer)              //nolint:errcheck // best-effort CLI table output
		if len(failedMinersStrs) == 0 {
			fmt.Fprintln(summary, "Unreachable Miners\t-") //nolint:errcheck // best-effort CLI table output
		} else {
			fmt.Fprintf(summary, "Unreachable Miners\t%s\n", failedMinersStrs[0]) //nolint:errcheck // best-effort CLI table output
			for _, v := range failedMinersStrs[1:] {
				fmt.Fprintf(summary, "\t%s\n", v) //nolint:errcheck // best-effort CLI table output
			}
		}
		_ = summary.Flush() //nolint:errcheck // best-effort CLI table output

		return nil
	},
}
