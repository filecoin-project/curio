//go:build integration

package storageingest

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	storageTransferITestOptInEnv    = "CURIO_STORAGE_TRANSFER_ITEST"
	storageTransferITestHostEnv     = "CURIO_STORAGE_TRANSFER_ITEST_HOST"
	storageTransferITestPortEnv     = "CURIO_STORAGE_TRANSFER_ITEST_PORT"
	storageTransferITestDBEnv       = "CURIO_STORAGE_TRANSFER_ITEST_DATABASE"
	storageTransferITestUserEnv     = "CURIO_STORAGE_TRANSFER_ITEST_USERNAME"
	storageTransferITestPasswordEnv = "CURIO_STORAGE_TRANSFER_ITEST_PASSWORD"
	storageTransferITestSectorSize  = int64(2 << 10)
)

// storageTransferITestDB connects only after explicit opt-in and only to an
// explicit literal loopback target. It deliberately constructs Config
// directly instead of inheriting Curio's normal database environment or the
// defaults in DefaultItestOptions. ReadOnly suppresses migration replay; it
// does not prevent test transactions after the connection is established.
func storageTransferITestDB(t *testing.T) (*harmonydb.DB, harmonydb.Config) {
	t.Helper()
	if os.Getenv(storageTransferITestOptInEnv) != "1" {
		t.Skipf("set %s=1 and all dedicated target variables to run", storageTransferITestOptInEnv)
	}

	host := requireStorageTransferITestEnv(t, storageTransferITestHostEnv)
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("%s must be a literal loopback IP address, got %q", storageTransferITestHostEnv, host)
	}
	port := requireStorageTransferITestEnv(t, storageTransferITestPortEnv)
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		t.Fatalf("%s must be a valid nonzero TCP port, got %q", storageTransferITestPortEnv, port)
	}

	id := harmonydb.ITestNewID()
	cfg := harmonydb.Config{
		Hosts:           []string{host},
		Port:            port,
		Database:        requireStorageTransferITestEnv(t, storageTransferITestDBEnv),
		Username:        requireStorageTransferITestEnv(t, storageTransferITestUserEnv),
		Password:        requireStorageTransferITestEnv(t, storageTransferITestPasswordEnv),
		LoadBalance:     false,
		ReadOnly:        true,
		SSLMode:         "disable",
		ApplicationName: "curio-storage-transfer-itest",
		ITestID:         id,
		UseTemplate:     false,
	}
	db, err := harmonydb.NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("opening isolated storage-transfer test schema: %v", err)
	}
	t.Cleanup(db.ITestDeleteAll)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, storageTransferITestSchema); err != nil {
		t.Fatalf("creating focused storage-transfer fixture: %v", err)
	}

	var version, displayedIsolation string
	if err := db.QueryRow(ctx, `SELECT version()`).Scan(&version); err != nil {
		t.Fatalf("reading database version: %v", err)
	}
	if err := db.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&displayedIsolation); err != nil {
		t.Fatalf("reading displayed transaction isolation: %v", err)
	}
	t.Logf("database version=%q; SQL displayed transaction_isolation=%q; effective Yugabyte isolation=UNVERIFIED (server flags were not inspected)", version, displayedIsolation)

	peerCfg := cfg
	peerCfg.ITestID = ""
	peerCfg.Schema = "itest_" + string(id)
	peerCfg.ApplicationName = "curio-storage-transfer-itest-peer"
	return db, peerCfg
}

func requireStorageTransferITestEnv(t *testing.T, name string) string {
	t.Helper()
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		t.Fatalf("%s must be set explicitly", name)
	}
	return value
}

func TestStorageTransferDBConcurrentHandlesMoveEachSectorOnce(t *testing.T) {
	db, peerCfg := storageTransferITestDB(t)
	peerDB, err := harmonydb.NewFromConfig(peerCfg)
	if err != nil {
		t.Fatalf("opening independent storage-transfer database handle: %v", err)
	}
	// HarmonyDB does not expose a non-destructive Close method. The owner
	// handle drops the isolated schema; the peer is scoped to this short-lived
	// opt-in test process and is never pointed at a non-loopback target.
	dbs := []*harmonydb.DB{db, peerDB}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const (
		spID  = int64(3100)
		total = 2*sealBatchSize + 1
	)
	insertStorageTransferOpenSectors(t, ctx, db, spID, 1, total, false)

	params := sealBatchParams{
		spID:                 spID,
		proof:                int64(abi.RegisteredSealProof_StackedDrg2KiBV1_1),
		sectorSize:           storageTransferITestSectorSize,
		maxWaitBefore:        time.Now().Add(-24 * time.Hour),
		sealBeforeChainEpoch: 0,
	}

	start := make(chan struct{})
	errs := make(chan error, len(dbs))
	var wg sync.WaitGroup
	for _, transactionDB := range dbs {
		wg.Add(1)
		go func(transactionDB *harmonydb.DB) {
			defer wg.Done()
			<-start
			errs <- drainSealProviders(ctx, transactionDB, []sealBatchParams{params})
		}(transactionDB)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	assertStorageTransferITestCount(t, ctx, db, storageTransferCountOpen, 0, spID)
	assertStorageTransferITestCount(t, ctx, db, storageTransferCountSDRPipeline, total, spID)
	assertStorageTransferITestCount(t, ctx, db, storageTransferCountSDRInitial, total, spID)
	assertStorageTransferITestCount(t, ctx, db, storageTransferCountDuplicateSDRInitial, 0, spID)
}

func TestStorageTransferDBInconsistentPipelineRollsBackAndContinues(t *testing.T) {
	db, _ := storageTransferITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const (
		spID         = int64(3101)
		poisonSector = int64(1)
		validSector  = int64(2)
	)
	insertStorageTransferOpenSectors(t, ctx, db, spID, 1, 2, false)
	if _, err := db.Exec(ctx, `INSERT INTO sectors_sdr_pipeline
		(sp_id, sector_number, reg_seal_proof) VALUES ($1, $2, $3)`,
		spID, poisonSector, abi.RegisteredSealProof_StackedDrg2KiBV1_1); err != nil {
		t.Fatal(err)
	}

	err := drainSealProviders(ctx, db, []sealBatchParams{{
		spID:                 spID,
		proof:                int64(abi.RegisteredSealProof_StackedDrg2KiBV1_1),
		sectorSize:           storageTransferITestSectorSize,
		maxWaitBefore:        time.Now().Add(-24 * time.Hour),
		sealBeforeChainEpoch: 0,
	}})
	if err == nil {
		t.Fatal("expected existing-pipeline inconsistency")
	}

	assertStorageTransferITestCount(t, ctx, db, storageTransferCountOpenSector, 1, spID, poisonSector)
	assertStorageTransferITestCount(t, ctx, db, storageTransferCountSDRPipelineSector, 1, spID, poisonSector)
	assertStorageTransferITestCount(t, ctx, db, storageTransferCountSDRInitialSector, 0, spID, poisonSector)

	assertStorageTransferITestCount(t, ctx, db, storageTransferCountOpenSector, 0, spID, validSector)
	assertStorageTransferITestCount(t, ctx, db, storageTransferCountSDRPipelineSector, 1, spID, validSector)
	assertStorageTransferITestCount(t, ctx, db, storageTransferCountSDRInitialSector, 1, spID, validSector)
}

func TestStorageTransferDBSnapMovesAtomically(t *testing.T) {
	db, _ := storageTransferITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const (
		spID   = int64(3102)
		sector = int64(11)
	)
	if _, err := db.Exec(ctx, `INSERT INTO sectors_meta (sp_id, sector_num) VALUES ($1, $2)`, spID, sector); err != nil {
		t.Fatal(err)
	}
	insertStorageTransferOpenSectors(t, ctx, db, spID, int(sector), 1, true)

	err := drainSealProviders(ctx, db, []sealBatchParams{{
		spID:                 spID,
		proof:                int64(abi.RegisteredUpdateProof_StackedDrg2KiBV1),
		sectorSize:           storageTransferITestSectorSize,
		isSnap:               true,
		maxWaitBefore:        time.Now().Add(-24 * time.Hour),
		sealBeforeChainEpoch: 0,
	}})
	if err != nil {
		t.Fatal(err)
	}

	assertStorageTransferITestCount(t, ctx, db, storageTransferCountOpenSector, 0, spID, sector)
	assertStorageTransferITestCount(t, ctx, db, storageTransferCountSnapPipelineSector, 1, spID, sector)
	assertStorageTransferITestCount(t, ctx, db, storageTransferCountSnapInitialSector, 1, spID, sector)
}

func insertStorageTransferOpenSectors(t *testing.T, ctx context.Context, db *harmonydb.DB, spID int64, first, count int, isSnap bool) {
	t.Helper()
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		for sector := first; sector < first+count; sector++ {
			if _, err := tx.Exec(`INSERT INTO open_sector_pieces (
				sp_id, sector_number, piece_index, piece_cid, piece_size,
				data_url, data_raw_size, data_delete_on_finalize, is_snap
			) VALUES ($1, $2, 0, $3, $4, $5, 2032, FALSE, $6)`,
				spID, sector, fmt.Sprintf("itest-piece-%d", sector), storageTransferITestSectorSize,
				fmt.Sprintf("pieceref:%d", sector), isSnap); err != nil {
				return false, err
			}
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("open-sector fixture transaction did not commit")
	}
}

type storageTransferCountQuery int

const (
	storageTransferCountOpen storageTransferCountQuery = iota
	storageTransferCountOpenSector
	storageTransferCountSDRPipeline
	storageTransferCountSDRPipelineSector
	storageTransferCountSDRInitial
	storageTransferCountSDRInitialSector
	storageTransferCountDuplicateSDRInitial
	storageTransferCountSnapPipelineSector
	storageTransferCountSnapInitialSector
)

func assertStorageTransferITestCount(t *testing.T, ctx context.Context, db *harmonydb.DB, query storageTransferCountQuery, want int, args ...any) {
	t.Helper()
	var got int
	var err error
	switch query {
	case storageTransferCountOpen:
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM open_sector_pieces WHERE sp_id = $1`, args...).Scan(&got)
	case storageTransferCountOpenSector:
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM open_sector_pieces WHERE sp_id = $1 AND sector_number = $2`, args...).Scan(&got)
	case storageTransferCountSDRPipeline:
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline WHERE sp_id = $1`, args...).Scan(&got)
	case storageTransferCountSDRPipelineSector:
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline WHERE sp_id = $1 AND sector_number = $2`, args...).Scan(&got)
	case storageTransferCountSDRInitial:
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_initial_pieces WHERE sp_id = $1`, args...).Scan(&got)
	case storageTransferCountSDRInitialSector:
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_initial_pieces WHERE sp_id = $1 AND sector_number = $2`, args...).Scan(&got)
	case storageTransferCountDuplicateSDRInitial:
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM (
			SELECT sector_number FROM sectors_sdr_initial_pieces
			WHERE sp_id = $1 GROUP BY sector_number HAVING COUNT(*) <> 1
		) duplicated`, args...).Scan(&got)
	case storageTransferCountSnapPipelineSector:
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_snap_pipeline WHERE sp_id = $1 AND sector_number = $2`, args...).Scan(&got)
	case storageTransferCountSnapInitialSector:
		err = db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_snap_initial_pieces WHERE sp_id = $1 AND sector_number = $2`, args...).Scan(&got)
	default:
		t.Fatalf("unknown storage-transfer count query %d", query)
	}
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count query %d = %d, want %d", query, got, want)
	}
}

const storageTransferITestSchema = `
CREATE TABLE sectors_allocated_numbers (
    sp_id BIGINT NOT NULL PRIMARY KEY,
    allocated JSONB NOT NULL
);

CREATE TABLE sectors_sdr_pipeline (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    reg_seal_proof INT NOT NULL,
    PRIMARY KEY (sp_id, sector_number)
);

CREATE TABLE sectors_sdr_initial_pieces (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    piece_index BIGINT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    data_url TEXT NOT NULL,
    data_headers JSONB NOT NULL DEFAULT '{}',
    data_raw_size BIGINT NOT NULL,
    data_delete_on_finalize BOOL NOT NULL,
    f05_publish_cid TEXT,
    f05_deal_id BIGINT,
    f05_deal_proposal JSONB,
    f05_deal_start_epoch BIGINT,
    f05_deal_end_epoch BIGINT,
    direct_start_epoch BIGINT,
    direct_end_epoch BIGINT,
    direct_piece_activation_manifest JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (sp_id, sector_number, piece_index),
    FOREIGN KEY (sp_id, sector_number)
        REFERENCES sectors_sdr_pipeline (sp_id, sector_number) ON DELETE CASCADE
);

CREATE TABLE sectors_meta (
    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,
    PRIMARY KEY (sp_id, sector_num)
);

CREATE TABLE sectors_snap_pipeline (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    upgrade_proof INT NOT NULL,
    PRIMARY KEY (sp_id, sector_number),
    FOREIGN KEY (sp_id, sector_number)
        REFERENCES sectors_meta (sp_id, sector_num)
);

CREATE TABLE sectors_snap_initial_pieces (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    piece_index BIGINT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    data_url TEXT NOT NULL,
    data_headers JSONB NOT NULL DEFAULT '{}',
    data_raw_size BIGINT NOT NULL,
    data_delete_on_finalize BOOL NOT NULL,
    direct_start_epoch BIGINT,
    direct_end_epoch BIGINT,
    direct_piece_activation_manifest JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (sp_id, sector_number, piece_index),
    FOREIGN KEY (sp_id, sector_number)
        REFERENCES sectors_snap_pipeline (sp_id, sector_number) ON DELETE CASCADE
);

CREATE TABLE open_sector_pieces (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    piece_index BIGINT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    data_url TEXT NOT NULL,
    data_headers JSONB NOT NULL DEFAULT '{}',
    data_raw_size BIGINT NOT NULL,
    data_delete_on_finalize BOOL NOT NULL,
    f05_publish_cid TEXT,
    f05_deal_id BIGINT,
    f05_deal_proposal JSONB,
    f05_deal_start_epoch BIGINT,
    f05_deal_end_epoch BIGINT,
    direct_start_epoch BIGINT,
    direct_end_epoch BIGINT,
    direct_piece_activation_manifest JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_snap BOOL NOT NULL DEFAULT FALSE,
    PRIMARY KEY (sp_id, sector_number, piece_index)
);

CREATE OR REPLACE FUNCTION transfer_and_delete_sorted_open_piece(v_sp_id BIGINT, v_sector_number BIGINT)
RETURNS VOID AS $$
DECLARE
    sorted_piece RECORD;
    new_index INT := 0;
BEGIN
    FOR sorted_piece IN
        SELECT piece_cid, piece_size, data_url, data_headers, data_raw_size,
               data_delete_on_finalize, f05_publish_cid, f05_deal_id,
               f05_deal_proposal, f05_deal_start_epoch, f05_deal_end_epoch,
               direct_start_epoch, direct_end_epoch,
               direct_piece_activation_manifest, created_at
        FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number
        ORDER BY piece_size DESC
    LOOP
        INSERT INTO sectors_sdr_initial_pieces (
            sp_id, sector_number, piece_index, piece_cid, piece_size,
            data_url, data_headers, data_raw_size, data_delete_on_finalize,
            f05_publish_cid, f05_deal_id, f05_deal_proposal,
            f05_deal_start_epoch, f05_deal_end_epoch, direct_start_epoch,
            direct_end_epoch, direct_piece_activation_manifest, created_at
        ) VALUES (
            v_sp_id, v_sector_number, new_index, sorted_piece.piece_cid,
            sorted_piece.piece_size, sorted_piece.data_url,
            sorted_piece.data_headers, sorted_piece.data_raw_size,
            sorted_piece.data_delete_on_finalize, sorted_piece.f05_publish_cid,
            sorted_piece.f05_deal_id, sorted_piece.f05_deal_proposal,
            sorted_piece.f05_deal_start_epoch, sorted_piece.f05_deal_end_epoch,
            sorted_piece.direct_start_epoch, sorted_piece.direct_end_epoch,
            sorted_piece.direct_piece_activation_manifest, sorted_piece.created_at
        );
        new_index := new_index + 1;
    END LOOP;

    IF FOUND THEN
        DELETE FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number;
    ELSE
        RAISE EXCEPTION 'No open pieces for provider % and sector %', v_sp_id, v_sector_number;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION transfer_and_delete_sorted_open_piece_snap(v_sp_id BIGINT, v_sector_number BIGINT)
RETURNS VOID AS $$
DECLARE
    sorted_piece RECORD;
    new_index INT := 0;
BEGIN
    IF EXISTS (
        SELECT 1 FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number
          AND f05_deal_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Snap transfer cannot contain an F05 deal';
    END IF;

    FOR sorted_piece IN
        SELECT piece_cid, piece_size, data_url, data_headers, data_raw_size,
               data_delete_on_finalize, direct_start_epoch, direct_end_epoch,
               direct_piece_activation_manifest, created_at
        FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number
        ORDER BY piece_size DESC
    LOOP
        INSERT INTO sectors_snap_initial_pieces (
            sp_id, sector_number, piece_index, piece_cid, piece_size,
            data_url, data_headers, data_raw_size, data_delete_on_finalize,
            direct_start_epoch, direct_end_epoch,
            direct_piece_activation_manifest, created_at
        ) VALUES (
            v_sp_id, v_sector_number, new_index, sorted_piece.piece_cid,
            sorted_piece.piece_size, sorted_piece.data_url,
            sorted_piece.data_headers, sorted_piece.data_raw_size,
            sorted_piece.data_delete_on_finalize, sorted_piece.direct_start_epoch,
            sorted_piece.direct_end_epoch,
            sorted_piece.direct_piece_activation_manifest, sorted_piece.created_at
        );
        new_index := new_index + 1;
    END LOOP;

    IF FOUND THEN
        DELETE FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number;
    ELSE
        RAISE EXCEPTION 'No open pieces for provider % and sector %', v_sp_id, v_sector_number;
    END IF;
END;
$$ LANGUAGE plpgsql;

`
