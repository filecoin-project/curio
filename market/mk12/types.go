package mk12

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/maurl"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"
	"github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/go-state-types/crypto"
)

const Libp2pScheme = "libp2p"

//go:generate cbor-gen-for --map-encoding DealParamsV120 DealParams DirectDealParams Transfer DealResponse DealStatusRequest DealStatusResponse DealStatus

// DealStatusRequest is sent to get the current state of a deal from a
// storage provider
type DealStatusRequest struct {
	DealUUID  uuid.UUID
	Signature crypto.Signature
}

// DealStatusResponse is the current state of a deal
type DealStatusResponse struct {
	DealUUID uuid.UUID
	// Error is non-empty if there is an error getting the deal status
	// (eg invalid request signature)
	Error          string
	DealStatus     *DealStatus
	IsOffline      bool
	TransferSize   uint64
	NBytesReceived uint64
}

type DealStatus struct {
	// Error is non-empty if the deal is in the error state
	Error string
	// Status is a string corresponding to a deal checkpoint
	Status string
	// SealingStatus is the sealing status reported by lotus miner
	SealingStatus string
	// Proposal is the deal proposal
	Proposal market.DealProposal
	// SignedProposalCid is the cid of the client deal proposal + signature
	SignedProposalCid cid.Cid
	// PublishCid is the cid of the Publish message sent on chain, if the deal
	// has reached the publish stage
	PublishCid *cid.Cid
	// ChainDealID is the id of the deal in chain state
	ChainDealID abi.DealID
}

type DealParamsV120 struct {
	DealUUID           uuid.UUID
	IsOffline          bool
	ClientDealProposal market.ClientDealProposal
	DealDataRoot       cid.Cid
	Transfer           Transfer
}

type DealParams struct {
	DealUUID           uuid.UUID
	IsOffline          bool
	ClientDealProposal market.ClientDealProposal
	DealDataRoot       cid.Cid
	Transfer           Transfer // Transfer params will be the zero value if this is an offline deal
	RemoveUnsealedCopy bool
	SkipIPNIAnnounce   bool
}

type DirectDealParams struct {
	DealUUID           uuid.UUID
	AllocationID       verifreg.AllocationId
	PieceCid           cid.Cid
	ClientAddr         address.Address
	StartEpoch         abi.ChainEpoch
	EndEpoch           abi.ChainEpoch
	FilePath           string
	DeleteAfterImport  bool
	RemoveUnsealedCopy bool
	SkipIPNIAnnounce   bool
}

// Transfer has the parameters for a data transfer
type Transfer struct {
	// The type of transfer eg "http"
	Type string
	// An optional ID that can be supplied by the client to identify the deal
	ClientID string
	// A byte array containing marshalled data specific to the transfer type
	// eg a JSON encoded struct { URL: "<url>", Headers: {...} }
	Params []byte
	// The size of the data transferred in bytes
	Size uint64
}

type TransportUrl struct {
	Scheme    string
	Url       string
	PeerID    peer.ID
	Multiaddr multiaddr.Multiaddr
}

// HttpRequest has parameters for an HTTP transfer
type HttpRequest struct {
	// URL can be
	// - an http URL:
	//   "https://example.com/path"
	// - a libp2p URL:
	//   "libp2p:///ip4/104.131.131.82/tcp/4001/ipfs/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ"
	//   Must include a Peer ID
	URL string
	// Headers are the HTTP headers that are sent as part of the request,
	// eg "Authorization"
	Headers map[string]string
}

func ParseUrl(urlStr string) (*TransportUrl, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parsing url '%s': %w", urlStr, err)
	}
	if u.Scheme == "" {
		return nil, fmt.Errorf("parsing url '%s': could not parse scheme", urlStr)
	}
	if u.Scheme == Libp2pScheme {
		return parseLibp2pUrl(urlStr)
	}
	return &TransportUrl{Scheme: u.Scheme, Url: urlStr}, nil
}

func parseLibp2pUrl(urlStr string) (*TransportUrl, error) {
	// Remove libp2p prefix
	prefix := Libp2pScheme + "://"
	if !strings.HasPrefix(urlStr, prefix) {
		return nil, fmt.Errorf("libp2p URL '%s' must start with prefix '%s'", urlStr, prefix)
	}

	// Convert to AddrInfo
	addrInfo, err := peer.AddrInfoFromString(urlStr[len(prefix):])
	if err != nil {
		return nil, fmt.Errorf("parsing address info from url '%s': %w", urlStr, err)
	}

	// There should be exactly one address
	if len(addrInfo.Addrs) != 1 {
		return nil, fmt.Errorf("expected only one address in url '%s'", urlStr)
	}

	return &TransportUrl{
		Scheme:    Libp2pScheme,
		Url:       Libp2pScheme + "://" + addrInfo.ID.String(),
		PeerID:    addrInfo.ID,
		Multiaddr: addrInfo.Addrs[0],
	}, nil
}

func (t *Transfer) Host() (string, error) {
	if t.Type != "http" && t.Type != "libp2p" {
		return "", fmt.Errorf("cannot parse params for unrecognized transfer type '%s'", t.Type)
	}

	// de-serialize transport opaque token
	tInfo := &HttpRequest{}
	if err := json.Unmarshal(t.Params, tInfo); err != nil {
		return "", fmt.Errorf("failed to de-serialize transport params bytes '%s': %w", string(t.Params), err)
	}

	// Parse http / multiaddr url
	u, err := ParseUrl(tInfo.URL)
	if err != nil {
		return "", fmt.Errorf("cannot parse url '%s': %w", tInfo.URL, err)
	}

	// If the url is in libp2p format
	if u.Scheme == Libp2pScheme {
		// Get the host from the multiaddr
		mahttp, err := maurl.ToURL(u.Multiaddr)
		if err != nil {
			return "", err
		}
		return mahttp.Host, nil
	}

	// Otherwise parse as an http url
	httpUrl, err := url.Parse(u.Url)
	if err != nil {
		return "", fmt.Errorf("cannot parse url '%s' from '%s': %w", u.Url, tInfo.URL, err)
	}

	return httpUrl.Host, nil
}

type DealResponse struct {
	Accepted bool
	// Message is the reason the deal proposal was rejected. It is empty if
	// the deal was accepted.
	Message string
}

type DealPublisher interface {
	Publish(ctx context.Context, deal market.ClientDealProposal) (cid.Cid, error)
}

// PublishDealsWaitResult is the result of a call to wait for publish deals to
// appear on chain
type PublishDealsWaitResult struct {
	DealID   abi.DealID
	FinalCid cid.Cid
}

// ProviderDealRejectionInfo is the information sent by the Storage Provider
// to the Client when it accepts or rejects a deal.
type ProviderDealRejectionInfo struct {
	Accepted bool
	Reason   string // The rejection reason, if the deal is rejected
}

// ProviderDealState is the local state tracked for a deal by the StorageProvider.
type ProviderDealState struct {
	// DealUuid is an unique uuid generated by client for the deal.
	DealUuid uuid.UUID
	// CreatedAt is the time at which the deal was stored
	CreatedAt time.Time
	// SignedProposalCID is cid for proposal and client signature
	SignedProposalCID cid.Cid
	// ClientDealProposal is the deal proposal sent by the client.
	ClientDealProposal market.ClientDealProposal
	// IsOffline is true for offline deals i.e. deals where the actual data to be stored by the SP is sent out of band
	// and not via an online data transfer.
	IsOffline bool
	// CleanupData indicates whether to remove the data for a deal after the deal has been added to a sector.
	// This is always true for online deals, and can be set as a flag for offline deals.
	CleanupData bool

	// ClientPeerID is the Clients libp2p Peer ID.
	ClientPeerID peer.ID

	// DealDataRoot is the root of the IPLD DAG that the client wants to store.
	DealDataRoot cid.Cid

	// InboundCARPath is the file-path where the storage provider will persist the CAR file sent by the client.
	InboundFilePath string

	// Transfer has the parameters for the data transfer
	Transfer Transfer

	// Chain Vars
	ChainDealID abi.DealID
	PublishCID  *cid.Cid

	// sector packing info
	SectorID abi.SectorNumber
	Offset   abi.PaddedPieceSize

	Length abi.PaddedPieceSize

	// set if there's an error
	Err string

	// Keep unsealed copy of the data
	FastRetrieval bool

	//Announce deal to the IPNI(Index Provider)
	AnnounceToIPNI bool
}

func (d *ProviderDealState) String() string {
	return fmt.Sprintf("%+v", *d)
}

func (d *ProviderDealState) GetSignedProposalCid() (cid.Cid, error) {
	propnd, err := cborutil.AsIpld(&d.ClientDealProposal)
	if err != nil {
		return cid.Undef, fmt.Errorf("failed to compute signed deal proposal ipld node: %w", err)
	}

	return propnd.Cid(), nil
}
