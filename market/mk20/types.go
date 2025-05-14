package mk20

import (
	"net/http"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
)

// Deal represents a structure defining the details and components of a specific deal in the system.
type Deal struct {

	// Identifier represents a unique identifier for the deal in UUID format.
	Identifier ulid.ULID `json:"identifier"`

	// Data represents the source of piece data and associated metadata.
	Data DataSource `json:"data"`

	// Products represents a collection of product-specific information associated with a deal
	Products Products `json:"products"`
}

type Products struct {
	DDOV1 *DDOV1 `json:"ddov1"`
}

// DataSource represents the source of piece data, including metadata and optional methods to fetch or describe the data origin.
type DataSource struct {

	// PieceCID represents the unique identifier for a piece of data, stored as a CID object.
	PieceCID cid.Cid `json:"piececid"`

	// Size represents the size of the padded piece in the data source.
	Size abi.PaddedPieceSize `json:"size"`

	// Format defines the format of the piece data, which can include CAR, Aggregate, or Raw formats.
	Format PieceDataFormat `json:"format"`

	// SourceHTTP represents the HTTP-based source of piece data within a deal, including raw size and URLs for retrieval.
	SourceHTTP *DataSourceHTTP `json:"sourcehttp"`

	// SourceAggregate represents an aggregated source, comprising multiple data sources as pieces.
	SourceAggregate *DataSourceAggregate `json:"sourceaggregate"`

	// SourceOffline defines the data source for offline pieces, including raw size information.
	SourceOffline *DataSourceOffline `json:"sourceoffline"`

	// SourceHTTPPut // allow clients to push piece data after deal accepted, sort of like offline import
	SourceHttpPut *DataSourceHttpPut `json:"sourcehttpput"`

	// SourceStorageProvider -> sp IDs/ipni, pieceCids
}

// PieceDataFormat represents various formats in which piece data can be defined, including CAR files, aggregate formats, or raw byte data.
type PieceDataFormat struct {

	// Car represents the optional CAR file format, including its metadata and versioning details.
	Car *FormatCar `json:"car"`

	// Aggregate holds a reference to the aggregated format of piece data.
	Aggregate *FormatAggregate `json:"aggregate"`

	// Raw represents the raw format of the piece data, encapsulated as bytes.
	Raw *FormatBytes `json:"raw"`
}

// FormatCar represents the CAR (Content Addressable aRchive) format with version metadata for piece data serialization.
type FormatCar struct {
	Version uint64 `json:"version"`
}

// FormatAggregate represents the aggregated format for piece data, identified by its type.
type FormatAggregate struct {

	// Type specifies the type of aggregation for data pieces, represented by an AggregateType value.
	Type AggregateType `json:"type"`

	// Sub holds a slice of PieceDataFormat, representing various formats of piece data aggregated under this format.
	// The order must be same as segment index to avoid incorrect indexing of sub pieces in an aggregate
	Sub []PieceDataFormat `json:"sub"`
}

// FormatBytes defines the raw byte representation of data as a format.
type FormatBytes struct{}

// DataSourceOffline represents the data source for offline pieces, including metadata such as the raw size of the piece.
type DataSourceOffline struct {
	RawSize uint64 `json:"rawsize"`
}

func (dso *DataSourceOffline) Name() DataSourceName {
	return DataSourceNameOffline
}

func (dso *DataSourceOffline) IsEnabled(dbDataSources []dbDataSource) (int, error) {
	name := string(dso.Name())
	for _, p := range dbDataSources {
		if p.Name == name {
			if p.Enabled {
				return Ok, nil
			}
		}
	}
	return ErrUnsupportedDataSource, xerrors.Errorf("data source %s is not enabled", name)
}

// DataSourceAggregate represents an aggregated data source containing multiple individual DataSource pieces.
type DataSourceAggregate struct {
	Pieces []DataSource `json:"pieces"`
}

func (dsa *DataSourceAggregate) Name() DataSourceName {
	return DataSourceNameAggregate
}

func (dsa *DataSourceAggregate) IsEnabled(dbDataSources []dbDataSource) (int, error) {
	name := string(dsa.Name())
	for _, p := range dbDataSources {
		if p.Name == name {
			if p.Enabled {
				return Ok, nil
			}
		}
	}
	return ErrUnsupportedDataSource, xerrors.Errorf("data source %s is not enabled", name)
}

// DataSourceHTTP represents an HTTP-based data source for retrieving piece data, including its raw size and associated URLs.
type DataSourceHTTP struct {

	// RawSize specifies the raw size of the data in bytes.
	RawSize uint64 `json:"rawsize"`

	// URLs lists the HTTP endpoints where the piece data can be fetched.
	URLs []HttpUrl `json:"urls"`
}

func (dsh *DataSourceHTTP) Name() DataSourceName {
	return DataSourceNameHTTP
}

func (dsh *DataSourceHTTP) IsEnabled(dbDataSources []dbDataSource) (int, error) {
	name := string(dsh.Name())
	for _, p := range dbDataSources {
		if p.Name == name {
			if p.Enabled {
				return Ok, nil
			}
		}
	}
	return ErrUnsupportedDataSource, xerrors.Errorf("data source %s is not enabled", name)
}

// HttpUrl represents an HTTP endpoint configuration for fetching piece data.
type HttpUrl struct {

	// URL specifies the HTTP endpoint where the piece data can be fetched.
	URL string `json:"url"`

	// HTTPHeaders represents the HTTP headers associated with the URL.
	HTTPHeaders http.Header `json:"httpheaders"`

	// Priority indicates the order preference for using the URL in requests, with lower values having higher priority.
	Priority uint64 `json:"priority"`

	// Fallback indicates whether this URL serves as a fallback option when other URLs fail.
	Fallback bool `json:"fallback"`
}

type DataSourceHttpPut struct {
	RawSize uint64 `json:"rawsize"`
}

func (dsh *DataSourceHttpPut) Name() DataSourceName {
	return DataSourceNamePut
}

func (dsh *DataSourceHttpPut) IsEnabled(dbDataSources []dbDataSource) (int, error) {
	name := string(dsh.Name())
	for _, p := range dbDataSources {
		if p.Name == name {
			if p.Enabled {
				return Ok, nil
			}
		}
	}
	return ErrUnsupportedDataSource, xerrors.Errorf("data source %s is not enabled", name)
}

// AggregateType represents an unsigned integer used to define the type of aggregation for data pieces in the system.
type AggregateType uint64

const (
	AggregateTypeNone AggregateType = iota
	AggregateTypeV1
)

type ErrCode int

const (
	Ok                         = 200
	ErrBadProposal             = 400
	ErrMalformedDataSource     = 430
	ErrUnsupportedDataSource   = 422
	ErrUnsupportedProduct      = 423
	ErrProductNotEnabled       = 424
	ErrProductValidationFailed = 425
	ErrDealRejectedByMarket    = 426
	ErrServiceMaintenance      = 503
	ErrServiceOverloaded       = 429
	ErrMarketNotEnabled        = 440
	ErrDurationTooShort        = 441
)

type ProductName string

const (
	ProductNameDDOV1 ProductName = "ddov1"
)

type DataSourceName string

const (
	DataSourceNameHTTP            DataSourceName = "http"
	DataSourceNameAggregate       DataSourceName = "aggregate"
	DataSourceNameOffline         DataSourceName = "offline"
	DataSourceNameStorageProvider DataSourceName = "storageprovider"
	DataSourceNamePDP             DataSourceName = "pdp"
	DataSourceNamePut             DataSourceName = "put"
)

type dbDataSource struct {
	Name    string `db:"name"`
	Enabled bool   `db:"enabled"`
}

// TODO: Client facing UI Page for SP
// TODO: Contract SP details pathway - sptool?
// TODO: SPID data source
// TODO: Test contract
// TODO: ACLv1?
