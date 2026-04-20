package mk20

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/mr-tron/base58"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"
	fcrypto "github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/lib/sigs"
)

func (d *Deal) Validate(db *harmonydb.DB, cfg *config.MK20Config, Auth string) (DealCode, error) {
	defer func() {
		if r := recover(); r != nil {
			trace := make([]byte, 1<<16)
			n := runtime.Stack(trace, false)
			log.Errorf("panic occurred in validation: %v\n%s", r, trace[:n])
			debug.PrintStack()
		}
	}()

	err := validateClient(d.Client, Auth)
	if err != nil {
		return ErrBadProposal, err
	}

	code, err := d.Products.Validate(db, cfg)
	if err != nil {
		return code, xerrors.Errorf("products validation failed: %w", err)
	}

	// Validate data if present
	if d.Data != nil {
		return d.Data.Validate(db)
	}

	// Return without validating data for initial phase of /Put deals or PDP Delete deals
	return Ok, nil
}

func validateClient(client string, auth string) error {
	if client == "" {
		return xerrors.Errorf("client is empty")
	}

	keyType, pubKey, _, err := parseCustomAuth(auth)
	if err != nil {
		return xerrors.Errorf("parsing auth header: %w", err)
	}

	switch keyType {
	case "ed25519":
		kStr, err := ED25519ToString(pubKey)
		if err != nil {
			return xerrors.Errorf("invalid public key for auth header: %w", err)
		}
		if client != kStr {
			return xerrors.Errorf("client in deal does not match client in auth header")
		}
		return nil
	case "secp256k1", "bls", "delegated":
		addr, err := address.NewFromBytes(pubKey)
		if err != nil {
			return xerrors.Errorf("invalid public key for auth header: %w", err)
		}
		if client != addr.String() {
			return xerrors.Errorf("client in deal does not match client in auth header")
		}
		return nil
	default:
		return fmt.Errorf("unsupported key type: %s", keyType)
	}
}

func (d *DataSource) Validate(db *harmonydb.DB) (DealCode, error) {

	err := ValidatePieceCID(d.PieceCID)
	if err != nil {
		return ErrBadProposal, err
	}

	sourceCount := 0

	if d.SourceHTTP != nil {
		sourceCount++
	}

	if d.SourceOffline != nil {
		sourceCount++
	}

	if d.SourceAggregate != nil {
		sourceCount++
	}

	if d.SourceHttpPut != nil {
		sourceCount++
	}

	if sourceCount > 1 {
		return ErrBadProposal, xerrors.Errorf("multiple sources defined for data source")
	}

	if d.SourceOffline == nil && d.SourceHTTP == nil && d.SourceAggregate == nil && d.SourceHttpPut == nil {
		return ErrBadProposal, xerrors.Errorf("no source defined for data source")
	}

	var fcar, fagg, fraw bool

	if d.Format.Car != nil {
		fcar = true
	}

	if d.Format.Aggregate != nil {
		fagg = true

		if d.Format.Aggregate.Type != AggregateTypeV1 {
			return ErrMalformedDataSource, xerrors.Errorf("aggregate type not supported")
		}

		// If client will supply individual pieces
		if d.SourceAggregate != nil {
			code, err := IsDataSourceEnabled(db, d.SourceAggregate.Name())
			if err != nil {
				return code, err
			}

			if len(d.SourceAggregate.Pieces) == 0 {
				return ErrMalformedDataSource, xerrors.Errorf("no pieces in aggregate")
			}

			if len(d.SourceAggregate.Pieces) == 1 {
				return ErrMalformedDataSource, xerrors.Errorf("aggregate must have at least 2 pieces")
			}

			for _, p := range d.SourceAggregate.Pieces {
				err := ValidatePieceCID(p.PieceCID)
				if err != nil {
					return ErrMalformedDataSource, xerrors.Errorf("invalid piece cid")
				}

				var ifcar, ifraw bool

				if p.Format.Car != nil {
					ifcar = true
				}

				if p.Format.Aggregate != nil {
					return ErrMalformedDataSource, xerrors.Errorf("aggregate of aggregate is not supported")
				}

				if p.Format.Raw != nil {
					ifraw = true
				}

				if !ifcar && !ifraw {
					return ErrMalformedDataSource, xerrors.Errorf("no format defined for sub piece in aggregate")
				}

				if ifcar && ifraw {
					return ErrMalformedDataSource, xerrors.Errorf("multiple formats defined for sub piece in aggregate")
				}

				if p.SourceAggregate != nil {
					return ErrMalformedDataSource, xerrors.Errorf("aggregate of aggregate is not supported")
				}

				if p.SourceOffline == nil && p.SourceHTTP == nil {
					return ErrMalformedDataSource, xerrors.Errorf("no source defined for sub piece in aggregate")
				}

				if p.SourceOffline != nil && p.SourceHTTP != nil {
					return ErrMalformedDataSource, xerrors.Errorf("multiple sources defined for sub piece in aggregate")
				}

				if p.SourceHTTP != nil {
					if len(p.SourceHTTP.URLs) == 0 {
						return ErrMalformedDataSource, xerrors.Errorf("no urls defined for sub piece in aggregate")
					}

					for _, u := range p.SourceHTTP.URLs {
						_, err := url.Parse(u.URL)
						if err != nil {
							return ErrMalformedDataSource, xerrors.Errorf("invalid url")
						}
					}
				}
			}
			if len(d.Format.Aggregate.Sub) > 0 {
				return ErrMalformedDataSource, xerrors.Errorf("sub pieces cannot be defined when dataSource is aggregate")
			}
		} else {
			// If client will supply pre-aggregated piece
			if len(d.Format.Aggregate.Sub) == 0 {
				return ErrMalformedDataSource, xerrors.Errorf("no sub pieces defined under aggregate")
			}
			for _, p := range d.Format.Aggregate.Sub {
				err := ValidatePieceCID(p.PieceCID)
				if err != nil {
					return ErrMalformedDataSource, xerrors.Errorf("invalid piece cid")
				}
				var ifcar, ifraw bool
				if p.Format.Car != nil {
					ifcar = true
				}

				if p.Format.Aggregate != nil {
					return ErrMalformedDataSource, xerrors.Errorf("aggregate of aggregate is not supported")
				}

				if p.Format.Raw != nil {
					ifraw = true
				}
				if !ifcar && !ifraw {
					return ErrMalformedDataSource, xerrors.Errorf("no format defined for sub piece in aggregate")
				}
				if ifcar && ifraw {
					return ErrMalformedDataSource, xerrors.Errorf("multiple formats defined for sub piece in aggregate")
				}
				if p.SourceAggregate != nil || p.SourceOffline != nil || p.SourceHTTP != nil || p.SourceHttpPut != nil {
					return ErrMalformedDataSource, xerrors.Errorf("sub piece of pre-aggregated piece cannot have source defined")
				}
			}
		}
	}

	if d.Format.Raw != nil {
		fraw = true
	}

	if !fcar && !fagg && !fraw {
		return ErrBadProposal, xerrors.Errorf("no format defined")
	}

	if fcar && fagg || fcar && fraw || fagg && fraw {
		return ErrBadProposal, xerrors.Errorf("multiple formats defined")
	}

	if d.SourceHTTP != nil {
		code, err := IsDataSourceEnabled(db, d.SourceHTTP.Name())
		if err != nil {
			return code, err
		}

		if len(d.SourceHTTP.URLs) == 0 {
			return ErrMalformedDataSource, xerrors.Errorf("no urls defined")
		}

		for _, u := range d.SourceHTTP.URLs {
			_, err := url.Parse(u.URL)
			if err != nil {
				return ErrMalformedDataSource, xerrors.Errorf("invalid url")
			}
		}
	}

	if d.SourceOffline != nil {
		code, err := IsDataSourceEnabled(db, d.SourceOffline.Name())
		if err != nil {
			return code, err
		}
	}

	if d.SourceHttpPut != nil {
		code, err := IsDataSourceEnabled(db, d.SourceHttpPut.Name())
		if err != nil {
			return code, err
		}
	}

	return Ok, nil
}

func ValidatePieceCID(c cid.Cid) error {
	if !c.Defined() {
		return xerrors.Errorf("piece cid is not defined")
	}

	if c.Prefix().Codec != cid.Raw {
		return xerrors.Errorf("piece cid is not raw")
	}

	_, rawSize, err := commcid.PieceCidV1FromV2(c)
	if err != nil {
		return xerrors.Errorf("invalid piece cid: %w", err)
	}

	if rawSize == 0 {
		return xerrors.Errorf("payload size is 0")
	}

	return nil
}

type PieceInfo struct {
	PieceCIDV1 cid.Cid             `json:"piece_cid"`
	Size       abi.PaddedPieceSize `json:"size"`
	RawSize    uint64              `json:"raw_size"`
}

func (d *Deal) RawSize() (uint64, error) {
	if d.Data == nil {
		return 0, xerrors.Errorf("no data")
	}
	_, rawSize, err := commcid.PieceCidV1FromV2(d.Data.PieceCID)
	if err != nil {
		return 0, xerrors.Errorf("invalid piece cid: %w", err)
	}
	return rawSize, nil
}

func (d *Deal) Size() (abi.PaddedPieceSize, error) {
	if d.Data == nil {
		return 0, xerrors.Errorf("no data")
	}
	_, rawSize, err := commcid.PieceCidV1FromV2(d.Data.PieceCID)
	if err != nil {
		return 0, xerrors.Errorf("invalid piece cid: %w", err)
	}
	return padreader.PaddedSize(rawSize).Padded(), nil
}

func (d *Deal) PieceInfo() (*PieceInfo, error) {
	return GetPieceInfo(d.Data.PieceCID)
}

func GetPieceInfo(c cid.Cid) (*PieceInfo, error) {
	pieceCid, rawSize, err := commcid.PieceCidV1FromV2(c)
	if err != nil {
		return nil, xerrors.Errorf("invalid piece cid: %w", err)
	}
	return &PieceInfo{
		PieceCIDV1: pieceCid,
		Size:       padreader.PaddedSize(rawSize).Padded(),
		RawSize:    rawSize,
	}, nil
}

func (d *Products) Validate(db *harmonydb.DB, cfg *config.MK20Config) (DealCode, error) {
	if d == nil {
		return ErrBadProposal, xerrors.Errorf("products must be defined")
	}
	var nproducts int
	if d.DDOV1 != nil {
		nproducts++
		code, err := d.DDOV1.Validate(db, cfg)
		if err != nil {
			return code, err
		}
		if d.RetrievalV1 == nil {
			return ErrProductValidationFailed, xerrors.Errorf("retrieval v1 is required for ddo v1")
		}
		if d.RetrievalV1.AnnouncePiece {
			return ErrProductValidationFailed, xerrors.Errorf("announce piece is not supported for ddo v1")
		}
	}
	if d.RetrievalV1 != nil {
		code, err := d.RetrievalV1.Validate(db, cfg)
		if err != nil {
			return code, err
		}
	}
	if d.PDPV1 != nil {
		nproducts++
		code, err := d.PDPV1.Validate(db, cfg)
		if err != nil {
			return code, err
		}
	}

	if nproducts == 0 {
		return ErrProductValidationFailed, xerrors.Errorf("no products defined")
	}

	if d.DDOV1 != nil && d.PDPV1 != nil {
		return ErrProductValidationFailed, xerrors.Errorf("ddo_v1 and pdp_v1 are mutually exclusive")
	}

	return Ok, nil
}

type ProviderDealRejectionInfo struct {
	HTTPCode DealCode
	Reason   string
}

// DealStatusResponse represents the response of a deal's status, including its current state and an optional error message.
type DealStatusResponse struct {

	// State indicates the current processing state of the deal as a DealState value.
	State DealState `json:"status"`

	// ErrorMsg is an optional field containing error details associated with the deal's current state if an error occurred.
	ErrorMsg string `json:"errorMsg"`
}

// DealProductStatusResponse represents the status response for deal products with their respective deal statuses.
type DealProductStatusResponse struct {

	// DDOV1 holds the DealStatusResponse for product "ddo_v1".
	DDOV1 *DealStatusResponse `json:"ddo_v1,omitempty"`

	// PDPV1 represents the DealStatusResponse for the product pdp_v1.
	PDPV1 *DealStatusResponse `json:"pdp_v1,omitempty"`
}

// DealStatus represents the status of a deal, including the HTTP code and an optional response detailing the deal's state and error message.
type DealStatus struct {

	// Response provides details about the deal's per product status, such as its current state and any associated error messages, if available.
	Response *DealProductStatusResponse

	// HTTPCode represents the HTTP status code providing additional context about the deal status or possible errors.
	HTTPCode int
}

// DealState represents the current status of a deal in the system as a string value.
type DealState string

const (

	// DealStateAccepted represents the state where a deal has been accepted and is pending further processing in the system.
	DealStateAccepted DealState = "accepted"

	// DealStateAwaitingUpload represents the state where a deal is awaiting file upload to proceed further in the process.
	DealStateAwaitingUpload DealState = "uploading"

	// DealStateProcessing represents the state of a deal currently being processed in the pipeline.
	DealStateProcessing DealState = "processing"

	// DealStateSealing indicates that the deal is currently being sealed in the system.
	DealStateSealing DealState = "sealing"

	// DealStateIndexing represents the state where a deal is undergoing indexing in the system.
	DealStateIndexing DealState = "indexing"

	// DealStateFailed indicates that the deal has failed due to an error during processing, sealing, or indexing.
	DealStateFailed DealState = "failed"

	// DealStateComplete indicates that the deal has successfully completed all processing and is finalized in the system.
	DealStateComplete DealState = "complete"
)

// SupportedContracts represents a collection of contract addresses supported by a system or application.
type SupportedContracts struct {
	// Contracts represents a list of supported contract addresses in string format.
	Contracts []string `json:"contracts"`
}

func NewULID() (ulid.ULID, error) {
	return ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
}

func (dsh *DataSourceHTTP) Name() DataSourceName {
	return DataSourceNameHTTP
}

func (dso *DataSourceOffline) Name() DataSourceName {
	return DataSourceNameOffline
}

func (dsa *DataSourceAggregate) Name() DataSourceName {
	return DataSourceNameAggregate
}

func (dsh *DataSourceHttpPut) Name() DataSourceName {
	return DataSourceNamePut
}

func IsDataSourceEnabled(db *harmonydb.DB, name DataSourceName) (DealCode, error) {
	var enabled bool

	err := db.QueryRow(context.Background(), `SELECT enabled FROM market_mk20_data_source WHERE name = $1`, name).Scan(&enabled)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return http.StatusInternalServerError, xerrors.Errorf("data source %s is not enabled", name)
		}
	}
	if !enabled {
		return ErrUnsupportedDataSource, xerrors.Errorf("data source %s is not enabled", name)
	}
	return Ok, nil
}

func IsProductEnabled(db *harmonydb.DB, name ProductName) (DealCode, error) {
	var enabled bool

	err := db.QueryRow(context.Background(), `SELECT enabled FROM market_mk20_products WHERE name = $1`, name).Scan(&enabled)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return http.StatusInternalServerError, xerrors.Errorf("data source %s is not enabled", name)
		}
		return ErrUnsupportedProduct, xerrors.Errorf("product %s is not supported by the provider", name)
	}
	if !enabled {
		return ErrProductNotEnabled, xerrors.Errorf("product %s is not enabled", name)
	}
	return Ok, nil
}

// SupportedProducts represents array of products supported by the SP.
type SupportedProducts struct {
	// Contracts represents a list of supported contract addresses in string format.
	Products []string `json:"products"`
}

// SupportedDataSources represents array of dats sources supported by the SP.
type SupportedDataSources struct {
	// Contracts represents a list of supported contract addresses in string format.
	Sources []string `json:"sources"`
}

// StartUpload represents metadata for initiating an upload operation.
type StartUpload struct {

	// RawSize indicates the total size of the data to be uploaded in bytes.
	RawSize uint64 `json:"raw_size"`

	// ChunkSize defines the size of each data chunk to be used during the upload process.
	ChunkSize int64 `json:"chunk_size"`
}

// UploadStatus represents the status of a file upload process, including progress and missing chunks.
type UploadStatus struct {

	// TotalChunks represents the total number of chunks required for the upload.
	TotalChunks int `json:"total_chunks"`

	// Uploaded represents the number of chunks successfully uploaded.
	Uploaded int `json:"uploaded"`

	// Missing represents the number of chunks that are not yet uploaded.
	Missing int `json:"missing"`

	// UploadedChunks is a slice containing the indices of successfully uploaded chunks.
	UploadedChunks []int `json:"uploaded_chunks"`

	//MissingChunks is a slice containing the indices of missing chunks.
	MissingChunks []int `json:"missing_chunks"`
}

func UpdateDealDetails(ctx context.Context, db *harmonydb.DB, id ulid.ULID, deal *Deal, cfg *config.MK20Config, auth string) (*Deal, DealCode, []ProductName, error) {
	ddeal, err := DealFromDB(ctx, db, id)
	if err != nil {
		return nil, ErrServerInternalError, nil, xerrors.Errorf("getting deal from DB: %w", err)
	}

	// Run the following checks
	// If Data details exist, do not update them
	// If DDOV1 is defined then no update to it
	// If PDPV1 is defined then no update to it
	// If PDPv1 is defined but DDOV1 is not, then allow updating it

	if ddeal.Data == nil && deal.Data != nil {
		ddeal.Data = deal.Data
	}

	var newProducts []ProductName

	if ddeal.Products.DDOV1 == nil || deal.Products.DDOV1 != nil {
		ddeal.Products.DDOV1 = deal.Products.DDOV1
		newProducts = append(newProducts, ProductNameDDOV1)
	}

	if ddeal.Products.PDPV1 == nil || deal.Products.PDPV1 != nil {
		ddeal.Products.PDPV1 = deal.Products.PDPV1
		newProducts = append(newProducts, ProductNamePDPV1)
	}

	if ddeal.Products.RetrievalV1 == nil || deal.Products.RetrievalV1 != nil {
		ddeal.Products.RetrievalV1 = deal.Products.RetrievalV1
		newProducts = append(newProducts, ProductNameRetrievalV1)
	}

	code, err := ddeal.Validate(db, cfg, auth)
	if err != nil {
		return nil, code, nil, xerrors.Errorf("validate deal: %w", err)
	}
	return ddeal, Ok, newProducts, nil
}

func AuthenticateClient(db *harmonydb.DB, id, client string) (bool, error) {
	var allowed bool
	err := db.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM market_mk20_deal WHERE id = $1 AND client = $2)`, id, client).Scan(&allowed)
	if err != nil {
		return false, xerrors.Errorf("querying client: %w", err)
	}
	return allowed, nil
}

func clientAllowed(ctx context.Context, db *harmonydb.DB, client string, cfg *config.CurioConfig) (bool, error) {
	var allowed bool
	err := db.QueryRow(ctx, `SELECT allowed FROM market_mk20_clients WHERE client = $1`, client).Scan(&allowed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Client is not in the database
			return !cfg.Market.StorageMarketConfig.MK20.DenyUnknownClients, nil
		}
		return false, xerrors.Errorf("querying client: %w", err)
	}

	return allowed, nil
}

const Authprefix = "CurioAuth "

// Auth verifies the custom authentication header by parsing its contents and validating the signature using the provided database connection.
func Auth(header string, db *harmonydb.DB, cfg *config.CurioConfig) (bool, string, error) {
	keyType, pubKey, sig, err := parseCustomAuth(header)
	if err != nil {
		return false, "", xerrors.Errorf("parsing auth header: %w", err)
	}
	return verifySignature(db, keyType, pubKey, sig, cfg)
}

func parseCustomAuth(header string) (keyType string, pubKey, sig []byte, err error) {
	// TODO: Add contract as client auth

	if !strings.HasPrefix(header, Authprefix) {
		return "", nil, nil, errors.New("missing CustomAuth prefix")
	}

	parts := strings.SplitN(strings.TrimPrefix(header, Authprefix), ":", 3)
	if len(parts) != 3 {
		return "", nil, nil, errors.New("invalid auth format")
	}

	keyType = parts[0]
	pubKey, err = base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid pubkey base64: %w", err)
	}

	if len(pubKey) == 0 {
		return "", nil, nil, fmt.Errorf("invalid pubkey")
	}

	sig, err = base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid signature base64: %w", err)
	}

	if len(sig) == 0 {
		return "", nil, nil, fmt.Errorf("invalid signature")
	}

	return keyType, pubKey, sig, nil
}

func verifySignature(db *harmonydb.DB, keyType string, pubKey, signature []byte, cfg *config.CurioConfig) (bool, string, error) {
	now := time.Now().Truncate(time.Hour)
	minus1 := now.Add(-59 * time.Minute)
	plus1 := now.Add(59 * time.Minute)
	timeStamps := []time.Time{now, minus1, plus1}
	var msgs [][32]byte

	for _, t := range timeStamps {
		msgs = append(msgs, sha256.Sum256(bytes.Join([][]byte{pubKey, []byte(t.Format(time.RFC3339))}, []byte{})))
	}

	switch keyType {
	case "ed25519":
		if len(pubKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
			return false, "", errors.New("invalid ed25519 sizes")
		}
		keyStr, err := ED25519ToString(pubKey)
		if err != nil {
			return false, "", xerrors.Errorf("invalid ed25519 pubkey: %w", err)
		}

		allowed, err := clientAllowed(context.Background(), db, keyStr, cfg)
		if err != nil {
			return false, "", xerrors.Errorf("checking client allowed: %w", err)
		}
		if !allowed {
			return false, "", nil
		}

		for _, m := range msgs {
			ok := ed25519.Verify(pubKey, m[:], signature)
			if ok {
				return true, keyStr, nil
			}
		}
		return false, "", errors.New("invalid ed25519 signature")

	case "secp256k1", "bls", "delegated":
		return verifyFilSignature(db, pubKey, signature, msgs, cfg)
	default:
		return false, "", fmt.Errorf("unsupported key type: %s", keyType)
	}
}

func verifyFilSignature(db *harmonydb.DB, pubKey, signature []byte, msgs [][32]byte, cfg *config.CurioConfig) (bool, string, error) {
	signs := &fcrypto.Signature{}
	err := signs.UnmarshalBinary(signature)
	if err != nil {
		return false, "", xerrors.Errorf("invalid signature")
	}
	addr, err := address.NewFromBytes(pubKey)
	if err != nil {
		return false, "", xerrors.Errorf("invalid filecoin pubkey")
	}

	allowed, err := clientAllowed(context.Background(), db, addr.String(), cfg)
	if err != nil {
		return false, "", xerrors.Errorf("checking client allowed: %w", err)
	}
	if !allowed {
		return false, "", nil
	}

	for _, m := range msgs {
		err = sigs.Verify(signs, addr, m[:])
		if err == nil {
			return true, addr.String(), nil
		}
	}

	return false, "", errors.New("invalid signature")
}

func ED25519ToString(pubKey []byte) (string, error) {
	if len(pubKey) != ed25519.PublicKeySize {
		return "", errors.New("invalid ed25519 pubkey size")
	}
	return base58.FastBase58Encoding(pubKey), nil
}

func StringToED25519(addr string) ([]byte, error) {
	return base58.FastBase58Decoding(addr)
}
