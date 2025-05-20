package mk20

import (
	"net/http"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"

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
	// DDOV1 represents a product v1 configuration for Direct Data Onboarding (DDO)
	DDOV1 *DDOV1 `json:"ddo_v1"`
}

// DataSource represents the source of piece data, including metadata and optional methods to fetch or describe the data origin.
type DataSource struct {

	// PieceCID represents the unique identifier for a piece of data, stored as a CID object.
	PieceCID cid.Cid `json:"piece_cid"`

	// Size represents the size of the padded piece in the data source.
	Size abi.PaddedPieceSize `json:"size"`

	// Format defines the format of the piece data, which can include CAR, Aggregate, or Raw formats.
	Format PieceDataFormat `json:"format"`

	// SourceHTTP represents the HTTP-based source of piece data within a deal, including raw size and URLs for retrieval.
	SourceHTTP *DataSourceHTTP `json:"source_http"`

	// SourceAggregate represents an aggregated source, comprising multiple data sources as pieces.
	SourceAggregate *DataSourceAggregate `json:"source_aggregate"`

	// SourceOffline defines the data source for offline pieces, including raw size information.
	SourceOffline *DataSourceOffline `json:"source_offline"`

	// SourceHTTPPut // allow clients to push piece data after deal accepted, sort of like offline import
	SourceHttpPut *DataSourceHttpPut `json:"source_httpput"`

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

// FormatCar represents the CAR (Content Addressable archive) format for piece data serialization.
type FormatCar struct{}

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
	// RawSize specifies the raw size of the data in bytes.
	RawSize uint64 `json:"raw_size"`
}

// DataSourceAggregate represents an aggregated data source containing multiple individual DataSource pieces.
type DataSourceAggregate struct {
	Pieces []DataSource `json:"pieces"`
}

// DataSourceHTTP represents an HTTP-based data source for retrieving piece data, including its raw size and associated URLs.
type DataSourceHTTP struct {

	// RawSize specifies the raw size of the data in bytes.
	RawSize uint64 `json:"rawsize"`

	// URLs lists the HTTP endpoints where the piece data can be fetched.
	URLs []HttpUrl `json:"urls"`
}

// HttpUrl represents an HTTP endpoint configuration for fetching piece data.
type HttpUrl struct {

	// URL specifies the HTTP endpoint where the piece data can be fetched.
	URL string `json:"url"`

	// HTTPHeaders represents the HTTP headers associated with the URL.
	Headers http.Header `json:"headers"`

	// Priority indicates the order preference for using the URL in requests, with lower values having higher priority.
	Priority uint64 `json:"priority"`

	// Fallback indicates whether this URL serves as a fallback option when other URLs fail.
	Fallback bool `json:"fallback"`
}

// DataSourceHttpPut represents a data source allowing clients to push piece data after a deal is accepted.
type DataSourceHttpPut struct {
	// RawSize specifies the raw size of the data in bytes.
	RawSize uint64 `json:"raw_size"`
}

// AggregateType represents an unsigned integer used to define the type of aggregation for data pieces in the system.
type AggregateType uint64

const (

	// AggregateTypeNone represents the default aggregation type, indicating no specific aggregation is applied.
	AggregateTypeNone AggregateType = iota

	// AggregateTypeV1 represents the first version of the aggregate type in the system.
	AggregateTypeV1
)

// ErrorCode represents an error code as an integer value
type ErrorCode int

const (

	// Ok represents a successful operation with an HTTP status code of 200.
	Ok ErrorCode = 200

	// ErrBadProposal represents a validation error that indicates an invalid or malformed proposal input in the context of validation logic.
	ErrBadProposal ErrorCode = 400

	// ErrMalformedDataSource indicates that the provided data source is incorrectly formatted or contains invalid data.
	ErrMalformedDataSource ErrorCode = 430

	// ErrUnsupportedDataSource indicates the specified data source is not supported or disabled for use in the current context.
	ErrUnsupportedDataSource ErrorCode = 422

	// ErrUnsupportedProduct indicates that the requested product is not supported by the provider.
	ErrUnsupportedProduct ErrorCode = 423

	// ErrProductNotEnabled indicates that the requested product is not enabled on the provider.
	ErrProductNotEnabled ErrorCode = 424

	// ErrProductValidationFailed indicates a failure during product-specific validation due to invalid or missing data.
	ErrProductValidationFailed ErrorCode = 425

	// ErrDealRejectedByMarket indicates that a proposed deal was rejected by the market for not meeting its acceptance criteria or rules.
	ErrDealRejectedByMarket ErrorCode = 426

	// ErrServiceMaintenance indicates that the service is temporarily unavailable due to maintenance, corresponding to HTTP status code 503.
	ErrServiceMaintenance ErrorCode = 503

	// ErrServiceOverloaded indicates that the service is overloaded and cannot process the request at the moment.
	ErrServiceOverloaded ErrorCode = 429

	// ErrMarketNotEnabled indicates that the market is not enabled for the requested operation.
	ErrMarketNotEnabled ErrorCode = 440

	// ErrDurationTooShort indicates that the provided duration value does not meet the minimum required threshold.
	ErrDurationTooShort ErrorCode = 441
)

// ProductName represents a type for defining the product name identifier used in various operations and validations.
type ProductName string

const (
	// ProductNameDDOV1 represents the identifier for the "ddo_v1" product used in contract operations and validations.
	ProductNameDDOV1 ProductName = "ddo_v1"
)

type DataSourceName string

const (
	DataSourceNameHTTP            DataSourceName = "http"
	DataSourceNameAggregate       DataSourceName = "aggregate"
	DataSourceNameOffline         DataSourceName = "offline"
	DataSourceNameStorageProvider DataSourceName = "storage_provider"
	DataSourceNamePDP             DataSourceName = "pdp"
	DataSourceNamePut             DataSourceName = "put"
)
