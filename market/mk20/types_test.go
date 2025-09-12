package mk20

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/builtin/v16/verifreg"
)

func mustCID(t *testing.T, s string) cid.Cid {
	t.Helper()
	c, err := cid.Parse(s)
	if err != nil {
		t.Fatalf("parse cid: %v", err)
	}
	return c
}

func mustULID(t *testing.T, s string) ulid.ULID {
	t.Helper()
	id, err := ulid.Parse(s)
	if err != nil {
		t.Fatalf("parse ulid: %v", err)
	}
	return id
}

func TestDeal_MarshalUnmarshal_Minimal(t *testing.T) {
	orig := Deal{
		Identifier: mustULID(t, "01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Client:     "f1abcclient",
		// Data omitted (omitempty)
		// Products is empty struct; inner fields are omitempty
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Expect "data" to be absent and "products" to be an empty object
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	if _, ok := m["data"]; ok {
		t.Fatalf("expected 'data' to be omitted, found present")
	}
	if p, ok := m["products"]; !ok {
		t.Fatalf("expected 'products' present")
	} else if obj, ok := p.(map[string]any); !ok || len(obj) != 0 {
		t.Fatalf("expected 'products' to be empty object, got: %#v", p)
	}

	var round Deal
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("round unmarshal: %v", err)
	}

	if !reflect.DeepEqual(orig, round) {
		t.Fatalf("round trip mismatch:\norig: %#v\nround:%#v", orig, round)
	}
}

func TestHttpHeaderRoundTrip(t *testing.T) {
	orig := http.Header{
		"X-Trace-Id":    []string{"abc123"},
		"Cache-Control": []string{"no-cache", "private"},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("marshaled JSON: %s", string(b))

	var round http.Header
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	t.Logf("unmarshaled Struct: %+v", round)
	v := round.Values("Cache-Control")
	require.Equal(t, 2, len(v))
	require.Equal(t, "no-cache", v[0])
	require.Equal(t, "private", v[1])
	v = round.Values("X-Trace-Id")
	require.Equal(t, 1, len(v))
	require.Equal(t, "abc123", v[0])
}

func TestDeal_HTTPSourceWithHeaders(t *testing.T) {
	addr, err := address.NewFromString("t01000")
	require.NoError(t, err)
	x := uint64(1234)
	v := verifreg.AllocationId(x)

	orig := Deal{
		Identifier: mustULID(t, "01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Client:     "f1client",
		Data: &DataSource{
			PieceCID: mustCID(t, "bafkzcibfxx3meais3xzh6qn56y6hiasmrufhegoweu3o5ccofs74nfdfr4yn76pqz4pq"),
			Format:   PieceDataFormat{Car: &FormatCar{}},
			SourceHTTP: &DataSourceHTTP{
				URLs: []HttpUrl{
					{
						URL:      "https://example.com/piece/xyz",
						Headers:  http.Header{"X-Trace-Id": []string{"abc123"}, "Cache-Control": []string{"no-cache", "private"}},
						Priority: 10,
						Fallback: false,
					},
					{
						URL:      "http://127.0.0.1:8080/piece/xyz",
						Headers:  http.Header{}, // empty headers should round-trip
						Priority: 20,
						Fallback: true,
					},
				},
			},
		},
		Products: Products{
			PDPV1: &PDPV1{
				CreateDataSet: true,
				RecordKeeper:  "0x158c8f05A616403589b99BE5d82d756860363A92",
				ExtraData:     []byte("extra data"),
				PieceIDs:      []uint64{0, 1},
				DataSetID:     &x,
			},
			DDOV1: &DDOV1{
				Provider:                   addr,
				PieceManager:               addr,
				AllocationId:               &v,
				ContractVerifyMethodParams: []byte("contract verify method params"),
			},
		},
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	t.Logf("marshaled JSON: %s", string(b))

	var round Deal
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(orig, round) {
		t.Fatalf("round trip mismatch:\norig: %#v\nround:%#v", orig, round)
	}

	// Spot-check headers survived correctly
	gotHdr := round.Data.SourceHTTP.URLs[0].Headers
	if v := gotHdr.Get("X-Trace-ID"); v != "abc123" {
		t.Fatalf("expected X-Trace-ID=abc123, got %q", v)
	}
}

func TestDeal_Aggregate_NoSub_vs_EmptySub(t *testing.T) {
	// Case A: Aggregate.Sub is nil (no omitempty on Sub), expected to marshal as "sub": null
	withNil := Deal{
		Identifier: mustULID(t, "01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Client:     "f1client",
		Data: &DataSource{
			PieceCID: mustCID(t, "bafkzcibfxx3meais3xzh6qn56y6hiasmrufhegoweu3o5ccofs74nfdfr4yn76pqz4pq"),
			Format: PieceDataFormat{
				Aggregate: &FormatAggregate{
					Type: AggregateTypeV1,
					Sub:  nil, // important
				},
			},
		},
	}

	bNil, err := json.Marshal(withNil)
	if err != nil {
		t.Fatalf("marshal nil-sub: %v", err)
	}
	var objNil map[string]any
	_ = json.Unmarshal(bNil, &objNil) // ignore error; presence check is all we need
	// Navigate: data.format.aggregate.sub should be null
	dataMap := objNil["data"].(map[string]any)
	format := dataMap["format"].(map[string]any)
	agg := format["aggregate"].(map[string]any)
	if _, ok := agg["sub"]; !ok {
		t.Fatalf("expected aggregate.sub to be present (as null) when Sub == nil")
	}
	if agg["sub"] != nil {
		t.Fatalf("expected aggregate.sub == null; got: %#v", agg["sub"])
	}

	// Case B: Aggregate.Sub is empty slice, expected to marshal as "sub": []
	withEmpty := withNil
	withEmpty.Data.Format.Aggregate.Sub = []DataSource{}

	bEmpty, err := json.Marshal(withEmpty)
	if err != nil {
		t.Fatalf("marshal empty-sub: %v", err)
	}
	var objEmpty map[string]any
	_ = json.Unmarshal(bEmpty, &objEmpty)
	dataMap = objEmpty["data"].(map[string]any)
	format = dataMap["format"].(map[string]any)
	agg = format["aggregate"].(map[string]any)
	arr, ok := agg["sub"].([]any)
	if !ok {
		t.Fatalf("expected aggregate.sub to be [] when Sub == empty slice; got %#v", agg["sub"])
	}
	if len(arr) != 0 {
		t.Fatalf("expected empty array for sub; got len=%d", len(arr))
	}
}

func TestDeal_Aggregate_WithSubpieces_RoundTrip(t *testing.T) {
	// Two subpieces: one Raw, one Car
	sub1 := DataSource{
		PieceCID:      mustCID(t, "bafkzcibfxx3meais3xzh6qn56y6hiasmrufhegoweu3o5ccofs74nfdfr4yn76pqz4pq"),
		Format:        PieceDataFormat{Raw: &FormatBytes{}},
		SourceOffline: &DataSourceOffline{}, // ensure additional fields survive
	}
	sub2 := DataSource{
		PieceCID: mustCID(t, "bafkzcibfxx3meais3xzh6qn56y6hiasmrufhegoweu3o5ccofs74nfdfr4yn76pqz4pd"),
		Format:   PieceDataFormat{Car: &FormatCar{}},
	}

	orig := Deal{
		Identifier: mustULID(t, "01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Client:     "f1client",
		Data: &DataSource{
			PieceCID: mustCID(t, "bafkzcibfxx3meais3xzh6qn56y6hiasmrufhegoweu3o5ccofs74nfdfr4yn76pqz4pe"),
			Format: PieceDataFormat{
				Aggregate: &FormatAggregate{
					Type: AggregateTypeV1,
					Sub:  []DataSource{sub1, sub2},
				},
			},
			SourceAggregate: &DataSourceAggregate{Pieces: []DataSource{sub1, sub2}},
		},
		Products: Products{
			// exercise omitempty pointers: all nil
		},
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round Deal
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Order must be preserved
	if len(round.Data.Format.Aggregate.Sub) != 2 {
		t.Fatalf("expected 2 subpieces, got %d", len(round.Data.Format.Aggregate.Sub))
	}
	if round.Data.Format.Aggregate.Sub[0].PieceCID.String() != sub1.PieceCID.String() {
		t.Fatalf("subpiece[0] order changed")
	}

	if !reflect.DeepEqual(orig, round) {
		t.Fatalf("round trip mismatch:\norig: %#v\nround:%#v", orig, round)
	}
}

func TestDeal_Products_OmitEmptyInnerFields(t *testing.T) {
	// All product pointers nil -> products should marshal as {}
	orig := Deal{
		Identifier: mustULID(t, "01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Client:     "f1client",
		Products:   Products{},
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal map: %v", err)
	}
	p, ok := m["products"]
	if !ok {
		t.Fatalf("products missing")
	}
	if obj, ok := p.(map[string]any); !ok || len(obj) != 0 {
		t.Fatalf("expected products to be {}, got %#v", p)
	}

	var round Deal
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("round unmarshal: %v", err)
	}
	if !reflect.DeepEqual(orig.Products, round.Products) {
		t.Fatalf("products changed on round trip: %#v -> %#v", orig.Products, round.Products)
	}
}

func TestPartialUnmarshal(t *testing.T) {
	iString := "{\"client\":\"t1k7ctd3hvmwwjdpb2ipd3kr7n4vk3xzfvzbbdrai\",\"data\":{\"format\":{\"raw\":{}},\"piece_cid\":{\"/\": \"bafkzcibfxx3meais7dgqlg24253d7s2unmxkczzlrnsoni6zmvjy6vi636nslfyggu3q\"},\"source_http_put\":{}},\"identifier\":\"01K4R3EK6QEPASQH8KFPKVBNWR\",\"products\":{\"pdp_v1\":{\"add_piece\":true,\"delete_data_set\":false,\"delete_piece\":false,\"extra_data\":[],\"record_keeper\":\"0x158c8f05A616403589b99BE5d82d756860363A92\"},\"retrieval_v1\":{\"announce_payload\":true,\"announce_piece\":true,\"indexing\":true}}}"
	var deal Deal
	if err := json.Unmarshal([]byte(iString), &deal); err != nil {
		t.Fatal(err)
	}
	require.NotNil(t, deal)
	require.NotNil(t, deal.Products)
	require.NotNil(t, deal.Products.PDPV1)
	require.Equal(t, false, deal.Products.PDPV1.CreateDataSet)
	require.Equal(t, true, deal.Products.PDPV1.AddPiece)
	require.Equal(t, "0x158c8f05A616403589b99BE5d82d756860363A92", deal.Products.PDPV1.RecordKeeper)
	require.True(t, deal.Data.PieceCID.Defined())
	require.NotNil(t, deal.Data.SourceHttpPut)
}
