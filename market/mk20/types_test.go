package mk20

import (
	"testing"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/builtin/v16/verifreg"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

var (
	testCID, _ = cid.Parse("baga6ea4seaqnnlsm75qhc4h76ts6bytfdxf6epjgqlhozjtuony4fwlui2xfuhq")
	raw2MiB    = uint64(2 << 20) // un‑padded 2MiB
	padded2MiB = padreader.PaddedSize(raw2MiB).Padded()
)

// ───────────────────────────────────────────────────────────────────────────────
// helpers to create *valid* structs that individual test‑cases mutate
// ───────────────────────────────────────────────────────────────────────────────

func validDBDataSources() []dbDataSource {
	return []dbDataSource{
		{Name: "http", Enabled: true},
		{Name: "offline", Enabled: true},
		{Name: "aggregate", Enabled: true},
		{Name: "put", Enabled: true},
	}
}

func validDBProducts() []dbProduct { return []dbProduct{{Name: "ddov1", Enabled: true}} }

func validDataSource() DataSource {
	return DataSource{
		PieceCID: testCID,
		Size:     padded2MiB,
		Format: PieceDataFormat{
			Car: &FormatCar{Version: 1},
		},
		SourceHTTP: &DataSourceHTTP{
			RawSize: raw2MiB,
			URLs:    []HttpUrl{{URL: "https://example.com/file.car"}},
		},
	}
}

func validDDOV1() *DDOV1 {
	sp, _ := address.NewFromString("f01234")
	cl, _ := address.NewFromString("f05678")
	pm, _ := address.NewFromString("f09999")

	return &DDOV1{
		Provider:                   sp,
		Client:                     cl,
		PieceManager:               pm,
		Duration:                   518400,
		ContractAddress:            "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ContractDealIDMethod:       "dealID",
		ContractDealIDMethodParams: []byte{0x01},
	}
}

func validDeal(t *testing.T) Deal {
	id, err := NewULID()
	require.NoError(t, err)
	return Deal{
		Identifier: id,
		Data:       validDataSource(),
		Products:   Products{DDOV1: validDDOV1()},
	}
}

// ───────────────────────────────────────────────────────────────────────────────
// 1.  Products.Validate  +  DDOV1.Validate
// ───────────────────────────────────────────────────────────────────────────────

func TestValidate_DDOV1(t *testing.T) {
	base := *validDDOV1() // copy
	tests := []struct {
		name     string
		prod     []dbProduct
		mutate   func(*DDOV1)
		wantCode int
	}{
		// enabled / disabled / unsupported
		{"no products on provider",
			nil,
			func(d *DDOV1) {},
			ErrUnsupportedProduct},
		{"product disabled",
			[]dbProduct{{Name: "ddov1", Enabled: false}},
			func(d *DDOV1) {},
			ErrProductNotEnabled},
		{"product unsupported",
			[]dbProduct{{Name: "other", Enabled: true}},
			func(d *DDOV1) {},
			ErrUnsupportedProduct},

		// field‑level failures
		{"provider undef", validDBProducts(),
			func(d *DDOV1) { d.Provider = address.Undef },
			ErrProductValidationFailed},
		{"client undef", validDBProducts(),
			func(d *DDOV1) { d.Client = address.Undef },
			ErrProductValidationFailed},
		{"piece‑manager undef", validDBProducts(),
			func(d *DDOV1) { d.PieceManager = address.Undef },
			ErrProductValidationFailed},
		{"allocation id == NoAllocationID", validDBProducts(),
			func(d *DDOV1) {
				na := verifreg.NoAllocationID
				d.AllocationId = &na
			},
			ErrProductValidationFailed},
		{"duration too short", validDBProducts(),
			func(d *DDOV1) { d.Duration = 10 },
			ErrDurationTooShort},
		{"contract address empty", validDBProducts(),
			func(d *DDOV1) { d.ContractAddress = "" },
			ErrProductValidationFailed},
		{"contract address no 0x", validDBProducts(),
			func(d *DDOV1) { d.ContractAddress = "abc" },
			ErrProductValidationFailed},
		{"contract params nil", validDBProducts(),
			func(d *DDOV1) { d.ContractDealIDMethodParams = nil },
			ErrProductValidationFailed},
		{"contract method empty", validDBProducts(),
			func(d *DDOV1) { d.ContractDealIDMethod = "" },
			ErrProductValidationFailed},

		// happy path
		{"happy path", validDBProducts(),
			func(d *DDOV1) {},
			Ok},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d := base
			tc.mutate(&d)
			code, _ := d.Validate(tc.prod)
			require.Equal(t, tc.wantCode, code)
		})
	}
}

// ───────────────────────────────────────────────────────────────────────────────
// 2.  DataSource.Validate (all branches)
// ───────────────────────────────────────────────────────────────────────────────

func TestValidate_DataSource(t *testing.T) {
	baseDB := validDBDataSources()
	tests := []struct {
		name        string
		mutateDS    func(*DataSource)
		mutateDBSrc func([]dbDataSource) []dbDataSource
		wantCode    int
	}{
		// provider‑level enable / disable checks
		{"no data sources enabled",
			func(ds *DataSource) {}, func(_ []dbDataSource) []dbDataSource { return nil },
			ErrUnsupportedDataSource},
		{"http disabled",
			func(ds *DataSource) {},
			func(src []dbDataSource) []dbDataSource { src[0].Enabled = false; return src },
			ErrUnsupportedDataSource},

		// top‑level sanity
		{"undefined CID",
			func(ds *DataSource) { ds.PieceCID = cid.Undef },
			nil, ErrBadProposal},
		{"size zero",
			func(ds *DataSource) { ds.Size = 0 },
			nil, ErrBadProposal},
		{"no source defined",
			func(ds *DataSource) {
				ds.SourceHTTP = nil
			}, nil, ErrBadProposal},
		{"multiple sources defined",
			func(ds *DataSource) {
				ds.SourceOffline = &DataSourceOffline{RawSize: raw2MiB}
				ds.SourceHttpPut = &DataSourceHttpPut{RawSize: raw2MiB}
				ds.SourceAggregate = &DataSourceAggregate{Pieces: []DataSource{}}
			}, nil, ErrBadProposal},

		// format combinations
		{"no format",
			func(ds *DataSource) { ds.Format = PieceDataFormat{} },
			nil, ErrBadProposal},
		{"multiple formats",
			func(ds *DataSource) {
				ds.Format.Raw = &FormatBytes{}
			}, nil, ErrBadProposal},
		{"car version unsupported",
			func(ds *DataSource) { ds.Format.Car.Version = 3 },
			nil, ErrMalformedDataSource},

		// HTTP source specific
		{"http rawsize zero",
			func(ds *DataSource) { ds.SourceHTTP.RawSize = 0 },
			nil, ErrMalformedDataSource},
		{"http urls empty",
			func(ds *DataSource) { ds.SourceHTTP.URLs = nil },
			nil, ErrMalformedDataSource},
		{"http url invalid",
			func(ds *DataSource) { ds.SourceHTTP.URLs[0].URL = "::::" },
			nil, ErrMalformedDataSource},

		// Offline source
		{"offline source disabled",
			func(ds *DataSource) {
				ds.SourceHTTP = nil
				ds.SourceOffline = &DataSourceOffline{RawSize: raw2MiB}
			},
			func(src []dbDataSource) []dbDataSource { src[1].Enabled = false; return src },
			ErrUnsupportedDataSource},
		{"offline rawsize zero",
			func(ds *DataSource) {
				ds.SourceHTTP = nil
				ds.SourceOffline = &DataSourceOffline{RawSize: 0}
			}, nil, ErrMalformedDataSource},

		// HttpPut source
		{"put source disabled",
			func(ds *DataSource) {
				ds.SourceHTTP = nil
				ds.SourceHttpPut = &DataSourceHttpPut{RawSize: raw2MiB}
			},
			func(src []dbDataSource) []dbDataSource { src[3].Enabled = false; return src },
			ErrUnsupportedDataSource},
		{"put rawsize zero",
			func(ds *DataSource) {
				ds.SourceHTTP = nil
				ds.SourceHttpPut = &DataSourceHttpPut{RawSize: 0}
			}, nil, ErrMalformedDataSource},

		// Size mismatch on final check
		{"declared size mismatch",
			func(ds *DataSource) { ds.Size *= 2 },
			nil, ErrBadProposal},

		// happy path
		{"happy path",
			func(ds *DataSource) {},
			nil, Ok},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ds := validDataSource()
			tc.mutateDS(&ds)

			db := baseDB
			if tc.mutateDBSrc != nil {
				db = tc.mutateDBSrc(append([]dbDataSource(nil), baseDB...))
			}
			code, _ := ds.Validate(db)
			require.Equal(t, tc.wantCode, code)
		})
	}
}

// ───────────────────────────────────────────────────────────────────────────────
// 3.  Deal.Validate (composition of the two)
// ───────────────────────────────────────────────────────────────────────────────

func TestValidate_Deal(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*Deal, *productAndDataSource)
		wantCode int
	}{
		{"happy path",
			func(d *Deal, _ *productAndDataSource) {},
			Ok},

		// propagate product failure
		{"product failure bubbles",
			func(d *Deal, pad *productAndDataSource) {
				pad.Products[0].Enabled = false // DDOV1 disabled
			}, ErrProductNotEnabled},

		// propagate data source failure
		{"data source failure bubbles",
			func(d *Deal, _ *productAndDataSource) {
				d.Data.PieceCID = cid.Undef
			}, ErrBadProposal},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			deal := validDeal(t)
			pad := &productAndDataSource{
				Products: append([]dbProduct(nil), validDBProducts()...),
				Data:     append([]dbDataSource(nil), validDBDataSources()...),
			}
			tc.mutate(&deal, pad)
			code, _ := deal.Validate(pad)
			require.Equal(t, tc.wantCode, code)
		})
	}
}

// ───────────────────────────────────────────────────────────────────────────────
// 4.  quick sanity that URL parsing in Validate works on aggregate sub‑pieces
// ───────────────────────────────────────────────────────────────────────────────

func TestValidate_Aggregate_SubPieceChecks(t *testing.T) {
	// base structure: an aggregate of one valid HTTP piece
	sub := validDataSource()
	agg := validDataSource()
	agg.Format = PieceDataFormat{
		Aggregate: &FormatAggregate{
			Type: AggregateTypeV1,
			Sub:  nil,
		},
	}
	agg.SourceHTTP = nil
	agg.SourceAggregate = &DataSourceAggregate{Pieces: []DataSource{sub}}

	// (size will mismatch  – test expects that specific error branch)
	agg.Size = padded2MiB * 8

	code, _ := agg.Validate(validDBDataSources())
	require.Equal(t, ErrBadProposal, code)
}
