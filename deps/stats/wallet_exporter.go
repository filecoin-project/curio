package stats

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	logging "github.com/ipfs/go-log/v2"
	promclient "github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("wallet_exporter")

// 1 epoch, kinda makes sense
var WalletExporterInterval = 30 * time.Second
var WalletExporterGCAfter = 3 * 24 * time.Hour

var WalletExporterMeasures = struct {
	BalanceNFil *stats.Int64Measure
	Power       *stats.Int64Measure

	MessageSent   *stats.Int64Measure
	MessageLanded *stats.Int64Measure

	GasUnitsRequested *stats.Int64Measure
	GasUnitsUsed      *stats.Int64Measure

	SentNFil    *stats.Int64Measure // landed messages
	GasPaidNFil *stats.Int64Measure // landed messages (gasUsed * (basefee + tip))

	MessageLandDuration promclient.Histogram
}{
	BalanceNFil: stats.Int64(pre+"balance_nfil", "Balance in NanoFIL", stats.UnitDimensionless),
	Power:       stats.Int64(pre+"power", "Power in Bytes", stats.UnitBytes),

	MessageSent:   stats.Int64(pre+"message_sent", "Message sent", stats.UnitDimensionless),
	MessageLanded: stats.Int64(pre+"message_landed", "Message landed", stats.UnitDimensionless),

	GasUnitsRequested: stats.Int64(pre+"gas_units_requested", "Gas units requested", stats.UnitDimensionless),
	GasUnitsUsed:      stats.Int64(pre+"gas_units_used", "Gas units used", stats.UnitDimensionless),

	SentNFil:    stats.Int64(pre+"sent_nfil", "Sent NanoFIL", stats.UnitDimensionless),
	GasPaidNFil: stats.Int64(pre+"gas_paid_nfil", "Gas paid NanoFIL", stats.UnitDimensionless),

	MessageLandDuration: promclient.NewHistogram(promclient.HistogramOpts{
		Name:    pre + "message_land_duration_seconds",
		Buckets: []float64{30 * 1, 30 * 2, 30 * 4, 30 * 5, 30 * 7, 30 * 15, 30 * 45, 30 * 120, 30 * 300, 30 * 1000, 30 * 2880},
		Help:    "The histogram of message land durations in seconds.",
	}),
}

func init() {
	err := view.Register(
		&view.View{
			Measure:     WalletExporterMeasures.BalanceNFil,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{tagAddressKey, tagTypeKey},
		},
		&view.View{
			Measure:     WalletExporterMeasures.Power,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{tagAddressKey, tagTypeKey},
		},
		&view.View{
			Measure:     WalletExporterMeasures.MessageSent,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{tagFromKey, tagFromName, tagToKey, tagToName, tagMethodKey, tagSendReasonKey},
		},
		&view.View{
			Measure:     WalletExporterMeasures.MessageLanded,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{tagFromKey, tagFromName, tagToKey, tagToName, tagMethodKey, tagSendReasonKey, tagExitCodeKey},
		},
		&view.View{
			Measure:     WalletExporterMeasures.GasUnitsRequested,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{tagFromKey, tagFromName, tagToKey, tagToName, tagMethodKey, tagSendReasonKey},
		},
		&view.View{
			Measure:     WalletExporterMeasures.GasUnitsUsed,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{tagFromKey, tagFromName, tagToKey, tagToName, tagMethodKey, tagSendReasonKey, tagExitCodeKey},
		},
		&view.View{
			Measure:     WalletExporterMeasures.SentNFil,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{tagFromKey, tagFromName, tagToKey, tagToName, tagMethodKey, tagSendReasonKey, tagExitCodeKey},
		},
		&view.View{
			Measure:     WalletExporterMeasures.GasPaidNFil,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{tagFromKey, tagFromName, tagToKey, tagToName, tagMethodKey, tagSendReasonKey, tagExitCodeKey},
		},
	)
	if err != nil {
		panic(err)
	}
}

/*
	Exporter for:
	* Wallet balance
	* Wallet transaction counts
	* Gas Fees
*/

var (
	tagAddressKey, _ = tag.NewKey("address")
	tagTypeKey, _    = tag.NewKey("type")

	tagFromKey, _       = tag.NewKey("from")
	tagFromName, _      = tag.NewKey("from_name")
	tagToKey, _         = tag.NewKey("to")
	tagToName, _        = tag.NewKey("to_name")
	tagMethodKey, _     = tag.NewKey("method")
	tagSendReasonKey, _ = tag.NewKey("send_reason")
	tagExitCodeKey, _   = tag.NewKey("exit_code")

	pre = "wallet_"
)

func attoToNano(atto types.BigInt) int64 {
	return big.Div(atto, big.NewInt(1e9)).Int64()
}

func StartWalletExporter(ctx context.Context, db *harmonydb.DB, api api.FullNode, spIDs []address.Address) {
	go func() {
		for {
			select {
			case <-ctx.Done():
			case <-time.After(WalletExporterInterval):
				walletExporterCycle(ctx, db, api, spIDs)
			}
		}
	}()
}

func walletExporterCycle(ctx context.Context, db *harmonydb.DB, api api.FullNode, spIDs []address.Address) {
	names, err := walletExporterBalances(ctx, db, api)
	if err != nil {
		log.Errorf("failed to get balances: %v", err)
		return
	}
	walletExporterSPs(ctx, db, api, spIDs)

	walletExporterNewWatchedMsgs(ctx, db, api, names)
	walletExporterObserveLandedMsgs(ctx, db, api, names)
	walletExporterGCObservedMsgs(ctx, db, api)
}

type walletName = string
type walletAddrStr = string

func walletExporterBalances(ctx context.Context, db *harmonydb.DB, api api.FullNode) (map[walletAddrStr]walletName, error) {
	// get a list of all wallets
	rows, err := db.Query(ctx, `SELECT wallet, name FROM wallet_names`)
	if err != nil {
		log.Errorf("failed to get wallets: %v", err)
		return nil, err
	}
	defer rows.Close()

	names := make(map[walletAddrStr]walletName)
	for rows.Next() {
		var wallet, name string
		err = rows.Scan(&wallet, &name)
		if err != nil {
			log.Errorf("failed to scan wallet: %v", err)
			continue
		}
		names[wallet] = name
	}

	resolved := make(map[address.Address]walletAddrStr)
	for _, wallet := range names {
		addr, err := address.NewFromString(wallet)
		if err != nil {
			log.Errorf("failed to resolve wallet: %v", err)
			continue
		}
		kaddr, err := api.StateAccountKey(ctx, addr, types.EmptyTSK)
		if err == nil {
			addr = kaddr
		}
		names[addr.String()] = names[wallet]
		resolved[addr] = wallet
	}

	// get the balance for each wallet
	for addr := range resolved {
		act, err := api.StateGetActor(ctx, addr, types.EmptyTSK)
		if err != nil {
			log.Errorf("failed to get balance for wallet: %v", err)
			continue
		}
		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(tagAddressKey, addr.String()),
			tag.Upsert(tagTypeKey, "wallet"),
		}, WalletExporterMeasures.BalanceNFil.M(attoToNano(act.Balance)))
	}

	return names, nil
}

func walletExporterSPs(ctx context.Context, db *harmonydb.DB, api api.FullNode, spIDs []address.Address) {
	// export SPIDs

	ts, err := api.ChainHead(ctx)
	if err != nil {
		log.Errorf("failed to get chain head: %v", err)
		return
	}

	for _, spID := range spIDs {
		availableBalance, err := api.StateMinerAvailableBalance(ctx, spID, ts.Key())
		if err != nil {
			log.Errorf("failed to get balance for SPID: %v", err)
			continue
		}
		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(tagAddressKey, spID.String()),
			tag.Upsert(tagTypeKey, "miner-available"),
		}, WalletExporterMeasures.BalanceNFil.M(attoToNano(availableBalance)))

		pow, err := api.StateMinerPower(ctx, spID, ts.Key())
		if err != nil {
			log.Errorf("failed to get miner power for SPID: %v", err)
			continue
		}
		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(tagAddressKey, spID.String()),
			tag.Upsert(tagTypeKey, "raw"),
		}, WalletExporterMeasures.Power.M(pow.MinerPower.RawBytePower.Int64()))
		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(tagAddressKey, spID.String()),
			tag.Upsert(tagTypeKey, "qap"),
		}, WalletExporterMeasures.Power.M(pow.MinerPower.QualityAdjPower.Int64()))
	}
}

func walletExporterNewWatchedMsgs(ctx context.Context, db *harmonydb.DB, api api.FullNode, nameMap map[walletAddrStr]walletName) {
	var unobservedMsgs []string
	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		unobservedMsgs = nil

		rows, err := tx.Query(`SELECT created_at FROM wallet_exporter_processing`)
		if err != nil {
			return false, err
		}

		var processedUntil time.Time
		if rows.Next() {
			err = rows.Scan(&processedUntil)
		}
		rows.Close()
		if err != nil {
			return false, err
		}

		// Get all messages from the message_waits table that have a created_at timestamp greater than the observed-until timestamp
		rows, err = tx.Query(`SELECT signed_message_cid, created_at FROM message_waits WHERE created_at > $1`, processedUntil)
		if err != nil {
			return false, err
		}

		var maxCreatedAt time.Time

		for rows.Next() {
			var msgCid string
			var createdAt time.Time
			err = rows.Scan(&msgCid, &createdAt)
			if err != nil {
				rows.Close()
				return false, err
			}
			if createdAt.After(maxCreatedAt) {
				maxCreatedAt = createdAt
			}
			unobservedMsgs = append(unobservedMsgs, msgCid)
		}
		rows.Close()

		if maxCreatedAt.After(processedUntil) {
			_, err = tx.Exec(`UPDATE wallet_exporter_processing SET processed_until = $1`, maxCreatedAt)
			if err != nil {
				return false, err
			}
		}

		// Insert into the watched_msgs table, in memory keep which ones we need to create observations for
		for _, msgCid := range unobservedMsgs {
			_, err = tx.Exec(`INSERT INTO wallet_exporter_watched_msgs (msg_cid) VALUES ($1)`, msgCid)
			if err != nil {
				return false, err
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		log.Errorf("failed to begin observe transaction: %v", err)
		return
	}

	if len(unobservedMsgs) == 0 {
		return
	}

	// we're now responsible for observing the messages

	for _, msgCid := range unobservedMsgs {
		var fromKey, toAddr, sendReason string
		var method int64
		var gasLimit int64

		rows, err := db.Query(ctx, `SELECT from_key, to_addr, (signed_json -> 'Message' ->> 'Method')::bigint, send_reason, (signed_json -> 'Message' ->> 'GasLimit')::bigint FROM message_sends WHERE signed_cid = $1`, msgCid)
		if err != nil {
			log.Errorf("failed to get message: %v", err)
			continue
		}

		if rows.Next() {
			err = rows.Scan(&fromKey, &toAddr, &method, &sendReason, &gasLimit)
			if err != nil {
				rows.Close()
				log.Errorf("failed to scan message: %v", err)
				continue
			}
		}
		rows.Close()

		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(tagFromKey, fromKey),
			tag.Upsert(tagFromName, nameMap[fromKey]),
			tag.Upsert(tagToKey, toAddr),
			tag.Upsert(tagToName, nameMap[toAddr]),
			tag.Upsert(tagMethodKey, strconv.FormatInt(method, 10)),
			tag.Upsert(tagSendReasonKey, sendReason),
		}, WalletExporterMeasures.MessageSent.M(1))

		// Gas requested metric
		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(tagFromKey, fromKey),
			tag.Upsert(tagFromName, nameMap[fromKey]),
			tag.Upsert(tagToKey, toAddr),
			tag.Upsert(tagToName, nameMap[toAddr]),
			tag.Upsert(tagMethodKey, strconv.FormatInt(method, 10)),
			tag.Upsert(tagSendReasonKey, sendReason),
		}, WalletExporterMeasures.GasUnitsRequested.M(gasLimit))
	}
}

func walletExporterObserveLandedMsgs(ctx context.Context, db *harmonydb.DB, api api.FullNode, nameMap map[walletAddrStr]walletName) {
	var msgCids []string

	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		msgCids = nil

		rows, err := tx.Query(`
			SELECT wem.msg_cid 
			FROM wallet_exporter_watched_msgs wem
			JOIN message_waits mw ON wem.msg_cid = mw.signed_message_cid
			WHERE wem.observed_landed = FALSE 
			AND mw.executed_tsk_cid IS NOT NULL`)
		if err != nil {
			return false, err
		}

		for rows.Next() {
			var msgCid string
			if err := rows.Scan(&msgCid); err != nil {
				rows.Close()
				return false, err
			}
			msgCids = append(msgCids, msgCid)
		}
		rows.Close()

		// Mark these messages as observed_landed
		for _, msgCid := range msgCids {
			_, err = tx.Exec(`UPDATE wallet_exporter_watched_msgs SET observed_landed = TRUE WHERE msg_cid = $1`, msgCid)
			if err != nil {
				return false, err
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		log.Errorf("failed to observe landed messages: %v", err)
		return
	}

	if len(msgCids) == 0 {
		return
	}

	ts, err := api.ChainHead(ctx)
	if err != nil {
		log.Errorf("failed to get chain head: %v", err)
		return
	}
	headEpoch := int64(ts.Height())
	headTimestamp := int64(ts.MinTimestamp())

	for _, msgCid := range msgCids {
		var (
			fromKey       string
			toAddr        string
			method        int64
			sendReason    string
			sendTime      sql.NullTime
			execEpoch     int64
			exitCode      int64
			gasUsed       int64
			valueAttoStr  string
			gasPremiumStr string
		)

		err := db.QueryRow(ctx, `
			SELECT ms.from_key,
			       ms.to_addr,
			       (ms.signed_json -> 'Message' ->> 'Method')::bigint,
			       ms.send_reason,
			       ms.send_time,
			       mw.executed_tsk_epoch,
			       mw.executed_rcpt_exitcode,
			       mw.executed_rcpt_gas_used,
			       (ms.signed_json -> 'Message' ->> 'Value')::text,
			       (ms.signed_json -> 'Message' ->> 'GasPremium')::text
			FROM message_sends ms
			JOIN message_waits mw ON ms.signed_cid = mw.signed_message_cid
			WHERE ms.signed_cid = $1`, msgCid).
			Scan(&fromKey, &toAddr, &method, &sendReason, &sendTime, &execEpoch, &exitCode, &gasUsed, &valueAttoStr, &gasPremiumStr)
		if err != nil {
			log.Errorf("failed to load landed message data: %v", err)
			continue
		}

		// Calculate duration between send and land
		if sendTime.Valid && execEpoch != 0 {
			diffEpochs := headEpoch - execEpoch
			if diffEpochs < 0 {
				diffEpochs = 0
			}
			landedTimestamp := headTimestamp - (int64(build.BlockDelaySecs) * diffEpochs)
			landedTime := time.Unix(landedTimestamp, 0)
			durationSeconds := landedTime.Sub(sendTime.Time).Seconds()
			if durationSeconds < 0 {
				durationSeconds = 0
			}
			WalletExporterMeasures.MessageLandDuration.Observe(durationSeconds)
		}

		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(tagFromKey, fromKey),
			tag.Upsert(tagFromName, nameMap[fromKey]),
			tag.Upsert(tagToKey, toAddr),
			tag.Upsert(tagToName, nameMap[toAddr]),
			tag.Upsert(tagMethodKey, strconv.FormatInt(method, 10)),
			tag.Upsert(tagSendReasonKey, sendReason),
			tag.Upsert(tagExitCodeKey, strconv.FormatInt(exitCode, 10)),
		}, WalletExporterMeasures.MessageLanded.M(1))

		// Gas used metric
		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(tagFromKey, fromKey),
			tag.Upsert(tagFromName, nameMap[fromKey]),
			tag.Upsert(tagToKey, toAddr),
			tag.Upsert(tagToName, nameMap[toAddr]),
			tag.Upsert(tagMethodKey, strconv.FormatInt(method, 10)),
			tag.Upsert(tagSendReasonKey, sendReason),
			tag.Upsert(tagExitCodeKey, strconv.FormatInt(exitCode, 10)),
		}, WalletExporterMeasures.GasUnitsUsed.M(gasUsed))

		// Compute Sent NanoFIL
		if valueAtto, err := big.FromString(valueAttoStr); err == nil {
			_ = stats.RecordWithTags(ctx, []tag.Mutator{
				tag.Upsert(tagFromKey, fromKey),
				tag.Upsert(tagFromName, nameMap[fromKey]),
				tag.Upsert(tagToKey, toAddr),
				tag.Upsert(tagToName, nameMap[toAddr]),
				tag.Upsert(tagMethodKey, strconv.FormatInt(method, 10)),
				tag.Upsert(tagSendReasonKey, sendReason),
				tag.Upsert(tagExitCodeKey, strconv.FormatInt(exitCode, 10)),
			}, WalletExporterMeasures.SentNFil.M(big.Div(valueAtto, big.NewInt(1e9)).Int64()))
		}

		// Gas paid computation
		// Get base fee at executed epoch
		var gasPaidNano int64
		if execEpoch != 0 {
			tip, err := api.ChainGetTipSetByHeight(ctx, abi.ChainEpoch(execEpoch), ts.Key())
			if err == nil && len(tip.Blocks()) > 0 {
				baseFee := tip.Blocks()[0].ParentBaseFee
				gp, err2 := big.FromString(gasPremiumStr)
				if err2 != nil {
					gp = big.Zero()
				}
				feePerGas := big.Add(baseFee, gp)
				feeTotal := big.Mul(feePerGas, big.NewInt(gasUsed))
				gasPaidNano = big.Div(feeTotal, big.NewInt(1e9)).Int64()
			}
		}
		if gasPaidNano > 0 {
			_ = stats.RecordWithTags(ctx, []tag.Mutator{
				tag.Upsert(tagFromKey, fromKey),
				tag.Upsert(tagFromName, nameMap[fromKey]),
				tag.Upsert(tagToKey, toAddr),
				tag.Upsert(tagToName, nameMap[toAddr]),
				tag.Upsert(tagMethodKey, strconv.FormatInt(method, 10)),
				tag.Upsert(tagSendReasonKey, sendReason),
				tag.Upsert(tagExitCodeKey, strconv.FormatInt(exitCode, 10)),
			}, WalletExporterMeasures.GasPaidNFil.M(gasPaidNano))
		}
	}
}

func walletExporterGCObservedMsgs(ctx context.Context, db *harmonydb.DB, api api.FullNode) {
	// Remove watched message records that have been observed as landed and are older than the configured GC window.
	cutoff := time.Now().Add(-WalletExporterGCAfter)

	rowsDeleted, err := db.Exec(ctx, `DELETE FROM wallet_exporter_watched_msgs WHERE observed_landed = TRUE AND created_at < $1`, cutoff)
	if err != nil {
		log.Errorf("failed to GC wallet exporter watched messages: %v", err)
		return
	}
	if rowsDeleted > 0 {
		log.Infow("wallet exporter GC removed watched msgs records", "count", rowsDeleted)
	}
}
