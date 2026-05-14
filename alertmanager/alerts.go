package alertmanager

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/lists"
	"github.com/filecoin-project/curio/tasks/tasknames"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	cliutil "github.com/filecoin-project/lotus/cli/util"
)

type AlertNow struct {
	db   *harmonydb.DB
	name string
}

func NewAlertNow(db *harmonydb.DB, name string) *AlertNow {
	return &AlertNow{
		db:   db,
		name: name,
	}
}

func (n *AlertNow) AddAlert(msg string) {
	_, err := n.db.Exec(context.Background(), "INSERT INTO alerts (machine_name, message) VALUES ($1, $2)", n.name, msg)
	if err != nil {
		log.Errorf("Failed to add alert: %s", err)
	}
}

func NowCheck(al *alerts) {
	Name := Name_NowCheck
	al.alertMap[Name] = &alertOut{}

	type NowType struct {
		ID      int    `db:"id"`
		Name    string `db:"machine_name"`
		Message string `db:"message"`
	}
	var nowAlerts []NowType
	err := al.db.Select(al.ctx, &nowAlerts, `
				SELECT id, machine_name, message
				FROM alerts`)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting now alerts: %w", err)
		return
	}

	if len(nowAlerts) == 0 {
		return
	}

	// Migrate alerts from old 'alerts' table to 'alert_history' and delete from 'alerts'
	for _, na := range nowAlerts {
		// Insert into alert_history
		_, insertErr := al.db.Exec(al.ctx, `
			INSERT INTO alert_history (alert_name, message, machine_name, sent_to_plugins, sent_at)
			VALUES ($1, $2, $3, FALSE, NOW())
		`, Name, na.Message, na.Name)
		if insertErr != nil {
			log.Errorf("Failed to migrate alert to history: %s", insertErr)
			continue
		}

		// Delete from old alerts table
		_, delErr := al.db.Exec(al.ctx, "DELETE FROM alerts WHERE id = $1", na.ID)
		if delErr != nil {
			log.Errorf("Failed to delete migrated alert: %s", delErr)
		}
	}

	// Build alert string for external notification
	al.alertMap[Name].alertString = strings.Join(lo.Map(nowAlerts, func(n NowType, _ int) string {
		return fmt.Sprintf("Machine %s: %s", n.Name, n.Message)
	}), " ")
}

// balanceCheck retrieves the machine details from the database and performs balance checks on unique addresses.
// It populates the alert map with any errors encountered during the process and with any alerts related to low wallet balance and missing wallets.
// The alert map key is "Balance Check".
// It queries the database for the configuration of each layer and decodes it using the toml.Decode function.
// It then iterates over the addresses in the configuration and curates a list of unique addresses.
// If an address is not found in the chain node, it adds an alert to the alert map.
// If the balance of an address is below MinimumWalletBalance, it adds an alert to the alert map.
// If there are any errors encountered during the process, the err field of the alert map is populated.
func balanceCheck(al *alerts) {
	Name := Name_BalanceCheck
	al.alertMap[Name] = &alertOut{}

	var ret strings.Builder

	for _, addr := range al.walletAddrs {
		keyAddr, err := al.api.StateAccountKey(al.ctx, addr, types.EmptyTSK)
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting account key: %w", err)
			return
		}

		has, err := al.api.WalletHas(al.ctx, keyAddr)
		if err != nil {
			al.alertMap[Name].err = err
			return
		}

		if !has {
			fmt.Fprintf(&ret, "Wallet %s was not found in chain node. ", keyAddr)
		}

		balance, err := al.api.WalletBalance(al.ctx, addr)
		if err != nil {
			al.alertMap[Name].err = err
		}

		if abi.TokenAmount(al.cfg.MinimumWalletBalance).GreaterThanEqual(balance) {
			fmt.Fprintf(&ret, "Balance for wallet %s (%s) is below %s. ", addr, keyAddr, al.cfg.MinimumWalletBalance.Short())
		}
	}
	if ret.String() != "" {
		al.alertMap[Name].alertString = ret.String()
	}
}

// sealingTasks are sealing-pipeline task names; any single failure triggers an alert.
var sealingTasks = []string{
	tasknames.SDR,
	tasknames.TreeD,
	tasknames.TreeRC,
	tasknames.PreCommitBatch,
	tasknames.PoRep,
	tasknames.Finalize,
	tasknames.MoveStorage,
	tasknames.CommitBatch,
	tasknames.WdPost,
	tasknames.ParkPiece,
}

// pdpTasks are PDP v1 and v0 task names; any single failure triggers an alert.
var pdpTasks = []string{
	// PDP v1
	tasknames.PDPProve,
	tasknames.PDPAddPiece,
	tasknames.PDPDeletePiece,
	tasknames.PDPAddDataSet,
	tasknames.PDPDelDataSet,
	tasknames.PDPInitPP,
	tasknames.PDPProvingPeriod,
	tasknames.PDPNotify,
	tasknames.PDPCommP,
	tasknames.PDPSaveCache,
	tasknames.AggregatePDPDeal,
	// PDP v0
	tasknames.PDPv0_Prove,
	tasknames.PDPv0_PullPiece,
	tasknames.PDPv0_SaveCache,
	tasknames.PDPv0_InitPP,
	tasknames.PDPv0_ProvPeriod,
	tasknames.PDPv0_Notify,
	tasknames.PDPv0_DelDataSet,
	tasknames.PDPv0_TermFWSS,
}

// taskFailureCheckWith is the parameterized core shared by taskFailureCheck
// and pdpTaskFailureCheck. It queries harmony_task_history for failures over
// the given interval, alerts on any failure in sensitiveTasks, and alerts on
// >5 failures for all other tasks or machines.
func taskFailureCheckWith(al *alerts, name AlertName, interval time.Duration, sensitiveTasks []string) {
	al.alertMap[name] = &alertOut{}

	type taskFailure struct {
		Machine  string `db:"completed_by_host_and_port"`
		Name     string `db:"name"`
		Failures int    `db:"failed_count"`
	}

	var taskFailures []taskFailure

	err := al.db.Select(al.ctx, &taskFailures, `
								SELECT completed_by_host_and_port, name, COUNT(*) AS failed_count
								FROM harmony_task_history
								WHERE result = FALSE
								  AND work_end >= NOW() - $1::interval
								GROUP BY completed_by_host_and_port, name
								ORDER BY completed_by_host_and_port, name;`, fmt.Sprintf("%f Minutes", interval.Minutes()))
	if err != nil {
		al.alertMap[name].err = xerrors.Errorf("getting failed task count: %w", err)
		return
	}

	mmap := make(map[string]int)
	tmap := make(map[string]int)

	for _, tf := range taskFailures {
		tmap[tf.Name] += tf.Failures
		mmap[tf.Machine] += tf.Failures
	}

	for taskName, count := range tmap {
		if slices.Contains(sensitiveTasks, taskName) || count > 5 {
			al.alertMap[name].alertString += fmt.Sprintf("Task: %s, Failures: %d. ", taskName, count)
		}
	}

	for machine, count := range mmap {
		if count > 5 {
			al.alertMap[name].alertString += fmt.Sprintf("Machine: %s, Failures: %d. ", machine, count)
		}
	}
}

// taskFailureCheck checks all tasks over the last FullAlertInterval.
func taskFailureCheck(al *alerts) {
	taskFailureCheckWith(al, Name_TaskFailures, FullAlertInterval, sealingTasks)
}

// pdpTaskFailureCheck checks PDP (v1 and v0) tasks over the last
// AlertMangerInterval so failures surface on every ping-health cadence.
func pdpTaskFailureCheck(al *alerts) {
	taskFailureCheckWith(al, Name_PDPTaskFailures, AlertManagerInterval, pdpTasks)
}

// permanentStorageCheck retrieves the storage details from the database and checks if there is sufficient space for sealing sectors.
// It queries the database for the available storage for all storage paths that can store data.
// It queries the database for sectors being sealed that have not been finalized yet.
// For each sector, it calculates the required space for sealing based on the sector size.
// It checks if there is enough available storage for each sector and updates the sectorMap accordingly.
// If any sectors are unaccounted for, it calculates the total missing space and adds an alert to the alert map.
func permanentStorageCheck(al *alerts) {
	Name := Name_PermanentStorageSpace
	al.alertMap[Name] = &alertOut{}
	// Get all storage path for permanent storages
	type storage struct {
		ID        string `db:"storage_id"`
		Available int64  `db:"available"`
	}

	var storages []storage

	err := al.db.Select(al.ctx, &storages, `
								SELECT storage_id, available
								FROM storage_path
								WHERE can_store = TRUE;`)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting storage details: %w", err)
		return
	}

	type sector struct {
		Miner  abi.ActorID             `db:"sp_id"`
		Number abi.SectorNumber        `db:"sector_number"`
		Proof  abi.RegisteredSealProof `db:"reg_seal_proof"`
	}

	var sectors []sector

	err = al.db.Select(al.ctx, &sectors, `
								SELECT sp_id, sector_number, reg_seal_proof
								FROM sectors_sdr_pipeline
								WHERE after_move_storage = FALSE;`)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting sectors being sealed: %w", err)
		return
	}

	type sm struct {
		s    sector
		size int64
	}

	sectorMap := make(map[sm]bool)

	for _, sec := range sectors {
		space := int64(0)
		sec := sec
		sectorSize, err := sec.Proof.SectorSize()
		if err != nil {
			space = int64(64<<30)*2 + int64(200<<20) // Assume 64 GiB sector
		} else {
			space = int64(sectorSize)*2 + int64(200<<20) // sealed + unsealed + cache
		}

		key := sm{s: sec, size: space}

		sectorMap[key] = false

		for _, strg := range storages {
			if space <= strg.Available {
				strg.Available -= space
				sectorMap[key] = true
				break
			}
		}
	}

	missingSpace := big.NewInt(0)
	for sec, accounted := range sectorMap {
		if !accounted {
			big.Add(missingSpace, big.NewInt(sec.size))
		}
	}

	if missingSpace.GreaterThan(big.NewInt(0)) {
		al.alertMap[Name].alertString = fmt.Sprintf("Insufficient storage space for sealing sectors. Additional %s required.", humanize.Bytes(missingSpace.Uint64()))
	}
}

// getAddresses retrieves machine details from the database, stores them in an array and compares layers for uniqueness.
// It employs addrMap to handle unique addresses, and generated slices for configuration fields and MinerAddresses.
// The function iterates over layers, storing decoded configuration and verifying address existence in addrMap.
// It ends by setting unique addresses and miner slices in the alerts struct which others can reuse. This must be called before other alerts funcs.
func (al *alerts) getAddresses() error {
	// MachineDetails represents the structure of data received from the SQL query.
	type machineDetail struct {
		ID          int
		HostAndPort string
		Layers      string
	}
	var machineDetails []machineDetail

	// Get all layers in use
	err := al.db.Select(al.ctx, &machineDetails, `
				SELECT m.id, m.host_and_port, d.layers
				FROM harmony_machines m
				LEFT JOIN harmony_machine_details d ON m.id = d.machine_id;`)
	if err != nil {
		return xerrors.Errorf("getting config layers for all machines: %w", err)
	}

	// UniqueLayers takes an array of MachineDetails and returns a slice of unique layers.

	layerMap := make(map[string]bool)
	var uniqueLayers []string

	// Get unique layers in use
	for _, machine := range machineDetails {
		// Split the Layers field into individual layers
		layers := strings.SplitSeq(machine.Layers, ",")
		for layer := range layers {
			layer = strings.TrimSpace(layer)
			if _, exists := layerMap[layer]; !exists && layer != "" {
				layerMap[layer] = true
				uniqueLayers = append(uniqueLayers, layer)
			}
		}
	}

	addrMap := make(map[string]struct{})
	minerMap := make(map[string]struct{})

	if len(uniqueLayers) > 0 {
		type minimalAddressInfo struct {
			Addresses []config.CurioAddresses `toml:"Addresses"`
		}

		err = config.ForEachConfig[minimalAddressInfo](al.ctx, al.db, func(name string, info minimalAddressInfo) error {
			if !slices.Contains(uniqueLayers, name) {
				return nil
			}

			for i := range info.Addresses {
				prec := info.Addresses[i].PreCommitControl
				com := info.Addresses[i].CommitControl
				term := info.Addresses[i].TerminateControl
				miners := info.Addresses[i].MinerAddresses
				for j := range prec {
					if prec[j] != "" {
						addrMap[prec[j]] = struct{}{}
					}
				}
				for j := range com {
					if com[j] != "" {
						addrMap[com[j]] = struct{}{}
					}
				}
				for j := range term {
					if term[j] != "" {
						addrMap[term[j]] = struct{}{}
					}
				}
				for j := range miners {
					if miners[j] != "" {
						minerMap[miners[j]] = struct{}{}
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	var wallets, minerAddrs []address.Address

	// Get control and wallet addresses from chain
	for m := range minerMap {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return err
		}
		info, err := al.api.StateMinerInfo(al.ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}
		minerAddrs = append(minerAddrs, maddr)
		addrMap[info.Worker.String()] = struct{}{}
		for _, w := range info.ControlAddresses {
			if _, ok := addrMap[w.String()]; !ok {
				addrMap[w.String()] = struct{}{}
			}
		}
	}

	for w := range addrMap {
		waddr, err := address.NewFromString(w)
		if err != nil {
			return err
		}
		wallets = append(wallets, waddr)
	}

	al.minerAddrs = minerAddrs
	al.walletAddrs = wallets

	return nil
}

const windowPostFinalityBuffer abi.ChainEpoch = 5

func wdPostCheck(al *alerts) {
	Name := Name_WindowPost
	al.alertMap[Name] = &alertOut{}
	head, err := al.api.ChainHead(al.ctx)
	if err != nil {
		al.alertMap[Name].err = err
		return
	}

	// Calculate from epoch for last AlertMangerInterval
	from := max(head.Height()-abi.ChainEpoch(math.Ceil(FullAlertInterval.Seconds()/float64(build.BlockDelaySecs)))-1, 0)

	// Cache current deadline metadata per miner. We use it to derive closed
	// deadlines in the alert window without walking every tipset.
	type minerCheck struct {
		addr            address.Address
		id              uint64
		deadlineIndex   uint64
		deadlineCount   uint64
		periodStart     abi.ChainEpoch
		deadlineOpen    abi.ChainEpoch
		challengeWindow abi.ChainEpoch
		provingPeriod   abi.ChainEpoch
	}
	minerChecks := make([]minerCheck, 0, len(al.minerAddrs))
	for _, maddr := range al.minerAddrs {
		id, err := address.IDFromAddress(maddr)
		if err != nil {
			idAddr, err := al.api.StateLookupID(al.ctx, maddr, head.Key())
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("looking up miner ID address: %w", err)
				return
			}
			id, err = address.IDFromAddress(idAddr)
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("getting miner ID: %w", err)
				return
			}
		}

		deadlineInfo, err := al.api.StateMinerProvingDeadline(al.ctx, maddr, head.Key())
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting miner deadline: %w", err)
			return
		}
		if !deadlineInfo.PeriodStarted() {
			continue
		}

		minerChecks = append(minerChecks, minerCheck{
			addr:            maddr,
			id:              id,
			deadlineIndex:   deadlineInfo.Index,
			deadlineCount:   deadlineInfo.WPoStPeriodDeadlines,
			periodStart:     deadlineInfo.PeriodStart,
			deadlineOpen:    deadlineInfo.Open,
			challengeWindow: deadlineInfo.WPoStChallengeWindow,
			provingPeriod:   deadlineInfo.WPoStProvingPeriod,
		})
	}

	to := head.Height() - windowPostFinalityBuffer

	// One entry per closed deadline that needs a local proof check.
	type closedDeadlineCheck struct {
		minerID     uint64
		minerAddr   address.Address
		periodStart abi.ChainEpoch
		deadline    uint64
		closeEpoch  abi.ChainEpoch
	}
	deadlineChecks := make([]closedDeadlineCheck, 0)
	for _, minerInfo := range minerChecks {
		// Deadline math depends on actor metadata being internally consistent.
		if minerInfo.deadlineCount == 0 || minerInfo.challengeWindow <= 0 || minerInfo.provingPeriod != abi.ChainEpoch(minerInfo.deadlineCount)*minerInfo.challengeWindow {
			al.alertMap[Name].err = xerrors.Errorf("invalid deadline metadata for miner %s: deadlines=%d challengeWindow=%d provingPeriod=%d", minerInfo.addr, minerInfo.deadlineCount, minerInfo.challengeWindow, minerInfo.provingPeriod)
			return
		}

		// The current deadline opens exactly when the previous deadline closed.
		closeEpoch := minerInfo.deadlineOpen
		deadlineIndex := (minerInfo.deadlineIndex + minerInfo.deadlineCount - 1) % minerInfo.deadlineCount
		periodStart := minerInfo.periodStart
		if minerInfo.deadlineIndex == 0 {
			// Deadline 0 means the previous closed deadline was deadline 47 in
			// the previous proving period.
			periodStart -= minerInfo.provingPeriod
		}

		for closeEpoch > from {
			if closeEpoch <= to {
				deadlineChecks = append(deadlineChecks, closedDeadlineCheck{
					minerID:     minerInfo.id,
					minerAddr:   minerInfo.addr,
					periodStart: periodStart,
					deadline:    deadlineIndex,
					closeEpoch:  closeEpoch,
				})
			}

			// Step back one deadline window, wrapping to the previous proving
			// period when we cross deadline 0.
			closeEpoch -= minerInfo.challengeWindow
			if deadlineIndex == 0 {
				deadlineIndex = minerInfo.deadlineCount - 1
				periodStart -= minerInfo.provingPeriod
			} else {
				deadlineIndex--
			}
		}
	}

	type proofKey struct {
		minerID            uint64
		provingPeriodStart abi.ChainEpoch
		deadline           uint64
		partition          uint64
	}
	type expectedPost struct {
		key        proofKey
		minerAddr  address.Address
		closeEpoch abi.ChainEpoch
	}
	expectedPosts := make([]expectedPost, 0)
	expectedByPost := make(map[proofKey]expectedPost)

	// Load only current deadline state. Chain state is used
	// here only to find non-empty partitions; proof status comes from the DB.
	for _, check := range deadlineChecks {
		partitions, err := al.api.StateMinerPartitions(al.ctx, check.minerAddr, check.deadline, head.Key())
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting miner partitions: %w", err)
			return
		}

		for idx, partition := range partitions {
			empty, err := partition.LiveSectors.IsEmpty()
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("checking live sectors for miner %s deadline %d partition %d: %w", check.minerAddr, check.deadline, idx, err)
				return
			}
			if empty {
				continue
			}

			post := expectedPost{
				key: proofKey{
					minerID:            check.minerID,
					provingPeriodStart: check.periodStart,
					deadline:           check.deadline,
					partition:          uint64(idx),
				},
				minerAddr:  check.minerAddr,
				closeEpoch: check.closeEpoch,
			}
			if _, ok := expectedByPost[post.key]; ok {
				continue
			}
			expectedByPost[post.key] = post
			expectedPosts = append(expectedPosts, post)
		}
	}

	if len(expectedPosts) > 0 {
		spIDs := make([]int64, 0, len(expectedPosts))
		periodStarts := make([]int64, 0, len(expectedPosts))
		deadlines := make([]int64, 0, len(expectedPosts))
		partitions := make([]int64, 0, len(expectedPosts))
		for _, expected := range expectedPosts {
			spIDs = append(spIDs, int64(expected.key.minerID))
			periodStarts = append(periodStarts, int64(expected.key.provingPeriodStart))
			deadlines = append(deadlines, int64(expected.key.deadline))
			partitions = append(partitions, int64(expected.key.partition))
		}

		var localProofs []struct {
			Miner              int64          `db:"sp_id"`
			ProvingPeriodStart abi.ChainEpoch `db:"proving_period_start"`
			Deadline           int64          `db:"deadline"`
			Partition          int64          `db:"partition"`
			MessageCID         *string        `db:"message_cid"`
			WaitMessageCID     sql.NullString `db:"wait_message_cid"`
			ExecutedTSKCID     sql.NullString `db:"executed_tsk_cid"`
			ExecutedTSKEpoch   sql.NullInt64  `db:"executed_tsk_epoch"`
			ExitCode           sql.NullInt64  `db:"executed_rcpt_exitcode"`
			Proof              []byte         `db:"proof_params"`
		}

		// message_waits tells us whether a recorded message CID was tracked,
		// landed, and executed successfully.
		err = al.db.Select(al.ctx, &localProofs, `
			WITH expected(sp_id, proving_period_start, deadline, partition) AS (
					SELECT *
					FROM unnest($1::bigint[], $2::bigint[], $3::bigint[], $4::bigint[])
				)
				SELECT p.sp_id, p.proving_period_start, p.deadline, p.partition, p.message_cid, p.proof_params,
					mw.signed_message_cid AS wait_message_cid, mw.executed_tsk_cid, mw.executed_tsk_epoch, mw.executed_rcpt_exitcode
				FROM wdpost_proofs p
				JOIN expected e USING (sp_id, proving_period_start, deadline, partition)
				LEFT JOIN message_waits mw ON mw.signed_message_cid = p.message_cid;`,
			spIDs, periodStarts, deadlines, partitions)
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting local WindowPost proofs: %w", err)
			return
		}

		type localProof struct {
			messageCID     string
			waitTracked    bool
			executedTSKCID sql.NullString
			executedEpoch  sql.NullInt64
			exitCode       sql.NullInt64
		}
		localProofByPost := make(map[proofKey]localProof, len(localProofs))
		for _, proof := range localProofs {
			if proof.Deadline < 0 || proof.Partition < 0 {
				continue
			}
			key := proofKey{
				minerID:            uint64(proof.Miner),
				provingPeriodStart: proof.ProvingPeriodStart,
				deadline:           uint64(proof.Deadline),
				partition:          uint64(proof.Partition),
			}
			messageCID := ""
			if proof.MessageCID != nil {
				messageCID = strings.TrimSpace(*proof.MessageCID)
			}
			localProofByPost[key] = localProof{
				messageCID:     messageCID,
				waitTracked:    proof.WaitMessageCID.Valid && strings.TrimSpace(proof.WaitMessageCID.String) != "",
				executedTSKCID: proof.ExecutedTSKCID,
				executedEpoch:  proof.ExecutedTSKEpoch,
				exitCode:       proof.ExitCode,
			}

			expected, ok := expectedByPost[key]
			if !ok {
				continue
			}
			var postOut miner.SubmitWindowedPoStParams
			err = postOut.UnmarshalCBOR(bytes.NewReader(proof.Proof))
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("unmarshaling windowPost proof params: %w", err)
				return
			}
			for _, postedPartition := range postOut.Partitions {
				skipped, err := postedPartition.Skipped.Count()
				if err != nil {
					al.alertMap[Name].err = xerrors.Errorf("getting skipped sector count: %w", err)
					return
				}
				if skipped > 0 {
					al.alertMap[Name].alertString += fmt.Sprintf("Miner %s skipped %d sectors in deadline %d partition %d. ", expected.minerAddr.String(), skipped, postOut.Deadline, postedPartition.Index)
				}
			}
		}

		// Classify each expected proof by the furthest local state it reached.
		for _, expected := range expectedPosts {
			proof, ok := localProofByPost[expected.key]
			if !ok {
				al.alertMap[Name].alertString += fmt.Sprintf("No WindowPost proof found for miner %s deadline %d partition %d (deadline closed at epoch %d; no local WindowPost proof was recorded). ", expected.minerAddr.String(), expected.key.deadline, expected.key.partition, expected.closeEpoch)
				continue
			}
			if proof.messageCID == "" {
				al.alertMap[Name].alertString += fmt.Sprintf("WindowPost proof found for miner %s deadline %d partition %d, but no local message CID was recorded (deadline closed at epoch %d; proof was created but not sent). ", expected.minerAddr.String(), expected.key.deadline, expected.key.partition, expected.closeEpoch)
				continue
			}
			if !proof.waitTracked {
				al.alertMap[Name].alertString += fmt.Sprintf("WindowPost proof message %s for miner %s deadline %d partition %d was not found in message_waits (deadline closed at epoch %d; message tracking is missing). ", proof.messageCID, expected.minerAddr.String(), expected.key.deadline, expected.key.partition, expected.closeEpoch)
				continue
			}
			if !proof.executedTSKCID.Valid || strings.TrimSpace(proof.executedTSKCID.String) == "" {
				al.alertMap[Name].alertString += fmt.Sprintf("WindowPost proof message %s for miner %s deadline %d partition %d has not landed on chain (deadline closed at epoch %d). ", proof.messageCID, expected.minerAddr.String(), expected.key.deadline, expected.key.partition, expected.closeEpoch)
				continue
			}
			if !proof.exitCode.Valid {
				al.alertMap[Name].alertString += fmt.Sprintf("WindowPost proof message %s for miner %s deadline %d partition %d landed, but no receipt exit code was recorded. ", proof.messageCID, expected.minerAddr.String(), expected.key.deadline, expected.key.partition)
				continue
			}
			if proof.exitCode.Int64 != 0 {
				if proof.executedEpoch.Valid {
					al.alertMap[Name].alertString += fmt.Sprintf("WindowPost proof message %s for miner %s deadline %d partition %d failed on chain at epoch %d with exit code %d. ", proof.messageCID, expected.minerAddr.String(), expected.key.deadline, expected.key.partition, proof.executedEpoch.Int64, proof.exitCode.Int64)
				} else {
					al.alertMap[Name].alertString += fmt.Sprintf("WindowPost proof message %s for miner %s deadline %d partition %d failed on chain with exit code %d. ", proof.messageCID, expected.minerAddr.String(), expected.key.deadline, expected.key.partition, proof.exitCode.Int64)
				}
			}
		}
	}

	// Check for new faults in deadlines that recently closed
	for _, minerInfo := range minerChecks {
		if minerInfo.deadlineCount == 0 {
			continue
		}
		// Check the deadline that just closed (current - 1)
		prevDeadline := (minerInfo.deadlineIndex + minerInfo.deadlineCount - 1) % minerInfo.deadlineCount
		partitions, err := al.api.StateMinerPartitions(al.ctx, minerInfo.addr, prevDeadline, head.Key())
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting partitions for fault check: %w", err)
			return
		}

		for pidx, partition := range partitions {
			faultyCount, err := partition.FaultySectors.Count()
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("counting faulty sectors: %w", err)
				return
			}
			if faultyCount > 0 {
				recoveringCount, err := partition.RecoveringSectors.Count()
				if err != nil {
					al.alertMap[Name].err = xerrors.Errorf("counting recovering sectors: %w", err)
					return
				}
				unrecovered := faultyCount - recoveringCount
				if unrecovered > 0 {
					al.alertMap[Name].alertString += fmt.Sprintf("Miner %s deadline %d partition %d has %d faulty sectors (%d not recovering). ", minerInfo.addr.String(), prevDeadline, pidx, faultyCount, unrecovered)
				}
			}
		}
	}
}

func wnPostCheck(al *alerts) {
	Name := Name_WinningPost
	al.alertMap[Name] = &alertOut{}

	miners := al.minerAddrs

	if len(miners) == 0 {
		return
	}

	head, err := al.api.ChainHead(al.ctx)
	if err != nil {
		al.alertMap[Name].err = err
		return
	}

	// Calculate from epoch for last AlertMangerInterval
	from := max(head.Height()-abi.ChainEpoch(math.Ceil(FullAlertInterval.Seconds()/float64(build.BlockDelaySecs)))-1, 0)

	var wnDetails []struct {
		Miner    int64          `db:"sp_id"`
		Block    string         `db:"mined_cid"`
		Epoch    abi.ChainEpoch `db:"epoch"`
		Included *bool          `db:"included"` // pointer to handle NULL
	}

	// Get all DB entries where we won the election in last AlertMangerInterval
	// Only get entries where inclusion has been checked (included IS NOT NULL)
	err = al.db.Select(al.ctx, &wnDetails, `
			SELECT sp_id, mined_cid, epoch, included 
			FROM mining_tasks 
			WHERE epoch > $1 AND won = TRUE 
			ORDER BY epoch;`, from)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting winningPost details from database: %w", err)
		return
	}

	// Get count of all mining tasks in DB in last AlertMangerInterval
	var count int64
	err = al.db.QueryRow(al.ctx, `
			SELECT COUNT(*)
			FROM mining_tasks 
			WHERE epoch > $1;`, from).Scan(&count)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting winningPost count details from database: %w", err)
		return
	}

	// If we have no task created for any miner ID, this is a serious issue
	if count == 0 {
		al.alertMap[Name].alertString += "No winningPost tasks found in the last " + humanize.Time(time.Now().Add(-FullAlertInterval))
		return
	}

	// Calculate how many tasks should be in DB for AlertMangerInterval (epochs) as each epoch should have 1 task
	expected := int64(math.Ceil(FullAlertInterval.Seconds() / float64(build.BlockDelaySecs)))
	if (head.Height() - abi.ChainEpoch(expected)) < 0 {
		expected = int64(head.Height())
	}

	const slack = 4
	slackTasks := slack * int64(len(miners))

	expected = expected * int64(len(miners)) // Multiply epochs by number of miner IDs

	if count < expected-slackTasks || count > expected+slackTasks {
		al.alertMap[Name].alertString += fmt.Sprintf("Expected %d WinningPost task and found %d in DB. ", expected, count)
	}

	if len(wnDetails) < 1 {
		return
	}

	// Report any block which we submitted but was not included in the chain
	// Only report if inclusion has actually been checked (included != NULL) and is false
	// Also only report for blocks that are old enough to have been checked (>= MinInclusionEpochs old)
	const minInclusionEpochs = 5 // same as in inclusion_check_task.go
	for _, wn := range wnDetails {
		// Skip if inclusion hasn't been checked yet
		if wn.Included == nil {
			continue
		}
		// Skip if block is too recent to have been checked reliably
		if int64(head.Height())-int64(wn.Epoch) < minInclusionEpochs+3 {
			continue
		}
		if !*wn.Included {
			al.alertMap[Name].alertString += fmt.Sprintf("Epoch %d: does not contain our block %s. ", wn.Epoch, wn.Block)
		}
	}
}

func chainSyncCheck(al *alerts) {
	Name := Name_ChainSync
	al.alertMap[Name] = &alertOut{}

	type minimalApiInfo struct {
		Apis struct {
			ChainApiInfo []string
		}
	}

	var rpcInfos []string // config name -> api info

	err := config.ForEachConfig[minimalApiInfo](al.ctx, al.db, func(name string, info minimalApiInfo) error {
		rpcInfos = append(rpcInfos, info.Apis.ChainApiInfo...)
		return nil
	})

	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting RPC API info: %w", err)
		return
	}

	dedup := map[string]bool{} // for dedup by address

	// For each unique API (chain), check if in sync
	for _, info := range lists.UniqNoAlloc(rpcInfos) {
		ai := cliutil.ParseApiInfo(info)
		if dedup[ai.Addr] {
			continue
		}
		dedup[ai.Addr] = true

		addr, err := ai.DialArgs("v1")
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("could not get DialArgs: %w", err)
			continue
		}

		var res api.ChainStruct
		closer, err := jsonrpc.NewMergeClient(al.ctx, addr, "Filecoin",
			api.GetInternalStructs(&res), ai.AuthHeader(), []jsonrpc.Option{jsonrpc.WithErrors(jsonrpc.NewErrors())}...)
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("error creating jsonrpc client: %v", err)
			continue
		}
		defer closer()

		full := &res

		head, err := full.ChainHead(al.ctx)
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("ChainHead: %w", err)
			continue
		}

		switch {
		case time.Now().Unix()-int64(head.MinTimestamp()) < int64(build.BlockDelaySecs*3/2): // within 1.5 epochs
			continue
		case time.Now().Unix()-int64(head.MinTimestamp()) < int64(build.BlockDelaySecs*5): // within 5 epochs
			log.Debugf("Chain Sync status: %s: slow (%s behind)", addr, time.Since(time.Unix(int64(head.MinTimestamp()), 0)).Truncate(time.Second))
		default:
			al.alertMap[Name].alertString += fmt.Sprintf("behind (%s behind)", time.Since(time.Unix(int64(head.MinTimestamp()), 0)).Truncate(time.Second))
		}
	}
}

func missingSectorCheck(al *alerts) {
	Name := Name_MissingSectors
	al.alertMap[Name] = &alertOut{}

	var sectors []struct {
		MinerID  int64 `db:"miner_id"`
		SectorID int64 `db:"sector_num"`
	}

	err := al.db.Select(al.ctx, &sectors, `SELECT miner_id, sector_num  FROM sector_location WHERE sector_filetype = ANY(ARRAY[2,8]) GROUP BY miner_id, sector_num ORDER BY miner_id, sector_num`)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting sealed sectors from database: %w", err)
		return
	}

	dbMap := map[address.Address]*bitfield.BitField{}
	minerMap := map[int64]address.Address{}

	for _, sector := range sectors {
		m, ok := minerMap[sector.MinerID]
		if !ok {
			m, err = address.NewIDAddress(uint64(sector.MinerID))
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("parsing miner address: %w", err)
				return
			}
			minerMap[sector.MinerID] = m
		}
		bf, ok := dbMap[m]
		if !ok {
			newbf := bitfield.New()
			newbf.Set(uint64(sector.SectorID))
			dbMap[m] = &newbf
			continue
		}
		bf.Set(uint64(sector.SectorID))
	}

	head, err := al.api.ChainHead(al.ctx)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("ChainHead: %w", err)
		return
	}

	for _, m := range minerMap {
		mact, err := al.api.StateGetActor(al.ctx, m, head.Key())
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting miner actor %s: %w", m.String(), err)
			return
		}
		tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(al.api), blockstore.NewMemory())
		mas, err := miner.Load(adt.WrapStore(al.ctx, cbor.NewCborStore(tbs)), mact)
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("loading miner %s: %w", m.String(), err)
			return
		}

		liveSectors, err := miner.AllPartSectors(mas, miner.Partition.LiveSectors)
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting live sectors for miner %s: %w", m.String(), err)
			return
		}

		diff, err := bitfield.SubtractBitField(liveSectors, *dbMap[m])
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("subtracting live sectors for miner %s: %w", m.String(), err)
		}
		err = diff.ForEach(func(i uint64) error {
			al.alertMap[Name].alertString += fmt.Sprintf("Missing sector %d in storage for miner %s. ", i, m.String())
			return nil
		})
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting missing sectors for miner %s: %w", m.String(), err)
			return
		}
	}
}

func pendingMessagesCheck(al *alerts) {
	Name := Name_PendingMessages
	al.alertMap[Name] = &alertOut{}

	var messages []struct {
		MessageCid string    `db:"signed_message_cid"`
		AddedAt    time.Time `db:"created_at"`
	}

	err := al.db.Select(al.ctx, &messages, `SELECT signed_message_cid, created_at FROM message_waits WHERE executed_tsk_cid IS NULL ORDER BY created_at DESC`)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting pending messages: %w", err)
	}

	if len(messages) == 0 {
		return
	}

	var msgs []string

	for _, msg := range messages {
		if time.Since(msg.AddedAt) > time.Hour {
			msgs = append(msgs, msg.MessageCid)
		}
	}

	if len(msgs) > 0 {
		al.alertMap[Name].alertString += fmt.Sprintf("Messages pending for more than 1 hour: %s", strings.Join(msgs, ", "))
	}
}

type AddrInfo struct {
	ID    string   `json:"ID"`
	Addrs []string `json:"Addrs"`
}

type Advertisement struct {
	Slash string `json:"/"`
}

type ParsedResponse struct {
	AddrInfo              AddrInfo       `json:"AddrInfo"`
	LastAdvertisement     Advertisement  `json:"LastAdvertisement"`
	LastAdvertisementTime time.Time      `json:"LastAdvertisementTime"`
	Publisher             AddrInfo       `json:"Publisher"`
	ExtendedProviders     map[string]any `json:"ExtendedProviders"`
	FrozenAt              string         `json:"FrozenAt"`
	LastError             string         `json:"LastError"`
}

func ipniSyncCheck(al *alerts) {
	Name := Name_IPNISync
	al.alertMap[Name] = &alertOut{}

	var summary []struct {
		SpId   int64  `db:"sp_id"`
		PeerID string `db:"peer_id"`
		Head   string `db:"head"`
		Miner  string
	}

	err := al.db.Select(al.ctx, &summary, `SELECT 
												ipp.sp_id,
												ipp.peer_id,
												ih.head
												FROM ipni_peerid ipp
												LEFT JOIN ipni_head ih ON ipp.peer_id = ih.provider`)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("failed to fetch the provider details from DB: %w", err)
		return
	}

	for i := range summary {
		if summary[i].SpId <= 0 {
			summary[i].Miner = "PDP"
		} else {
			maddr, err := address.NewIDAddress(uint64(summary[i].SpId))
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("failed to parse miner address: %w", err)
				return
			}
			summary[i].Miner = maddr.String()
		}
	}

	type minimalIpniInfo struct {
		Market struct {
			StorageMarketConfig struct {
				IPNI struct {
					ServiceURL []string
				}
			}
		}
	}

	var services []string

	err = config.ForEachConfig[minimalIpniInfo](al.ctx, al.db, func(name string, info minimalIpniInfo) error {
		services = append(services, info.Market.StorageMarketConfig.IPNI.ServiceURL...)
		return nil
	})

	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting IPNI services: %w", err)
		return
	}

	for _, service := range lists.UniqNoAlloc(services) {
		for _, d := range summary {
			url := service + "/providers/" + d.PeerID
			resp, err := http.Get(url)
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("Failed to fetch data from IPNI service: %s\n", err)
				return
			}
			defer func(Body io.ReadCloser) {
				_ = Body.Close()
			}(resp.Body)
			if resp.StatusCode == http.StatusNotFound {
				al.alertMap[Name].alertString += fmt.Sprintf("PeerID %s for provider %s not found in IPNI service: %s\n", d.PeerID, d.Miner, url)
			}
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
				al.alertMap[Name].err = xerrors.Errorf("Failed to fetch data from IPNI service: %s\n", resp.Status)
				return
			}
			out, err := io.ReadAll(resp.Body)
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("Failed to read response body: %w", err)
				return
			}
			var parsed ParsedResponse
			err = json.Unmarshal(out, &parsed)
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("Failed to unmarshal IPNI service response: %w", err)
				return
			}
			if parsed.LastAdvertisement.Slash == d.Head {
				continue
			}

			if parsed.LastError != "" {
				al.alertMap[Name].alertString += fmt.Sprintf("IPNI service %s is not synced for %s provider: %s\n", service, d.Miner, parsed.LastError)
				continue
			}

			if parsed.LastAdvertisementTime.IsZero() {
				al.alertMap[Name].alertString += fmt.Sprintf("IPNI service %s is not synced for %s provider: LastAdvertisementTime is zero\n", service, d.Miner)
				continue
			}

			if parsed.LastAdvertisementTime.Before(time.Now().Add(-1 * time.Hour)) {
				al.alertMap[Name].alertString += fmt.Sprintf("IPNI service %s is not synced for %s provider in last 1 hour: LastAdvertisementTime is too old\n", service, d.Miner)
				continue
			}
		}
	}
}
