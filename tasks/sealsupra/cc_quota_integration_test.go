//go:build integration

package sealsupra

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/chain/types"
)

type ccQuotaAPI struct {
	SupraSealNodeAPI
	beforeAllocate func(address.Address)
}

func (a ccQuotaAPI) StateMinerAllocated(_ context.Context, miner address.Address, _ types.TipSetKey) (*bitfield.BitField, error) {
	if a.beforeAllocate != nil {
		a.beforeAllocate(miner)
	}
	b := bitfield.New()
	return &b, nil
}

func TestCCQuota(t *testing.T) {
	port := os.Getenv("CURIO_CC_QUOTA_TEST_PORT")
	if port == "" {
		t.Skip("set CURIO_CC_QUOTA_TEST_PORT for a disposable loopback PostgreSQL instance")
	}
	n, err := strconv.ParseUint(port, 10, 16)
	require.NoError(t, err)
	require.NotZero(t, n)
	db, err := harmonydb.NewFromConfigWithITestID(t, func(o *harmonydb.ItestOptions) {
		o.Hosts, o.Port = []string{"127.0.0.1"}, port
		o.Database, o.Username, o.Password = "postgres", "postgres", ""
	})
	require.NoError(t, err)
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		quotas []int64
		want   []int64
	}{
		{"last provider capped and redistributed", []int64{5, 4, 1}, []int64{5, 4, 1}},
		{"preserve valid weighted remainder", []int64{20, 20, 20}, []int64{5, 3, 2}},
		{"exhausted first provider", []int64{0, 9, 1}, []int64{0, 9, 1}},
		{"previously overdrawn provider", []int64{-1, 9, 1}, []int64{0, 9, 1}},
		{"exhausted last provider", []int64{5, 5, 0}, []int64{5, 5, 0}},
		{"insufficient total", []int64{5, 3, 1}, nil},
		{"all exhausted", []int64{0, 0, 0}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seedCCQuota(t, db, tc.quotas)
			s := SupraSeal{api: ccQuotaAPI{}, spt: abi.RegisteredSealProof_StackedDrg32GiBV1_1}
			var claims []sectorClaim
			committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
				var claimErr error
				claims, claimErr = s.claimsFromCCScheduler(tx, 10)
				return claimErr == nil && len(claims) == 10, claimErr
			})
			require.NoError(t, err)
			if tc.want == nil {
				require.False(t, committed)
				require.Empty(t, claims)
				assertCCQuotaRollback(t, db, tc.quotas)
				return
			}
			require.True(t, committed)
			got := make([]int64, len(tc.quotas))
			for _, claim := range claims {
				got[claim.SpID-1001]++
			}
			require.Equal(t, tc.want, got)
			for i, count := range got {
				var remaining, pipelineCount int64
				var allocated []byte
				require.NoError(t, db.QueryRow(ctx, `SELECT to_seal FROM sectors_cc_scheduler WHERE sp_id = $1`, 1001+i).Scan(&remaining))
				require.Equal(t, tc.quotas[i]-count, remaining)
				require.GreaterOrEqual(t, remaining, min(tc.quotas[i], int64(0)))
				require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM sectors_sdr_pipeline WHERE sp_id = $1`, 1001+i).Scan(&pipelineCount))
				require.Equal(t, count, pipelineCount)
				if count > 0 {
					require.NoError(t, db.QueryRow(ctx, `SELECT allocated FROM sectors_allocated_numbers WHERE sp_id = $1`, 1001+i).Scan(&allocated))
					var bits bitfield.BitField
					require.NoError(t, bits.UnmarshalJSON(allocated))
					n, err := bits.Count()
					require.NoError(t, err)
					require.Equal(t, uint64(count), n)
				}
			}
		})
	}

	for _, disabled := range []bool{false, true} {
		name := "insufficient current quota"
		if disabled {
			name = "disabled current schedule"
		}
		t.Run(name, func(t *testing.T) {
			quotas := []int64{20, 20, 20}
			seedCCQuota(t, db, quotas)
			injected := false
			committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
				s := SupraSeal{spt: abi.RegisteredSealProof_StackedDrg32GiBV1_1}
				s.api = ccQuotaAPI{beforeAllocate: func(miner address.Address) {
					id, err := address.IDFromAddress(miner)
					require.NoError(t, err)
					if id != 1002 {
						return
					}
					var written int
					require.NoError(t, tx.QueryRow(`SELECT count(*) FROM sectors_sdr_pipeline WHERE sp_id = 1001`).Scan(&written))
					require.Equal(t, 5, written)
					// Change the schedule after selection, in the same transaction.
					// This exercises debit rejection/rollback, not a concurrent writer.
					if disabled {
						_, err = tx.Exec(`UPDATE sectors_cc_scheduler SET enabled = FALSE WHERE sp_id = 1002`)
					} else {
						_, err = tx.Exec(`UPDATE sectors_cc_scheduler SET to_seal = 0 WHERE sp_id = 1002`)
					}
					require.NoError(t, err)
					injected = true
				}}
				_, err := s.claimsFromCCScheduler(tx, 10)
				return err == nil, err
			})
			require.True(t, injected)
			require.ErrorContains(t, err, "CC quota changed")
			require.False(t, committed)
			assertCCQuotaRollback(t, db, quotas)
		})
	}
}

func seedCCQuota(t *testing.T, db *harmonydb.DB, quotas []int64) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Exec(ctx, `DELETE FROM sectors_sdr_pipeline`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `DELETE FROM sectors_allocated_numbers`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `DELETE FROM sectors_cc_scheduler`)
	require.NoError(t, err)
	for i, quota := range quotas {
		_, err = db.Exec(ctx, `INSERT INTO sectors_cc_scheduler (sp_id, to_seal, weight, duration_days) VALUES ($1, $2, $3, 200)`, 1001+i, quota, 3-i)
		require.NoError(t, err)
	}
}

func assertCCQuotaRollback(t *testing.T, db *harmonydb.DB, quotas []int64) {
	t.Helper()
	ctx := context.Background()
	var pipelines, allocations int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM sectors_sdr_pipeline`).Scan(&pipelines))
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM sectors_allocated_numbers`).Scan(&allocations))
	require.Zero(t, pipelines)
	require.Zero(t, allocations)
	for i, quota := range quotas {
		var remaining int64
		var enabled bool
		require.NoError(t, db.QueryRow(ctx, `SELECT to_seal, enabled FROM sectors_cc_scheduler WHERE sp_id = $1`, 1001+i).Scan(&remaining, &enabled))
		require.Equal(t, quota, remaining)
		require.True(t, enabled)
	}
}
