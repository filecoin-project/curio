package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gbrlsnchs/jwt/v3"
	"github.com/ipfs/go-cid"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-jsonrpc/auth"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/cmd/curio/rpc"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/reqcontext"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/lib/tablewriter"
)

const providerEnvVar = "CURIO_API_INFO"

var cliCmd = &cli.Command{
	Name:  "cli",
	Usage: translations.T("Execute cli commands"),
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "machine",
			Usage: translations.T("machine host:port (curio run --listen address)"),
		},
	},
	Before: func(cctx *cli.Context) error {
		if os.Getenv(providerEnvVar) != "" {
			// set already
			return nil
		}
		if os.Getenv("LOTUS_DOCS_GENERATION") == "1" {
			return nil
		}

		db, err := deps.MakeDB(cctx)
		if err != nil {
			return err
		}

		ctx := reqcontext.ReqContext(cctx)

		machine := cctx.String("machine")
		if machine == "" {
			// interactive picker
			var machines []struct {
				HostAndPort string    `db:"host_and_port"`
				LastContact time.Time `db:"last_contact"`
			}

			err := db.Select(ctx, &machines, "select host_and_port, last_contact from harmony_machines")
			if err != nil {
				return xerrors.Errorf("getting machine list: %w", err)
			}

			now := time.Now()
			fmt.Println("Available machines:")
			for i, m := range machines {
				// A machine is healthy if contacted not longer than 2 minutes ago
				healthStatus := "unhealthy"
				if now.Sub(m.LastContact) <= 2*time.Minute {
					healthStatus = "healthy"
				}
				fmt.Printf("%d. %s %s\n", i+1, m.HostAndPort, healthStatus)
			}

			fmt.Print("Select: ")
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return xerrors.Errorf("reading selection: %w", err)
			}

			var selection int
			_, err = fmt.Sscanf(input, "%d", &selection)
			if err != nil {
				return xerrors.Errorf("parsing selection: %w", err)
			}

			if selection < 1 || selection > len(machines) {
				return xerrors.New("invalid selection")
			}

			machine = machines[selection-1].HostAndPort
		}

		var apiKeys []string
		{
			var dbconfigs []struct {
				Config string `db:"config"`
				Title  string `db:"title"`
			}

			err := db.Select(ctx, &dbconfigs, "select config from harmony_config")
			if err != nil {
				return xerrors.Errorf("getting configs: %w", err)
			}

			var seen = make(map[string]struct{})

			for _, config := range dbconfigs {
				var layer struct {
					Apis struct {
						StorageRPCSecret string
					}
				}

				if _, err := toml.Decode(config.Config, &layer); err != nil {
					return xerrors.Errorf("decode config layer %s: %w", config.Title, err)
				}

				if layer.Apis.StorageRPCSecret != "" {
					if _, ok := seen[layer.Apis.StorageRPCSecret]; ok {
						continue
					}
					seen[layer.Apis.StorageRPCSecret] = struct{}{}
					apiKeys = append(apiKeys, layer.Apis.StorageRPCSecret)
				}
			}
		}

		if len(apiKeys) == 0 {
			return xerrors.New("no api keys found in the database")
		}
		if len(apiKeys) > 1 {
			return xerrors.Errorf("multiple api keys found in the database, not supported yet")
		}

		var apiToken []byte
		{
			type jwtPayload struct {
				Allow []auth.Permission
			}

			p := jwtPayload{
				Allow: api.AllPermissions,
			}

			sk, err := base64.StdEncoding.DecodeString(apiKeys[0])
			if err != nil {
				return xerrors.Errorf("decode secret: %w", err)
			}

			apiToken, err = jwt.Sign(&p, jwt.NewHS256(sk))
			if err != nil {
				return xerrors.Errorf("signing token: %w", err)
			}
		}

		{

			laddr, err := net.ResolveTCPAddr("tcp", machine)
			if err != nil {
				return xerrors.Errorf("net resolve: %w", err)
			}

			if len(laddr.IP) == 0 {
				// set localhost
				laddr.IP = net.IPv4(127, 0, 0, 1)
			}

			ma, err := manet.FromNetAddr(laddr)
			if err != nil {
				return xerrors.Errorf("net from addr (%v): %w", laddr, err)
			}

			token := fmt.Sprintf("%s:%s", string(apiToken), ma)
			if err := os.Setenv(providerEnvVar, token); err != nil {
				return xerrors.Errorf("setting env var: %w", err)
			}
		}

		{
			api, closer, err := rpc.GetCurioAPI(cctx)
			if err != nil {
				return err
			}
			defer closer()

			v, err := api.Version(ctx)
			if err != nil {
				return xerrors.Errorf("querying version: %w", err)
			}

			fmt.Println("remote node version:", v)
		}

		return nil
	},
	Subcommands: []*cli.Command{
		infoCmd,
		storageCmd,
		logCmd,
		waitApiCmd,
		stopCmd,
		cordonCmd,
		uncordonCmd,
		indexSampleCmd,
		downgradeCmd,
	},
}

var waitApiCmd = &cli.Command{
	Name:  "wait-api",
	Usage: translations.T("Wait for Curio api to come online"),
	Flags: []cli.Flag{
		&cli.DurationFlag{
			Name:  "timeout",
			Usage: translations.T("duration to wait till fail"),
			Value: time.Second * 30,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := reqcontext.ReqContext(cctx)
		ctx, cancel := context.WithTimeout(ctx, cctx.Duration("timeout"))
		defer cancel()
		for ctx.Err() == nil {

			api, closer, err := rpc.GetCurioAPI(cctx)
			if err != nil {
				fmt.Printf("Not online yet... (%s)\n", err)
				time.Sleep(time.Second)
				continue
			}
			defer closer()

			_, err = api.Version(ctx)
			if err != nil {
				return err
			}

			return nil
		}

		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("timed out waiting for api to come online")
		}

		return ctx.Err()
	},
}

var indexSampleCmd = &cli.Command{
	Name:      "index-sample",
	Usage:     translations.T("Provides a sample of CIDs from an indexed piece"),
	ArgsUsage: translations.T("piece-cid"),
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "json",
			Usage: translations.T("output in json format"),
			Value: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		capi, closer, err := rpc.GetCurioAPI(cctx)
		if err != nil {
			return err
		}

		defer closer()
		ctx := reqcontext.ReqContext(cctx)

		pieceCid, err := cid.Parse(cctx.Args().First())
		if err != nil {
			return xerrors.Errorf("parsing piece cid: %w", err)
		}

		samples, err := capi.IndexSamples(ctx, pieceCid)
		if err != nil {
			return xerrors.Errorf("getting samples: %w", err)
		}

		MhHex := "Multihash HEX"
		MhB58 := "Multihash B58"
		CID := "CID v1"

		type mhJson struct {
			B58 string `json:"b58"`
			CID string `json:"cid"`
		}

		mhMap := make([]map[string]interface{}, 0, len(samples))
		jsonMap := make(map[string]mhJson, len(samples))
		for _, sample := range samples {
			cidv1 := cid.NewCidV1(cid.DagProtobuf, sample)
			m := map[string]interface{}{
				MhHex: sample.HexString(),
				MhB58: sample.B58String(),
				CID:   cidv1.String(),
			}
			mhMap = append(mhMap, m)

			jsonMap[sample.HexString()] = mhJson{
				B58: sample.B58String(),
				CID: cidv1.String(),
			}
		}

		if cctx.Bool("json") {
			return PrintJson(jsonMap)
		}

		tw := tablewriter.New(
			tablewriter.Col(MhHex),
			tablewriter.Col(MhB58),
			tablewriter.Col(CID),
		)

		for i := range mhMap {
			tw.Write(mhMap[i])
		}

		return tw.Flush(os.Stdout)
	},
}

func PrintJson(obj interface{}) error {
	resJson, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return xerrors.Errorf("marshalling json: %w", err)
	}

	fmt.Println(string(resJson))
	return nil
}

var downgradeCmd = &cli.Command{
	Name:        "downgrade",
	Usage:       translations.T("Downgrade a cluster's daatabase to a previous software version."),
	Description: translations.T("If, however, the upgrade has a serious bug and you need to downgrade, first shutdown all nodes in your cluster and then run this command. Finally, only start downgraded nodes."),
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:     "last_good_date",
			Usage:    translations.T("YYYYMMDD when your cluster had the preferred schema. Ex: 20251128"),
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		db, err := deps.MakeDB(cctx)
		if err != nil {
			return err
		}
		return db.RevertTo(cctx.Context, cctx.Int("last_good_date"))
	},
}
