package alertmanager

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/dustin/go-humanize"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
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
	Name := "NowCheck"
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
	Name := "Balance Check"
	al.alertMap[Name] = &alertOut{}

	var ret string

	uniqueAddrs, _, err := al.getAddresses()
	if err != nil {
		al.alertMap[Name].err = err
		return
	}

	for _, addr := range uniqueAddrs {
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
			ret += fmt.Sprintf("Wallet %s was not found in chain node. ", keyAddr)
		}

		balance, err := al.api.WalletBalance(al.ctx, addr)
		if err != nil {
			al.alertMap[Name].err = err
		}

		if abi.TokenAmount(al.cfg.MinimumWalletBalance).GreaterThanEqual(balance) {
			ret += fmt.Sprintf("Balance for wallet %s (%s) is below 5 Fil. ", addr, keyAddr)
		}
	}
	if ret != "" {
		al.alertMap[Name].alertString = ret
	}
}

// taskFailureCheck retrieves the task failure counts from the database for a specific time period.
// It then checks for specific sealing tasks and tasks with more than 5 failures to generate alerts.
func taskFailureCheck(al *alerts) {
	Name := "TaskFailures"
	al.alertMap[Name] = &alertOut{}

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
								ORDER BY completed_by_host_and_port, name;`, fmt.Sprintf("%f Minutes", AlertMangerInterval.Minutes()))
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting failed task count: %w", err)
		return
	}

	mmap := make(map[string]int)
	tmap := make(map[string]int)

	if len(taskFailures) > 0 {
		for _, tf := range taskFailures {
			_, ok := tmap[tf.Name]
			if !ok {
				tmap[tf.Name] = tf.Failures
			} else {
				tmap[tf.Name] += tf.Failures
			}
			_, ok = mmap[tf.Machine]
			if !ok {
				mmap[tf.Machine] = tf.Failures
			} else {
				mmap[tf.Machine] += tf.Failures
			}
		}
	}

	sealingTasks := []string{"SDR", "TreeD", "TreeRC", "PreCommitSubmit", "PoRep", "Finalize", "MoveStorage", "CommitSubmit", "WdPost", "ParkPiece"}
	contains := func(s []string, e string) bool {
		for _, a := range s {
			if a == e {
				return true
			}
		}
		return false
	}

	// Alerts for any sealing pipeline failures. Other tasks should have at least 5 failures for an alert
	for name, count := range tmap {
		if contains(sealingTasks, name) {
			al.alertMap[Name].alertString += fmt.Sprintf("Task: %s, Failures: %d. ", name, count)
		}
		if count > 5 {
			al.alertMap[Name].alertString += fmt.Sprintf("Task: %s, Failures: %d. ", name, count)
		}
	}

	// Alert if a machine failed more than 5 tasks
	for name, count := range tmap {
		if count > 5 {
			al.alertMap[Name].alertString += fmt.Sprintf("Machine: %s, Failures: %d. ", name, count)
		}
	}
}

// permanentStorageCheck retrieves the storage details from the database and checks if there is sufficient space for sealing sectors.
// It queries the database for the available storage for all storage paths that can store data.
// It queries the database for sectors being sealed that have not been finalized yet.
// For each sector, it calculates the required space for sealing based on the sector size.
// It checks if there is enough available storage for each sector and updates the sectorMap accordingly.
// If any sectors are unaccounted for, it calculates the total missing space and adds an alert to the alert map.
func permanentStorageCheck(al *alerts) {
	Name := "PermanentStorageSpace"
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
// It ends by returning unique addresses and miner slices.
func (al *alerts) getAddresses() ([]address.Address, []address.Address, error) {
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
		return nil, nil, xerrors.Errorf("getting config layers for all machines: %w", err)
	}

	// UniqueLayers takes an array of MachineDetails and returns a slice of unique layers.

	layerMap := make(map[string]bool)
	var uniqueLayers []string

	// Get unique layers in use
	for _, machine := range machineDetails {
		machine := machine
		// Split the Layers field into individual layers
		layers := strings.Split(machine.Layers, ",")
		for _, layer := range layers {
			layer = strings.TrimSpace(layer)
			if _, exists := layerMap[layer]; !exists && layer != "" {
				layerMap[layer] = true
				uniqueLayers = append(uniqueLayers, layer)
			}
		}
	}

	addrMap := make(map[string]struct{})
	minerMap := make(map[string]struct{})

	// Get all unique addresses
	for _, layer := range uniqueLayers {
		text := ""
		cfg := config.DefaultCurioConfig()
		err := al.db.QueryRow(al.ctx, `SELECT config FROM harmony_config WHERE title=$1`, layer).Scan(&text)
		if err != nil {
			if strings.Contains(err.Error(), pgx.ErrNoRows.Error()) {
				return nil, nil, xerrors.Errorf("missing layer '%s' ", layer)
			}
			return nil, nil, xerrors.Errorf("could not read layer '%s': %w", layer, err)
		}

		err = config.FixTOML(text, cfg)
		if err != nil {
			return nil, nil, err
		}

		_, err = config.TransparentDecode(text, cfg)
		if err != nil {
			return nil, nil, xerrors.Errorf("could not read layer, bad toml %s: %w", layer, err)
		}

		addrs := cfg.Addresses.Get()
		for i := range addrs {
			prec := addrs[i].PreCommitControl
			com := addrs[i].CommitControl
			term := addrs[i].TerminateControl
			miners := addrs[i].MinerAddresses
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
	}

	var wallets, minerAddrs []address.Address

	// Get control and wallet addresses from chain
	for m := range minerMap {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return nil, nil, err
		}
		info, err := al.api.StateMinerInfo(al.ctx, maddr, types.EmptyTSK)
		if err != nil {
			return nil, nil, err
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
			return nil, nil, err
		}
		wallets = append(wallets, waddr)
	}

	return wallets, minerAddrs, nil
}

func wdPostCheck(al *alerts) {
	Name := "WindowPost"
	al.alertMap[Name] = &alertOut{}
	head, err := al.api.ChainHead(al.ctx)
	if err != nil {
		al.alertMap[Name].err = err
		return
	}

	// Calculate from epoch for last AlertMangerInterval
	from := head.Height() - abi.ChainEpoch(math.Ceil(AlertMangerInterval.Seconds()/float64(build.BlockDelaySecs))) - 1
	if from < 0 {
		from = 0
	}

	_, miners, err := al.getAddresses()
	if err != nil {
		al.alertMap[Name].err = err
		return
	}

	// Start from the newest finalized tipset.
	h, err := al.api.ChainGetTipSet(al.ctx, head.Parents())
	if err != nil {
		al.alertMap[Name].err = err
		return
	}

	// Map[Miner Address]Map[DeadlineIdx]deadlinePartitionInfo
	type deadlinePartitionInfo struct {
		partitions     []bool // true if proof submitted
		closeEpoch     abi.ChainEpoch
		periodStart    abi.ChainEpoch
		deadlineClosed bool
	}
	msgCheck := make(map[address.Address]map[uint64]*deadlinePartitionInfo)

	// Walk back all tipset from current height to from height and find all deadlines and their partitions
	for h.Height() >= from {
		for _, maddr := range miners {
			deadlineInfo, err := al.api.StateMinerProvingDeadline(al.ctx, maddr, h.Key())
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("getting miner deadline: %w", err)
				return
			}
			partitions, err := al.api.StateMinerPartitions(al.ctx, maddr, deadlineInfo.Index, h.Key())
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("getting miner partitions: %w", err)
				return
			}
			if _, ok := msgCheck[maddr]; !ok {
				msgCheck[maddr] = make(map[uint64]*deadlinePartitionInfo)
			}
			if _, ok := msgCheck[maddr][deadlineInfo.Index]; !ok {
				ps := make([]bool, len(partitions))
				msgCheck[maddr][deadlineInfo.Index] = &deadlinePartitionInfo{
					partitions:     ps,
					closeEpoch:     deadlineInfo.Close,
					periodStart:    deadlineInfo.PeriodStart,
					deadlineClosed: head.Height() > deadlineInfo.Close,
				}
			}
		}
		h, err = al.api.ChainGetTipSet(al.ctx, h.Parents())
		if err != nil {
			al.alertMap[Name].err = err
			return
		}
	}

	// Get all wdPost tasks from DB between from and head
	var wdDetails []struct {
		Miner     int64          `db:"sp_id"`
		Deadline  int64          `db:"deadline"`
		Partition int64          `db:"partition"`
		Epoch     abi.ChainEpoch `db:"submit_at_epoch"`
		Proof     []byte         `db:"proof_params"`
	}

	err = al.db.Select(al.ctx, &wdDetails, `
				SELECT sp_id, submit_at_epoch, proof_params, partition, deadline
				FROM wdpost_proofs 
				WHERE submit_at_epoch > $1;`, from)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting windowPost details from database: %w", err)
		return
	}

	// For all tasks between from and head, match how many we posted successfully
	for _, detail := range wdDetails {
		addr, err := address.NewIDAddress(uint64(detail.Miner))
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting miner address: %w", err)
			return
		}
		dlInfo, ok := msgCheck[addr][uint64(detail.Deadline)]
		if !ok {
			// Don't alert for unknown deadlines - may be from a previous period
			continue
		}

		if int(detail.Partition) >= len(dlInfo.partitions) {
			continue // partition index out of range, skip
		}

		// If entry for a partition is found we should mark it as processed
		dlInfo.partitions[detail.Partition] = true

		// Check if we skipped any sectors
		var postOut miner.SubmitWindowedPoStParams
		err = postOut.UnmarshalCBOR(bytes.NewReader(detail.Proof))
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("unmarshaling windowPost proof params: %w", err)
			return
		}

		for i := range postOut.Partitions {
			c, err := postOut.Partitions[i].Skipped.Count()
			if err != nil {
				al.alertMap[Name].err = xerrors.Errorf("getting skipped sector count: %w", err)
				return
			}
			if c > 0 {
				al.alertMap[Name].alertString += fmt.Sprintf("Miner %s skipped %d sectors in deadline %d partition %d. ", addr.String(), c, postOut.Deadline, postOut.Partitions[i].Index)
			}
		}
	}

	// Check if we missed any deadline/partitions - ONLY for deadlines that have CLOSED
	for maddr, deadlines := range msgCheck {
		for deadlineIndex, dlInfo := range deadlines {
			// Only alert for closed deadlines - open deadlines may not have proofs yet
			if !dlInfo.deadlineClosed {
				continue
			}
			for idx, submitted := range dlInfo.partitions {
				if !submitted {
					al.alertMap[Name].alertString += fmt.Sprintf("No WindowPost proof found for miner %s deadline %d partition %d (deadline closed at epoch %d). ", maddr.String(), deadlineIndex, idx, dlInfo.closeEpoch)
				}
			}
		}
	}

	// Check for new faults in deadlines that recently closed
	for _, maddr := range miners {
		deadlineInfo, err := al.api.StateMinerProvingDeadline(al.ctx, maddr, head.Key())
		if err != nil {
			al.alertMap[Name].err = xerrors.Errorf("getting miner deadline for fault check: %w", err)
			return
		}

		// Check the deadline that just closed (current - 1)
		prevDeadline := (deadlineInfo.Index + 47) % 48 // wrap around
		partitions, err := al.api.StateMinerPartitions(al.ctx, maddr, prevDeadline, head.Key())
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
					al.alertMap[Name].alertString += fmt.Sprintf("Miner %s deadline %d partition %d has %d faulty sectors (%d not recovering). ", maddr.String(), prevDeadline, pidx, faultyCount, unrecovered)
				}
			}
		}
	}
}

func wnPostCheck(al *alerts) {
	Name := "WinningPost"
	al.alertMap[Name] = &alertOut{}
	head, err := al.api.ChainHead(al.ctx)
	if err != nil {
		al.alertMap[Name].err = err
		return
	}

	// Calculate from epoch for last AlertMangerInterval
	from := head.Height() - abi.ChainEpoch(math.Ceil(AlertMangerInterval.Seconds()/float64(build.BlockDelaySecs))) - 1
	if from < 0 {
		from = 0
	}

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
		al.alertMap[Name].alertString += "No winningPost tasks found in the last " + humanize.Time(time.Now().Add(-AlertMangerInterval))
		return
	}

	// Calculate how many tasks should be in DB for AlertMangerInterval (epochs) as each epoch should have 1 task
	expected := int64(math.Ceil(AlertMangerInterval.Seconds() / float64(build.BlockDelaySecs)))
	if (head.Height() - abi.ChainEpoch(expected)) < 0 {
		expected = int64(head.Height())
	}

	_, miners, err := al.getAddresses()
	if err != nil {
		al.alertMap[Name].err = err
		return
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
	Name := "ChainSync"
	al.alertMap[Name] = &alertOut{}

	type minimalApiInfo struct {
		Apis struct {
			ChainApiInfo []string
		}
	}

	rpcInfos := map[string]minimalApiInfo{} // config name -> api info
	confNameToAddr := map[string]string{}   // config name -> api address

	// Get all config from DB
	rows, err := al.db.Query(al.ctx, `SELECT title, config FROM harmony_config`)
	if err != nil {
		al.alertMap[Name].err = xerrors.Errorf("getting db configs: %w", err)
		return
	}

	configs := make(map[string]string)
	for rows.Next() {
		var title, cfg string
		if err := rows.Scan(&title, &cfg); err != nil {
			al.alertMap[Name].err = xerrors.Errorf("scanning db configs: %w", err)
			return
		}
		configs[title] = cfg
	}

	// Parse all configs minimal to get API
	for name, tomlStr := range configs {
		var info minimalApiInfo
		if err := toml.Unmarshal([]byte(tomlStr), &info); err != nil {
			al.alertMap[Name].err = xerrors.Errorf("unmarshaling %s config: %w", name, err)
			continue
		}

		if len(info.Apis.ChainApiInfo) == 0 {
			continue
		}

		rpcInfos[name] = info

		for _, addr := range info.Apis.ChainApiInfo {
			ai := cliutil.ParseApiInfo(addr)
			confNameToAddr[name] = ai.Addr
		}
	}

	dedup := map[string]bool{} // for dedup by address

	// For each unique API (chain), check if in sync
	for _, info := range rpcInfos {
		ai := cliutil.ParseApiInfo(info.Apis.ChainApiInfo[0])
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
	Name := "MissingSectors"
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
	Name := "PendingMessages"
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
