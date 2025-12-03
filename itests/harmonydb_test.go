package itests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestCrud(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sharedITestID := harmonydb.ITestNewID()
	cdb, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID)
	require.NoError(t, err)

	//cdb := miner.BaseAPI.(*impl.StorageMinerAPI).HarmonyDB
	_, err = cdb.Exec(ctx, `
			INSERT INTO 
				itest_scratch (some_int, content) 
			VALUES 
				(11, 'cows'), 
				(5, 'cats')
		`)
	require.NoError(t, err)
	var ints []struct {
		Count       int    `db:"some_int"`
		Animal      string `db:"content"`
		Unpopulated int
	}
	err = cdb.Select(ctx, &ints, "SELECT content, some_int FROM itest_scratch")
	require.NoError(t, err)

	require.Len(t, ints, 2, "unexpected count of returns. Want 2, Got ", len(ints))
	require.True(t, ints[0].Count == 11 || ints[1].Count == 5, "expected [11,5] got ", ints)
	require.True(t, ints[0].Animal == "cows" || ints[1].Animal == "cats", "expected, [cows, cats] ", ints)
	fmt.Println("test completed")

}

func TestTransaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testID := harmonydb.ITestNewID()
	cdb, err := harmonydb.NewFromConfigWithITestID(t, testID)
	require.NoError(t, err)
	_, err = cdb.Exec(ctx, "INSERT INTO itest_scratch (some_int) VALUES (4), (5), (6)")
	require.NoError(t, err)
	_, err = cdb.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		if _, err = tx.Exec("INSERT INTO itest_scratch (some_int) VALUES (7), (8), (9)"); err != nil {
			return false, err
		}

		// sum1 is read from OUTSIDE the transaction so it's the old value
		var sum1 int
		if err := cdb.QueryRow(ctx, "SELECT SUM(some_int) FROM itest_scratch").Scan(&sum1); err != nil {
			t.Fatal("E2", err)
		}
		if sum1 != 4+5+6 {
			return false, xerrors.Errorf("Expected 15, got %d", sum1)
		}

		// sum2 is from INSIDE the transaction, so the updated value.
		var sum2 int
		if err := tx.QueryRow("SELECT SUM(some_int) FROM itest_scratch").Scan(&sum2); err != nil {
			t.Fatal("E3", err)
		}
		if sum2 != 4+5+6+7+8+9 {
			return false, xerrors.Errorf("Expected 39, got %d", sum2)
		}
		return false, nil // rollback
	})
	require.NoError(t, err)

	var sum2 int
	// Query() example (yes, QueryRow would be preferred here)
	q, err := cdb.Query(ctx, "SELECT SUM(some_int) FROM itest_scratch")
	require.NoError(t, err)
	defer q.Close()
	var rowCt int
	for q.Next() {
		err := q.Scan(&sum2)
		require.NoError(t, err)
		rowCt++
	}
	require.Equal(t, 4+5+6, sum2, "Expected 15, got ", sum2)
	require.Equal(t, 1, rowCt, "unexpected count of rows")
}

func TestPartialWalk(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testID := harmonydb.ITestNewID()
	cdb, err := harmonydb.NewFromConfigWithITestID(t, testID)
	require.NoError(t, err)
	_, err = cdb.Exec(ctx, `
			INSERT INTO 
				itest_scratch (content, some_int) 
			VALUES 
				('andy was here', 5), 
				('lotus is awesome', 6), 
				('hello world', 7),
				('3rd integration test', 8),
				('fiddlesticks', 9)
			`)
	require.NoError(t, err)

	// TASK: FIND THE ID of the string with a specific SHA256
	needle := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	q, err := cdb.Query(ctx, `SELECT id, content FROM itest_scratch`)
	require.NoError(t, err)
	defer q.Close()

	var tmp struct {
		Src string `db:"content"`
		ID  int
	}

	var done bool
	for q.Next() {

		err = q.StructScan(&tmp)
		require.NoError(t, err)

		bSha := sha256.Sum256([]byte(tmp.Src))
		if hex.EncodeToString(bSha[:]) == needle {
			done = true
			break
		}
	}
	require.True(t, done)
}

func TestRevertTo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testID := harmonydb.ITestNewID()
	cdb, err := harmonydb.NewFromConfigWithITestID(t, testID)
	require.NoError(t, err)

	// The setup: lets make revert files going forward in time, but ignore the past.
	rowCt, err := cdb.Exec(ctx, "UPDATE base SET applied = applied - INTERVAL '10 DAY' WHERE TO_DATE(entry, 'YYYYMMDD') < DATE '2025-08-15'")
	require.NoError(t, err)

	if rowCt == 0 {
		t.Fatal("no rows in save set")
	}
	n, _ := strconv.Atoi(time.Now().AddDate(0, 0, -1).Format("20060102"))
	err = cdb.RevertTo(ctx, n)
	require.NoError(t, err, "error reverting. All sql entries need a revert file.")

	var shouldBeReverted []string
	err = cdb.Select(ctx, &shouldBeReverted, "SELECT entry FROM base WHERE entry > '20250815'")
	require.NoError(t, err)
	require.Len(t, shouldBeReverted, 0, "expected no entries to be reverted. Got ", len(shouldBeReverted))

	var rowCt2 int
	err = cdb.QueryRow(ctx, "SELECT COUNT(*) FROM base WHERE entry < '20250815'").Scan(&rowCt2)
	require.NoError(t, err)
	require.Equal(t, rowCt2, rowCt, "expected no older entries to be reverted. Got ", rowCt2-rowCt)

	_, err = cdb.Exec(ctx, "UPDATE base SET applied = applied + INTERVAL '2 DAY' WHERE entry < '20250815'")
	require.NoError(t, err)
}
