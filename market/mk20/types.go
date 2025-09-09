package mk20

import (
	"net/http"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// Deal represents a structure defining the details and components of a specific deal in the system.
type Deal struct {

	// Identifier represents a unique identifier for the deal in ULID format.
	Identifier ulid.ULID `json:"identifier"`

	// Client wallet string for the deal
	Client string `json:"client"`

	// Data represents the source of piece data and associated metadata.
	Data *DataSource `json:"data,omitempty"`

	// Products represents a collection of product-specific information associated with a deal
	Products Products `json:"products"`
}

type Products struct {
	// DDOV1 represents a product v1 configuration for Direct Data Onboarding (DDO)
	DDOV1 *DDOV1 `json:"ddo_v1,omitempty"`

	// RetrievalV1 represents configuration for retrieval settings in the system, including indexing and announcement flags.
	RetrievalV1 *RetrievalV1 `json:"retrieval_v1,omitempty"`

	// PDPV1 represents product-specific configuration for PDP version 1 deals.
	PDPV1 *PDPV1 `json:"pdp_v1,omitempty"`
}

// DataSource represents the source of piece data, including metadata and optional methods to fetch or describe the data origin.
type DataSource struct {

	// PieceCID represents the unique identifier (pieceCID V2) for a piece of data, stored as a CID object.
	PieceCID cid.Cid `json:"piece_cid"`

	// Format defines the format of the piece data, which can include CAR, Aggregate, or Raw formats.
	Format PieceDataFormat `json:"format"`

	// SourceHTTP represents the HTTP-based source of piece data within a deal, including raw size and URLs for retrieval.
	SourceHTTP *DataSourceHTTP `json:"source_http,omitempty"`

	// SourceAggregate represents an aggregated source, comprising multiple data sources as pieces.
	SourceAggregate *DataSourceAggregate `json:"source_aggregate,omitempty"`

	// SourceOffline defines the data source for offline pieces, including raw size information.
	SourceOffline *DataSourceOffline `json:"source_offline,omitempty"`

	// SourceHttpPut allow clients to push piece data after deal is accepted
	SourceHttpPut *DataSourceHttpPut `json:"source_http_put,omitempty"`
}

// PieceDataFormat represents various formats in which piece data can be defined, including CAR files, aggregate formats, or raw byte data.
type PieceDataFormat struct {

	// Car represents the optional CAR file format.
	Car *FormatCar `json:"car,omitempty"`

	// Aggregate holds a reference to the aggregated format of piece data.
	Aggregate *FormatAggregate `json:"aggregate,omitempty"`

	// Raw represents the raw format of the piece data, encapsulated as bytes.
	Raw *FormatBytes `json:"raw,omitempty"`
}

// FormatCar represents the CAR (Content Addressable archive) format for piece data serialization.
type FormatCar struct{}

// FormatAggregate represents the aggregated format for piece data, identified by its type.
type FormatAggregate struct {

	// Type specifies the type of aggregation for data pieces, represented by an AggregateType value.
	Type AggregateType `json:"type"`

	// Sub holds a slice of DataSource, representing details of sub pieces aggregated under this format.
	// The order must be same as segment index to avoid incorrect indexing of sub pieces in an aggregate
	Sub []DataSource `json:"sub"`
}

// FormatBytes defines the raw byte representation of data as a format.
type FormatBytes struct{}

// DataSourceOffline represents the data source for offline pieces.
type DataSourceOffline struct{}

// DataSourceAggregate represents an aggregated data source containing multiple individual DataSource pieces.
type DataSourceAggregate struct {
	Pieces []DataSource `json:"pieces"`
}

// DataSourceHTTP represents an HTTP-based data source for retrieving piece data, including associated URLs.
type DataSourceHTTP struct {
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
	Priority int `json:"priority"`

	// Fallback indicates whether this URL serves as a fallback option when other URLs fail.
	Fallback bool `json:"fallback"`
}

// DataSourceHttpPut represents a data source allowing clients to push piece data after a deal is accepted.
type DataSourceHttpPut struct{}

// AggregateType represents an unsigned integer used to define the type of aggregation for data pieces in the system.
type AggregateType int

const (

	// AggregateTypeNone represents the default aggregation type, indicating no specific aggregation is applied.
	AggregateTypeNone AggregateType = iota

	// AggregateTypeV1 represents the first version of the aggregate type in the system. This is current PODSI aggregation
	// based on https://github.com/filecoin-project/FIPs/blob/master/FRCs/frc-0058.md
	AggregateTypeV1
)

// DealCode represents an error code as an integer value
type DealCode int

const (

	// Ok represents a successful operation with an HTTP status code of 200.
	Ok DealCode = 200

	// ErrUnAuthorized represents an error indicating unauthorized access with the code 401.
	ErrUnAuthorized DealCode = 401

	// ErrBadProposal represents a validation error that indicates an invalid or malformed proposal input in the context of validation logic.
	ErrBadProposal DealCode = 400

	// ErrDealNotFound indicates that the specified deal could not be found, corresponding to the HTTP status code 404.
	ErrDealNotFound DealCode = 404

	// ErrMalformedDataSource indicates that the provided data source is incorrectly formatted or contains invalid data.
	ErrMalformedDataSource DealCode = 430

	// ErrUnsupportedDataSource indicates the specified data source is not supported or disabled for use in the current context.
	ErrUnsupportedDataSource DealCode = 422

	// ErrUnsupportedProduct indicates that the requested product is not supported by the provider.
	ErrUnsupportedProduct DealCode = 423

	// ErrProductNotEnabled indicates that the requested product is not enabled on the provider.
	ErrProductNotEnabled DealCode = 424

	// ErrProductValidationFailed indicates a failure during product-specific validation due to invalid or missing data.
	ErrProductValidationFailed DealCode = 425

	// ErrDealRejectedByMarket indicates that a proposed deal was rejected by the market for not meeting its acceptance criteria or rules.
	ErrDealRejectedByMarket DealCode = 426

	// ErrServerInternalError indicates an internal server error with a corresponding error code of 500.
	ErrServerInternalError DealCode = 500

	// ErrServiceMaintenance indicates that the service is temporarily unavailable due to maintenance, corresponding to HTTP status code 503.
	ErrServiceMaintenance DealCode = 503

	// ErrServiceOverloaded indicates that the service is overloaded and cannot process the request at the moment.
	ErrServiceOverloaded DealCode = 429

	// ErrMarketNotEnabled indicates that the market is not enabled for the requested operation.
	ErrMarketNotEnabled DealCode = 440

	// ErrDurationTooShort indicates that the provided duration value does not meet the minimum required threshold.
	ErrDurationTooShort DealCode = 441
)

// ProductName represents a type for defining the product name identifier used in various operations and validations.
type ProductName string

const (
	// ProductNameDDOV1 represents the identifier for the "ddo_v1" product used in contract operations and validations.
	ProductNameDDOV1       ProductName = "ddo_v1"
	ProductNamePDPV1       ProductName = "pdp_v1"
	ProductNameRetrievalV1 ProductName = "retrieval_v1"
)

type DataSourceName string

const (
	DataSourceNameHTTP      DataSourceName = "http"
	DataSourceNameAggregate DataSourceName = "aggregate"
	DataSourceNameOffline   DataSourceName = "offline"
	DataSourceNamePut       DataSourceName = "put"
)

type product interface {
	Validate(db *harmonydb.DB, cfg *config.MK20Config) (DealCode, error)
	ProductName() ProductName
}

// UploadStatusCode defines the return codes for the upload status
type UploadStatusCode int

const (

	// UploadStatusCodeOk represents a successful upload operation with status code 200.
	UploadStatusCodeOk UploadStatusCode = 200

	// UploadStatusCodeDealNotFound indicates that the requested deal was not found, corresponding to status code 404.
	UploadStatusCodeDealNotFound UploadStatusCode = 404

	// UploadStatusCodeUploadNotStarted indicates that the upload process has not started yet.
	UploadStatusCodeUploadNotStarted UploadStatusCode = 425

	// UploadStatusCodeServerError indicates an internal server error occurred during the upload process, corresponding to status code 500.
	UploadStatusCodeServerError UploadStatusCode = 500
)

// UploadStartCode represents an integer type for return codes related to the upload start process.
type UploadStartCode int

const (

	// UploadStartCodeOk indicates a successful upload start request with status code 200.
	UploadStartCodeOk UploadStartCode = 200

	// UploadStartCodeBadRequest indicates a bad upload start request error with status code 400.
	UploadStartCodeBadRequest UploadStartCode = 400

	// UploadStartCodeDealNotFound represents a 404 status indicating the deal was not found during the upload start process.
	UploadStartCodeDealNotFound UploadStartCode = 404

	// UploadStartCodeAlreadyStarted indicates that the upload process has already been initiated and cannot be started again.
	UploadStartCodeAlreadyStarted UploadStartCode = 409

	// UploadStartCodeServerError indicates an error occurred on the server while processing an upload start request.
	UploadStartCodeServerError UploadStartCode = 500
)

// UploadCode represents return codes related to upload operations, typically based on HTTP status codes.
type UploadCode int

const (

	// UploadOk indicates a successful upload operation, represented by the HTTP status code 200.
	UploadOk UploadCode = 200

	// UploadBadRequest represents a bad request error with an HTTP status code of 400.
	UploadBadRequest UploadCode = 400

	// UploadNotFound represents an error where the requested upload chunk could not be found, typically corresponding to HTTP status 404.
	UploadNotFound UploadCode = 404

	// UploadChunkAlreadyUploaded indicates that the chunk has already been uploaded and cannot be re-uploaded.
	UploadChunkAlreadyUploaded UploadCode = 409

	// UploadServerError indicates a server-side error occurred during the upload process, represented by the HTTP status code 500.
	UploadServerError UploadCode = 500

	// UploadRateLimit indicates that the upload operation is being rate-limited, corresponding to the HTTP status code 429.
	UploadRateLimit UploadCode = 429
)
