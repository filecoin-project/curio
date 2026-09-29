package webrpcporep

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/web/api/webrpc"
)

func TestPoRepPageSnapshot(t *testing.T) {
	for _, empty := range []bool{false, true} {
		calls := 0
		sp := int64(1000)
		at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		result, err := loadPoRepPage(context.Background(), PoRepPageRequest{Offset: 200, HidePendingSDR: true},
			func(ctx context.Context, out interface{}, query string, args ...interface{}) error {
				calls++
				if query != porepPageQuery || len(args) != 3 || args[0] != true || args[1] != 100 || args[2] != 200 {
					t.Fatalf("production query/arguments: %v", args)
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > POREP_PAGE_TIMEOUT {
					t.Fatal("missing bounded query context")
				}
				row := porepPageRow{Total: 31477, Matching: 294, WaitingForPrecommit: 7, WaitingForCommit: 9, ObservedAt: at}
				if !empty {
					row.PageSpID = &sp
					row.SpID, row.SectorNumber = sp, 500
					row.TaskSDR = NullInt64{Int64: 42, Valid: true}
					row.SDROwned = true
				}
				*out.(*[]porepPageRow) = []porepPageRow{row}
				return nil
			})
		if err != nil || calls != 1 || result.Total != 31477 || result.Matching != 294 || result.Offset != 200 || result.Limit != 100 || !result.ObservedAt.Equal(at) {
			t.Fatalf("snapshot: %#v, calls=%d error=%v", result, calls, err)
		}
		if empty {
			if len(result.Sectors) != 0 {
				t.Fatal("fabricated row for empty page")
			}
			continue
		}
		wire, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Sectors []struct {
				TaskSDR     int64
				SDROwned    bool
				ChainActive *bool
				AfterSeed   *bool
			}
		}
		if err := json.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		row := decoded.Sectors[0]
		if row.TaskSDR != 42 || !row.SDROwned || row.ChainActive != nil || row.AfterSeed != nil {
			t.Fatalf("unknown chain state or task identity misrepresented: %s", wire)
		}
	}
}

func TestPoRepPageErrorsAndCancellation(t *testing.T) {
	// The registered production entry rejects invalid input without touching DB
	// or Chain. Positive-path tests use the exact loader with a Select boundary.
	a := &PoRep{Handler: &webrpc.Handler{Deps: &deps.Deps{}}}
	if _, err := a.PipelinePorepPage(context.Background(), PoRepPageRequest{Offset: -1}); err == nil {
		t.Fatal("negative offset accepted")
	}
	for _, size := range []int{0, 101} {
		_, err := loadPoRepPage(context.Background(), PoRepPageRequest{}, func(_ context.Context, out interface{}, _ string, _ ...interface{}) error {
			*out.(*[]porepPageRow) = make([]porepPageRow, size)
			return nil
		})
		if err == nil {
			t.Fatal("invalid snapshot row count accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := loadPoRepPage(ctx, PoRepPageRequest{}, func(ctx context.Context, _ interface{}, _ string, _ ...interface{}) error { return ctx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	want := errors.New("query failed")
	result, err := loadPoRepPage(context.Background(), PoRepPageRequest{}, func(context.Context, interface{}, string, ...interface{}) error { return want })
	if result != nil || !errors.Is(err, want) {
		t.Fatal("query failure became an empty success")
	}
}

func TestPoRepPageSQLShape(t *testing.T) {
	// SQL shape only: this does not execute PostgreSQL/Yugabyte or establish a
	// planner/runtime cost. Production counts/page selection use this literal.
	for _, fragment := range []string{
		"COUNT(*) AS total", "COUNT(*) FILTER (WHERE NOT $1 OR failed OR after_sdr OR sdr_owned) AS matching",
		"WHERE NOT $1 OR failed OR after_sdr OR sdr_owned", "CASE WHEN failed OR after_sdr OR sdr_owned THEN 0 ELSE 1 END AS priority",
		"ORDER BY priority, sp_id, sector_number\n\tLIMIT $2 OFFSET $3",
		"sp.sp_id = page.sp_id AND sp.sector_number = page.sector_number", "LEFT JOIN page ON TRUE",
		"statement_timestamp() AS observed_at",
	} {
		if !strings.Contains(porepPageQuery, fragment) {
			t.Errorf("missing guard: %s", fragment)
		}
	}
	for _, unwanted := range []string{"porep_proof", "SELECT sp.*", "harmony_task_history"} {
		if strings.Contains(porepPageQuery, unwanted) {
			t.Errorf("unbounded payload/work: %s", unwanted)
		}
	}
}

func TestPoRepPageRegisteredRPC(t *testing.T) {
	server := jsonrpc.NewServer()
	server.Register("CurioWeb", &PoRep{Handler: &webrpc.Handler{Deps: &deps.Deps{}}})
	request := httptest.NewRequest("POST", "/api/webrpc/v0", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"CurioWeb.PipelinePorepPage","params":[{"Offset":-1}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	var body struct{ Error struct{ Message string } }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Error.Message, "negative PoRep page offset") {
		t.Fatalf("production RPC registration/decoding: %s", response.Body.String())
	}
}

func TestPoRepPageSerializationBounds(t *testing.T) {
	all := make([]sectorListEntry, 31477)
	page := &PoRepPage{Total: 31477, Matching: 31477, Sectors: make([]PoRepPageEntry, POREP_PAGE_SIZE)}
	for i := range all {
		all[i].SpID, all[i].SectorNumber = 1000, int64(i+1)
		all[i].TaskSDR = NullInt64{Valid: true, Int64: int64(i + 100)}
		if i < POREP_PAGE_SIZE {
			page.Sectors[i].PipelineTask = all[i].PipelineTask
		}
	}
	oldStart := time.Now()
	oldWire, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	oldTime := time.Since(oldStart)
	pageStart := time.Now()
	newWire, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	newTime := time.Since(pageStart)
	if len(newWire) >= len(oldWire)/100 {
		t.Fatal("page serialization no longer bounded")
	}
	t.Logf("metadata-only synthetic serialization: legacy rows=%d bytes=%d wall=%s; page rows=%d bytes=%d wall=%s; not DB/network/production timing", len(all), len(oldWire), oldTime, len(page.Sectors), len(newWire), newTime)
}
