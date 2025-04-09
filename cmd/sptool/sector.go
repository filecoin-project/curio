package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	gobig "math/big"
	"os"
	"sort"
	"strconv"

	"github.com/docker/go-units"
	"github.com/fatih/color"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/lib/reqcontext"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/cli/spcli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/lib/tablewriter"
)

var sectorsCmd = &cli.Command{
	Name:  "sectors",
	Usage: "interact with sector store",
	Subcommands: []*cli.Command{
		spcli.SectorsStatusCmd(SPTActorGetter, nil),
		sectorsListCmd, // in-house b/c chain-only is so different. Needs Curio *web* implementation
		spcli.SectorPreCommitsCmd(SPTActorGetter),
		spcli.SectorsCheckExpireCmd(SPTActorGetter),
		sectorsExpiredCmd, // in-house b/c chain-only is so different
		sectorsExtendCmd,
		spcli.TerminateSectorCmd(SPTActorGetter),
		spcli.SectorsCompactPartitionsCmd(SPTActorGetter),
	}}

var sectorsExpiredCmd = &cli.Command{
	Name:  "expired",
	Usage: "Get or cleanup expired sectors",
	Flags: []cli.Flag{
		&cli.Int64Flag{
			Name:        "expired-epoch",
			Usage:       "epoch at which to check sector expirations",
			DefaultText: "WinningPoSt lookback epoch",
		},
	},
	Action: func(cctx *cli.Context) error {
		fullApi, nCloser, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return xerrors.Errorf("getting fullnode api: %w", err)
		}
		defer nCloser()
		ctx := reqcontext.ReqContext(cctx)

		head, err := fullApi.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("getting chain head: %w", err)
		}

		lbEpoch := abi.ChainEpoch(cctx.Int64("expired-epoch"))
		if !cctx.IsSet("expired-epoch") {
			nv, err := fullApi.StateNetworkVersion(ctx, head.Key())
			if err != nil {
				return xerrors.Errorf("getting network version: %w", err)
			}

			lbEpoch = head.Height() - policy.GetWinningPoStSectorSetLookback(nv)
			if lbEpoch < 0 {
				return xerrors.Errorf("too early to terminate sectors")
			}
		}

		if cctx.IsSet("confirm-remove-count") && !cctx.IsSet("expired-epoch") {
			return xerrors.Errorf("--expired-epoch must be specified with --confirm-remove-count")
		}

		lbts, err := fullApi.ChainGetTipSetByHeight(ctx, lbEpoch, head.Key())
		if err != nil {
			return xerrors.Errorf("getting lookback tipset: %w", err)
		}

		maddr, err := SPTActorGetter(cctx)
		if err != nil {
			return xerrors.Errorf("getting actor address: %w", err)
		}

		// toCheck is a working bitfield which will only contain terminated sectors
		toCheck := bitfield.New()
		{
			sectors, err := fullApi.StateMinerSectors(ctx, maddr, nil, lbts.Key())
			if err != nil {
				return xerrors.Errorf("getting sector on chain info: %w", err)
			}

			for _, sector := range sectors {
				if sector.Expiration <= lbts.Height() {
					toCheck.Set(uint64(sector.SectorNumber))
				}
			}
		}

		mact, err := fullApi.StateGetActor(ctx, maddr, lbts.Key())
		if err != nil {
			return err
		}

		tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(fullApi), blockstore.NewMemory())
		mas, err := miner.Load(adt.WrapStore(ctx, cbor.NewCborStore(tbs)), mact)
		if err != nil {
			return err
		}

		alloc, err := mas.GetAllocatedSectors()
		if err != nil {
			return xerrors.Errorf("getting allocated sectors: %w", err)
		}

		// only allocated sectors can be expired,
		toCheck, err = bitfield.IntersectBitField(toCheck, *alloc)
		if err != nil {
			return xerrors.Errorf("intersecting bitfields: %w", err)
		}

		if err := mas.ForEachDeadline(func(dlIdx uint64, dl miner.Deadline) error {
			return dl.ForEachPartition(func(partIdx uint64, part miner.Partition) error {
				live, err := part.LiveSectors()
				if err != nil {
					return err
				}

				toCheck, err = bitfield.SubtractBitField(toCheck, live)
				if err != nil {
					return err
				}

				unproven, err := part.UnprovenSectors()
				if err != nil {
					return err
				}

				toCheck, err = bitfield.SubtractBitField(toCheck, unproven)

				return err
			})
		}); err != nil {
			return err
		}

		err = mas.ForEachPrecommittedSector(func(pci miner.SectorPreCommitOnChainInfo) error {
			toCheck.Unset(uint64(pci.Info.SectorNumber))
			return nil
		})
		if err != nil {
			return err
		}

		// toCheck now only contains sectors which either failed to precommit or are expired/terminated
		fmt.Printf("Sectors that either failed to precommit or are expired/terminated:\n")

		err = toCheck.ForEach(func(u uint64) error {
			fmt.Println(abi.SectorNumber(u))

			return nil
		})
		if err != nil {
			return err
		}

		return nil
	},
}

var sectorsListCmd = &cli.Command{
	Name:  "list",
	Usage: "List sectors",
	Flags: []cli.Flag{
		/*
			&cli.BoolFlag{
				Name:    "show-removed",
				Usage:   "show removed sectors",
				Aliases: []string{"r"},
			},
			&cli.BoolFlag{
				Name:    "fast",
				Usage:   "don't show on-chain info for better performance",
				Aliases: []string{"f"},
			},
			&cli.BoolFlag{
				Name:    "events",
				Usage:   "display number of events the sector has received",
				Aliases: []string{"e"},
			},
			&cli.BoolFlag{
				Name:    "initial-pledge",
				Usage:   "display initial pledge",
				Aliases: []string{"p"},
			},
			&cli.BoolFlag{
				Name:    "seal-time",
				Usage:   "display how long it took for the sector to be sealed",
				Aliases: []string{"t"},
			},
			&cli.StringFlag{
				Name:  "states",
				Usage: "filter sectors by a comma-separated list of states",
			},
			&cli.BoolFlag{
				Name:    "unproven",
				Usage:   "only show sectors which aren't in the 'Proving' state",
				Aliases: []string{"u"},
			},
		*/
	},
	Subcommands: []*cli.Command{
		//sectorsListUpgradeBoundsCmd,
	},
	Action: func(cctx *cli.Context) error {
		fullApi, closer2, err := lcli.GetFullNodeAPI(cctx) // TODO: consider storing full node address in config
		if err != nil {
			return err
		}
		defer closer2()

		ctx := reqcontext.ReqContext(cctx)

		maddr, err := SPTActorGetter(cctx)
		if err != nil {
			return err
		}

		head, err := fullApi.ChainHead(ctx)
		if err != nil {
			return err
		}

		activeSet, err := fullApi.StateMinerActiveSectors(ctx, maddr, head.Key())
		if err != nil {
			return err
		}
		activeIDs := make(map[abi.SectorNumber]struct{}, len(activeSet))
		for _, info := range activeSet {
			activeIDs[info.SectorNumber] = struct{}{}
		}

		sset, err := fullApi.StateMinerSectors(ctx, maddr, nil, head.Key())
		if err != nil {
			return err
		}
		commitedIDs := make(map[abi.SectorNumber]struct{}, len(sset))
		for _, info := range sset {
			commitedIDs[info.SectorNumber] = struct{}{}
		}

		sort.Slice(sset, func(i, j int) bool {
			return sset[i].SectorNumber < sset[j].SectorNumber
		})

		tw := tablewriter.New(
			tablewriter.Col("ID"),
			tablewriter.Col("State"),
			tablewriter.Col("OnChain"),
			tablewriter.Col("Active"),
			tablewriter.Col("Expiration"),
			tablewriter.Col("SealTime"),
			tablewriter.Col("Events"),
			tablewriter.Col("Deals"),
			tablewriter.Col("DealWeight"),
			tablewriter.Col("VerifiedPower"),
			tablewriter.Col("Pledge"),
			tablewriter.NewLineCol("Error"),
			tablewriter.NewLineCol("RecoveryTimeout"))

		fast := cctx.Bool("fast")

		for _, st := range sset {
			s := st.SectorNumber
			_, inSSet := commitedIDs[s]
			_, inASet := activeIDs[s]

			const verifiedPowerGainMul = 9
			dw, vp := .0, .0
			{
				rdw := big.Add(st.DealWeight, st.VerifiedDealWeight)
				dw = float64(big.Div(rdw, big.NewInt(int64(st.Expiration-st.PowerBaseEpoch))).Uint64())
				vp = float64(big.Div(big.Mul(st.VerifiedDealWeight, big.NewInt(verifiedPowerGainMul)), big.NewInt(int64(st.Expiration-st.PowerBaseEpoch))).Uint64())
			}

			var deals int
			for _, deal := range st.DeprecatedDealIDs {
				if deal != 0 {
					deals++
				}
			}

			exp := st.Expiration
			// if st.OnTime > 0 && st.OnTime < exp {
			// 	exp = st.OnTime // Can be different when the sector was CC upgraded
			// }

			m := map[string]interface{}{
				"ID": s,
				//"State":   color.New(spcli.StateOrder[sealing.SectorState(st.State)].Col).Sprint(st.State),
				"OnChain": yesno(inSSet),
				"Active":  yesno(inASet),
			}

			isCC := st.DealWeight.IsZero() && st.VerifiedDealWeight.IsZero()

			if deals > 0 {
				m["Deals"] = color.GreenString("%d", deals)
			} else {
				if isCC {
					m["Deals"] = color.BlueString("CC")
				} else {
					m["Deals"] = color.CyanString("DDO")
				}
				// if st.ToUpgrade {
				// 	m["Deals"] = color.CyanString("CC(upgrade)")
				// }
			}

			if !fast {
				if !inSSet {
					m["Expiration"] = "n/a"
				} else {
					m["Expiration"] = cliutil.EpochTime(head.Height(), exp)
					// if st.Early > 0 {
					// 	m["RecoveryTimeout"] = color.YellowString(cliutil.EpochTime(head.Height(), st.Early))
					// }
				}
				if inSSet && cctx.Bool("initial-pledge") {
					m["Pledge"] = types.FIL(st.InitialPledge).Short()
				}
			}

			if !fast && (deals > 0 || !isCC) {
				m["DealWeight"] = units.BytesSize(dw)
				if vp > 0 {
					m["VerifiedPower"] = color.GreenString(units.BytesSize(vp))
				}
			}

			tw.Write(m)
		}

		return tw.Flush(os.Stdout)
	},
}

var sectorsExtendCmd = &cli.Command{
	Name:  "extend",
	Usage: "Extend expiring sectors while not exceeding each sector's max life",
	Description: `NOTE: --new-expiration, --from and --to flags have multiple formats:
	1. Absolute epoch number: <epoch>
	2. Relative epoch number: +<delta>, e.g. +1000, means 1000 epochs from now
	3. Relative day number: +<delta>d, e.g. +10d, means 10 days from now

The --extension flag has two formats:
	1. Number of epochs to extend by: <epoch>
	2. Number of days to extend by: <delta>d

Extensions will be clamped at either the maximum sector extension of 3.5 years/1278 days or the sector's maximum lifetime
	which currently is 5 years.

`,
	ArgsUsage: "<sectorNumbers...(optional)>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "only consider sectors whose current expiration epoch is in the range of [from, to], <from> defaults to: now + 120 (1 hour)",
			Value: "+120",
		},
		&cli.StringFlag{
			Name:  "to",
			Usage: "only consider sectors whose current expiration epoch is in the range of [from, to], <to> defaults to: now + 92160 (32 days)",
			Value: "+92160",
		},
		&cli.StringFlag{
			Name:  "sector-file",
			Usage: "provide a file containing one sector number in each line, ignoring above selecting criteria",
		},
		&cli.StringFlag{
			Name:  "exclude",
			Usage: "optionally provide a file containing excluding sectors",
		},
		&cli.StringFlag{
			Name:  "extension",
			Usage: "try to extend selected sectors by this number of epochs, defaults to 540 days",
			Value: "540d",
		},
		&cli.StringFlag{
			Name:  "new-expiration",
			Usage: "try to extend selected sectors to this epoch, ignoring extension",
		},
		&cli.BoolFlag{
			Name:  "only-cc",
			Usage: "only extend CC sectors (useful for making sector ready for snap upgrade)",
		},
		&cli.BoolFlag{
			Name:  "no-cc",
			Usage: "don't extend CC sectors (exclusive with --only-cc)",
		},
		&cli.BoolFlag{
			Name:  "drop-claims",
			Usage: "drop claims for sectors that can be extended, but only by dropping some of their verified power claims",
		},
		&cli.Int64Flag{
			Name:  "tolerance",
			Usage: "don't try to extend sectors by fewer than this number of epochs, defaults to 7 days",
			Value: 20160,
		},
		&cli.StringFlag{
			Name:  "max-fee",
			Usage: "use up to this amount of FIL for one message. pass this flag to avoid message congestion.",
			Value: "0",
		},
		&cli.Int64Flag{
			Name:  "max-sectors",
			Usage: "the maximum number of sectors contained in each message",
		},
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "pass this flag to really extend sectors, otherwise will only print out json representation of parameters",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Bool("only-cc") && cctx.Bool("no-cc") {
			return xerrors.Errorf("only one of --only-cc and --no-cc can be set")
		}

		mf, err := types.ParseFIL(cctx.String("max-fee"))
		if err != nil {
			return err
		}

		spec := &api.MessageSendSpec{MaxFee: abi.TokenAmount(mf)}

		fullApi, nCloser, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer nCloser()

		ctx := lcli.ReqContext(cctx)

		maddr, err := SPTActorGetter(cctx)
		if err != nil {
			return err
		}

		head, err := fullApi.ChainHead(ctx)
		if err != nil {
			return err
		}
		currEpoch := head.Height()

		parseEpochString := func(relTo abi.ChainEpoch, s string) (abi.ChainEpoch, error) {
			base := abi.ChainEpoch(0)
			numMult := abi.ChainEpoch(1)
			if s[0] == '+' {
				base = relTo
				s = s[1:]
			}
			if len(s) > 1 && s[len(s)-1] == 'd' {
				if base == 0 {
					return 0, xerrors.Errorf("cannot use day-based delta in absolute mode (add a + prefix)")
				}
				s = s[:len(s)-1]
				numMult = builtin.EpochsInDay
			}
			d, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return 0, err
			}
			return base + (numMult * abi.ChainEpoch(d)), nil
		}

		nv, err := fullApi.StateNetworkVersion(ctx, types.EmptyTSK)
		if err != nil {
			return err
		}

		activeSet, err := fullApi.StateMinerActiveSectors(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		activeSectorsInfo := make(map[abi.SectorNumber]*miner.SectorOnChainInfo, len(activeSet))
		for _, info := range activeSet {
			activeSectorsInfo[info.SectorNumber] = info
		}

		mact, err := fullApi.StateGetActor(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(fullApi), blockstore.NewMemory())
		adtStore := adt.WrapStore(ctx, cbor.NewCborStore(tbs))
		mas, err := miner.Load(adtStore, mact)
		if err != nil {
			return err
		}

		activeSectorsLocation := make(map[abi.SectorNumber]*miner.SectorLocation, len(activeSet))

		if err := mas.ForEachDeadline(func(dlIdx uint64, dl miner.Deadline) error {
			return dl.ForEachPartition(func(partIdx uint64, part miner.Partition) error {
				pas, err := part.ActiveSectors()
				if err != nil {
					return err
				}

				return pas.ForEach(func(i uint64) error {
					activeSectorsLocation[abi.SectorNumber(i)] = &miner.SectorLocation{
						Deadline:  dlIdx,
						Partition: partIdx,
					}
					return nil
				})
			})
		}); err != nil {
			return err
		}

		excludeSet := make(map[abi.SectorNumber]struct{})
		if cctx.IsSet("exclude") {
			excludeSectors, err := getSectorsFromFile(cctx.String("exclude"))
			if err != nil {
				return err
			}

			for _, id := range excludeSectors {
				excludeSet[id] = struct{}{}
			}
		}

		var sectors []abi.SectorNumber
		if cctx.Args().Present() {
			if cctx.IsSet("sector-file") {
				return xerrors.Errorf("sector-file specified along with command line params")
			}

			for i, s := range cctx.Args().Slice() {
				id, err := strconv.ParseUint(s, 10, 64)
				if err != nil {
					return xerrors.Errorf("could not parse sector %d: %w", i, err)
				}

				sectors = append(sectors, abi.SectorNumber(id))
			}
		} else if cctx.IsSet("sector-file") {
			sectors, err = getSectorsFromFile(cctx.String("sector-file"))
			if err != nil {
				return err
			}
		} else {
			from := currEpoch + 120
			to := currEpoch + 92160
			var err error

			if cctx.IsSet("from") {
				from, err = parseEpochString(currEpoch, cctx.String("from"))
				if err != nil {
					return xerrors.Errorf("parsing epoch string: %w", err)
				}
			}

			if cctx.IsSet("to") {
				to, err = parseEpochString(currEpoch, cctx.String("to"))
				if err != nil {
					return xerrors.Errorf("parsing epoch string: %w", err)
				}
			}

			for _, si := range activeSet {
				if si.Expiration >= from && si.Expiration <= to {
					sectors = append(sectors, si.SectorNumber)
				}
			}
		}

		var sis []*miner.SectorOnChainInfo
		for _, id := range sectors {
			if _, exclude := excludeSet[id]; exclude {
				continue
			}

			si, found := activeSectorsInfo[id]
			if !found {
				return xerrors.Errorf("sector %d is not active", id)
			}

			isCC := len(si.DeprecatedDealIDs) == 0 && si.DealWeight.IsZero() && si.VerifiedDealWeight.IsZero()
			if !isCC && cctx.Bool("only-cc") {
				continue
			}
			if isCC && cctx.Bool("no-cc") {
				continue
			}

			sis = append(sis, si)
		}

		withinTolerance := func(a, b abi.ChainEpoch) bool {
			diff := a - b
			if diff < 0 {
				diff = -diff
			}

			return diff <= abi.ChainEpoch(cctx.Int64("tolerance"))
		}

		extensions := map[miner.SectorLocation]map[abi.ChainEpoch][]abi.SectorNumber{}
		for _, si := range sis {
			var newExp abi.ChainEpoch
			if cctx.IsSet("new-expiration") {
				newExp, err = parseEpochString(currEpoch, cctx.String("new-expiration"))
			} else {
				estr := cctx.String("extension")
				newExp, err = parseEpochString(si.Expiration, "+"+estr)
			}
			if err != nil {
				return xerrors.Errorf("parsing expiration: %w", err)
			}

			maxExtension, err := policy.GetMaxSectorExpirationExtension(nv)
			if err != nil {
				return xerrors.Errorf("failed to get max extension: %w", err)
			}

			maxExtendNow := currEpoch + maxExtension
			if newExp > maxExtendNow {
				newExp = maxExtendNow
			}

			maxExp := si.Activation + policy.GetSectorMaxLifetime(si.SealProof, nv)
			if newExp > maxExp {
				newExp = maxExp
			}

			if newExp <= si.Expiration || withinTolerance(newExp, si.Expiration) {
				continue
			}

			l, found := activeSectorsLocation[si.SectorNumber]
			if !found {
				return xerrors.Errorf("location for sector %d not found", si.SectorNumber)
			}

			es, found := extensions[*l]
			if !found {
				ne := make(map[abi.ChainEpoch][]abi.SectorNumber)
				ne[newExp] = []abi.SectorNumber{si.SectorNumber}
				extensions[*l] = ne
			} else {
				added := false
				for exp := range es {
					if withinTolerance(newExp, exp) {
						es[exp] = append(es[exp], si.SectorNumber)
						added = true
						break
					}
				}

				if !added {
					es[newExp] = []abi.SectorNumber{si.SectorNumber}
				}
			}
		}

		verifregAct, err := fullApi.StateGetActor(ctx, builtin.VerifiedRegistryActorAddr, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("failed to lookup verifreg actor: %w", err)
		}

		verifregSt, err := verifreg.Load(adtStore, verifregAct)
		if err != nil {
			return xerrors.Errorf("failed to load verifreg state: %w", err)
		}

		claimsMap, err := verifregSt.GetClaims(maddr)
		if err != nil {
			return xerrors.Errorf("failed to lookup claims for miner: %w", err)
		}

		claimIdsBySector, err := verifregSt.GetClaimIdsBySector(maddr)
		if err != nil {
			return xerrors.Errorf("failed to lookup claim IDs by sector: %w", err)
		}

		sectorsMax, err := policy.GetAddressedSectorsMax(nv)
		if err != nil {
			return err
		}

		declMax, err := policy.GetDeclarationsMax(nv)
		if err != nil {
			return err
		}

		addrSectors := sectorsMax
		if cctx.Int("max-sectors") != 0 {
			addrSectors = cctx.Int("max-sectors")
			if addrSectors > sectorsMax {
				return xerrors.Errorf("the specified max-sectors exceeds the maximum limit")
			}
		}

		var params []miner.ExtendSectorExpiration2Params

		p := miner.ExtendSectorExpiration2Params{}
		scount := 0

		for l, exts := range extensions {
			for newExp, numbers := range exts {
				sectorsWithoutClaimsToExtend := bitfield.New()
				numbersToExtend := make([]abi.SectorNumber, 0, len(numbers))
				var sectorsWithClaims []miner.SectorClaim
				for _, sectorNumber := range numbers {
					claimIdsToMaintain := make([]verifreg.ClaimId, 0)
					claimIdsToDrop := make([]verifreg.ClaimId, 0)
					cannotExtendSector := false
					claimIds, ok := claimIdsBySector[sectorNumber]
					// Nothing to check, add to ccSectors
					if !ok {
						sectorsWithoutClaimsToExtend.Set(uint64(sectorNumber))
						numbersToExtend = append(numbersToExtend, sectorNumber)
					} else {
						for _, claimId := range claimIds {
							claim, ok := claimsMap[claimId]
							if !ok {
								return xerrors.Errorf("failed to find claim for claimId %d", claimId)
							}
							claimExpiration := claim.TermStart + claim.TermMax
							// can be maintained in the extended sector
							if claimExpiration > newExp {
								claimIdsToMaintain = append(claimIdsToMaintain, claimId)
							} else {
								sectorInfo, ok := activeSectorsInfo[sectorNumber]
								if !ok {
									return xerrors.Errorf("failed to find sector in active sector set: %w", err)
								}
								if !cctx.Bool("drop-claims") ||
									// FIP-0045 requires the claim minimum duration to have passed
									currEpoch <= (claim.TermStart+claim.TermMin) ||
									// FIP-0045 requires the sector to be in its last 30 days of life
									(currEpoch <= sectorInfo.Expiration-builtin.EndOfLifeClaimDropPeriod) {
									fmt.Printf("skipping sector %d because claim %d (client f0%s, piece %s) does not live long enough \n", sectorNumber, claimId, claim.Client, claim.Data)
									cannotExtendSector = true
									break
								}

								claimIdsToDrop = append(claimIdsToDrop, claimId)
							}

							numbersToExtend = append(numbersToExtend, sectorNumber)
						}
						if cannotExtendSector {
							continue
						}

						if len(claimIdsToMaintain)+len(claimIdsToDrop) != 0 {
							sectorsWithClaims = append(sectorsWithClaims, miner.SectorClaim{
								SectorNumber:   sectorNumber,
								MaintainClaims: claimIdsToMaintain,
								DropClaims:     claimIdsToDrop,
							})
						}
					}
				}

				sectorsWithoutClaimsCount, err := sectorsWithoutClaimsToExtend.Count()
				if err != nil {
					return xerrors.Errorf("failed to count cc sectors: %w", err)
				}

				sectorsInDecl := int(sectorsWithoutClaimsCount) + len(sectorsWithClaims)
				scount += sectorsInDecl

				if scount > addrSectors || len(p.Extensions) >= declMax {
					params = append(params, p)
					p = miner.ExtendSectorExpiration2Params{}
					scount = sectorsInDecl
				}

				p.Extensions = append(p.Extensions, miner.ExpirationExtension2{
					Deadline:          l.Deadline,
					Partition:         l.Partition,
					Sectors:           spcli.SectorNumsToBitfield(numbersToExtend),
					SectorsWithClaims: sectorsWithClaims,
					NewExpiration:     newExp,
				})

			}
		}

		// if we have any sectors, then one last append is needed here
		if scount != 0 {
			params = append(params, p)
		}

		if len(params) == 0 {
			fmt.Println("nothing to extend")
			return nil
		}

		mi, err := fullApi.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("getting miner info: %w", err)
		}

		stotal := 0

		for i := range params {
			scount := 0
			for _, ext := range params[i].Extensions {
				count, err := ext.Sectors.Count()
				if err != nil {
					return err
				}
				scount += int(count)
			}
			fmt.Printf("Extending %d sectors: ", scount)
			stotal += scount

			sp, aerr := actors.SerializeParams(&params[i])
			if aerr != nil {
				return xerrors.Errorf("serializing params: %w", err)
			}

			m := &types.Message{
				From:   mi.Worker,
				To:     maddr,
				Method: builtin.MethodsMiner.ExtendSectorExpiration2,
				Value:  big.Zero(),
				Params: sp,
			}

			if !cctx.Bool("really-do-it") {
				pp, err := spcli.NewPseudoExtendParams(&params[i])
				if err != nil {
					return err
				}

				data, err := json.MarshalIndent(pp, "", "  ")
				if err != nil {
					return err
				}

				fmt.Println("\n", string(data))

				ge, err := fullApi.GasEstimateMessageGas(ctx, m, spec, types.EmptyTSK)
				if err != nil {
					return xerrors.Errorf("simulating message execution: %w", err)
				}

				fmt.Printf("GasLimit: %v\n", siStr(types.NewInt(uint64(ge.GasLimit))))
				fmt.Printf("FeeCap: %s\n", types.FIL(ge.GasFeeCap).Short())
				fmt.Printf("MaxFee: %s\n", types.FIL(ge.RequiredFunds()))

				continue
			}

			smsg, err := fullApi.MpoolPushMessage(ctx, m, spec)
			if err != nil {
				return xerrors.Errorf("mpool push message: %w", err)
			}

			fmt.Println(smsg.Cid())
		}

		if !cctx.Bool("really-do-it") {
			fmt.Printf("%d sectors to extended, pass --really-do-it to proceed\n", stotal)
		} else {
			fmt.Printf("%d sectors extended\n", stotal)
		}

		return nil
	},
}

func yesno(b bool) string {
	if b {
		return color.GreenString("YES")
	}
	return color.RedString("NO")
}

func getSectorsFromFile(filePath string) ([]abi.SectorNumber, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(file)
	sectors := make([]abi.SectorNumber, 0)

	for scanner.Scan() {
		line := scanner.Text()

		id, err := strconv.ParseUint(line, 10, 64)
		if err != nil {
			return nil, xerrors.Errorf("could not parse %s as sector id: %s", line, err)
		}

		sectors = append(sectors, abi.SectorNumber(id))
	}

	if err = file.Close(); err != nil {
		return nil, err
	}

	return sectors, nil
}

var siUnits = []string{"", "K", "M", "G", "T"}

func siStr(bi types.BigInt) string {
	r := new(gobig.Rat).SetInt(bi.Int)
	den := gobig.NewRat(1, 1000)

	var i int
	for f, _ := r.Float64(); f >= 1000 && i+1 < len(siUnits); f, _ = r.Float64() {
		i++
		r = r.Mul(r, den)
	}

	f, _ := r.Float64()
	return fmt.Sprintf("%.3g %s", f, siUnits[i])
}
