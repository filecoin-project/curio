package itests

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// TestTimestampTimezoneHandling verifies our understanding of how PostgreSQL
// handles timestamp storage under different session timezones. Specifically:
//
//   - NOW() returns TIMESTAMPTZ and stores correctly regardless of session TZ.
//   - CURRENT_TIMESTAMP AT TIME ZONE 'UTC' returns TIMESTAMP WITHOUT TIME ZONE;
//     when implicitly cast to TIMESTAMPTZ on INSERT, PostgreSQL re-interprets it
//     using the session TZ, shifting the stored instant by -offset.
//   - Go time.Time parameters (sent via pgx) always round-trip correctly.
//
// This test exists because of a production bug where triggers used the buggy
// expression, causing batch timeouts to fire late (UTC-) or early (UTC+).
// See: 20260222-fix-trigger-timestamps.sql
func TestTimestampTimezoneHandling(t *testing.T) {
	ctx := context.Background()

	testID := harmonydb.ITestNewID()
	cdb, err := harmonydb.NewFromConfigWithITestID(t, testID, false)
	require.NoError(t, err)

	_, err = cdb.Exec(ctx, `CREATE TABLE IF NOT EXISTS tz_test (
		id SERIAL PRIMARY KEY,
		tz_name TEXT NOT NULL,
		offset_sec INT NOT NULL,
		via_now TIMESTAMPTZ NOT NULL,
		via_buggy_expr TIMESTAMPTZ NOT NULL,
		via_go_param TIMESTAMPTZ NOT NULL
	)`)
	require.NoError(t, err)

	goRefTime := time.Now().UTC().Truncate(time.Microsecond)

	timezones := []string{
		"UTC",
		"America/New_York",  // UTC-5 (EST) / UTC-4 (EDT)
		"Asia/Kolkata",      // UTC+5:30 (fixed, no DST)
		"Pacific/Auckland",  // UTC+12 (NZST) / UTC+13 (NZDT)
	}

	for _, tz := range timezones {
		tz := tz
		_, err := cdb.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// set_config('timezone', value, is_local) lets us parameterize
			// the timezone name while keeping the SQL string literal.
			var applied string
			if err := tx.QueryRow(
				"SELECT set_config('timezone', $1, true)", tz,
			).Scan(&applied); err != nil {
				return false, err
			}

			var offsetSec int
			if err := tx.QueryRow(
				"SELECT EXTRACT(TIMEZONE FROM NOW())::int",
			).Scan(&offsetSec); err != nil {
				return false, err
			}

			_, err := tx.Exec(
				"INSERT INTO tz_test (tz_name, offset_sec, via_now, via_buggy_expr, via_go_param) VALUES ($1, $2, NOW(), CURRENT_TIMESTAMP AT TIME ZONE 'UTC', $3)",
				tz, offsetSec, goRefTime,
			)
			return err == nil, err
		})
		require.NoError(t, err, "timezone %s", tz)
	}

	type tzRow struct {
		TzName       string    `db:"tz_name"`
		OffsetSec    int       `db:"offset_sec"`
		ViaNow       time.Time `db:"via_now"`
		ViaBuggyExpr time.Time `db:"via_buggy_expr"`
		ViaGoParam   time.Time `db:"via_go_param"`
	}
	var rows []tzRow
	err = cdb.Select(ctx, &rows,
		"SELECT tz_name, offset_sec, via_now, via_buggy_expr, via_go_param FROM tz_test ORDER BY tz_name")
	require.NoError(t, err)
	require.Len(t, rows, len(timezones))

	byTz := make(map[string]tzRow, len(rows))
	for _, r := range rows {
		byTz[r.TzName] = r
		t.Logf("%-20s offset=%+7ds  now=%s  buggy=%s  go=%s",
			r.TzName, r.OffsetSec,
			r.ViaNow.UTC().Format(time.RFC3339Nano),
			r.ViaBuggyExpr.UTC().Format(time.RFC3339Nano),
			r.ViaGoParam.UTC().Format(time.RFC3339Nano))
	}

	t.Run("NOW() stores the same absolute instant regardless of session timezone", func(t *testing.T) {
		ref := byTz["UTC"].ViaNow
		for _, tz := range timezones {
			diff := byTz[tz].ViaNow.Sub(ref)
			assert.Less(t, math.Abs(diff.Seconds()), 5.0,
				"NOW() from session %q should be within 5s of UTC session, got diff=%v", tz, diff)
		}
	})

	t.Run("Go time.Time parameter round-trips exactly through any session timezone", func(t *testing.T) {
		for _, tz := range timezones {
			got := byTz[tz].ViaGoParam
			assert.True(t, got.Equal(goRefTime),
				"session %q: want %v, got %v (diff=%v)", tz, goRefTime, got, got.Sub(goRefTime))
		}
	})

	t.Run("CURRENT_TIMESTAMP AT TIME ZONE UTC is shifted by -offset in non-UTC sessions", func(t *testing.T) {
		// The buggy expression strips the timezone marker, producing a bare
		// wall-clock value. On INSERT into TIMESTAMPTZ, PG re-interprets
		// it as session-local time, shifting the stored instant by -offset.
		for _, tz := range timezones {
			r := byTz[tz]
			actualShift := r.ViaBuggyExpr.Sub(r.ViaNow)
			expectedShift := time.Duration(-r.OffsetSec) * time.Second

			diffSec := math.Abs((actualShift - expectedShift).Seconds())
			assert.Less(t, diffSec, 2.0,
				"session %q (offset=%+ds): buggy shift=%v, expected shift=%v",
				tz, r.OffsetSec, actualShift, expectedShift)
		}
	})

	t.Run("UTC session: buggy expression happens to be correct", func(t *testing.T) {
		r := byTz["UTC"]
		diff := r.ViaBuggyExpr.Sub(r.ViaNow)
		assert.Less(t, math.Abs(diff.Seconds()), 2.0,
			"UTC session: CURRENT_TIMESTAMP AT TIME ZONE 'UTC' should match NOW(), diff=%v", diff)
	})

	t.Run("non-UTC sessions: buggy expression produces multi-hour shifts", func(t *testing.T) {
		for _, tz := range timezones[1:] { // skip UTC
			r := byTz[tz]
			shiftHours := math.Abs(r.ViaBuggyExpr.Sub(r.ViaNow).Hours())
			assert.Greater(t, shiftHours, 3.0,
				"session %q (offset=%+ds): expected significant shift, got only %.1fh",
				tz, r.OffsetSec, shiftHours)
			t.Logf("%-20s shift=%.1fh (offset=%+ds) — this is the bug NOW() fixes",
				tz, r.ViaBuggyExpr.Sub(r.ViaNow).Hours(), r.OffsetSec)
		}
	})
}
