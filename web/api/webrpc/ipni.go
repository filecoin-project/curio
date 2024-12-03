package webrpc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

type IpniAd struct {
	AdCid           string         `db:"ad_cid" json:"ad_cid"`
	ContextID       []byte         `db:"context_id" json:"context_id"`
	IsRM            bool           `db:"is_rm" json:"is_rm"`
	IsSkip          bool           `db:"is_skip" json:"is_skip"`
	PreviousAd      sql.NullString `db:"previous"`
	Previous        string         `json:"previous"`
	SpID            int64          `db:"sp_id" json:"sp_id"`
	Addresses       sql.NullString `db:"addresses"`
	AddressesString string         `json:"addresses"`
	Entries         string         `db:"entries" json:"entries"`
	PieceCid        string         `json:"piece_cid"`
	PieceSize       int64          `json:"piece_size"`
	Miner           string         `json:"miner"`

	EntryCount int64 `json:"entry_count"`
	CIDCount   int64 `json:"cid_count"`

	AdCids []string `db:"-" json:"ad_cids"`
}

func (a *WebRPC) GetAd(ctx context.Context, ad string) (*IpniAd, error) {
	adCid, err := cid.Parse(ad)
	if err != nil {
		return nil, xerrors.Errorf("failed to parse the ad cid: %w", err)
	}

	var ads []IpniAd

	err = a.deps.DB.Select(ctx, &ads, `SELECT 
									ip.ad_cid, 
									ip.context_id, 
									ip.is_rm,
									ip.is_skip,
									ip.previous,
									ipp.sp_id,
									ip.addresses,
									ip.entries
									FROM ipni ip
									LEFT JOIN ipni_peerid ipp ON ip.provider = ipp.peer_id
									WHERE ip.ad_cid = $1`, adCid.String())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the ad details from DB: %w", err)
	}

	if len(ads) == 0 {
		// try to get as entry

		err = a.deps.DB.Select(ctx, &ads, `SELECT
											ip.ad_cid,
											ip.context_id,
											ip.is_rm,
											ip.is_skip,
											ip.previous,
											ipp.sp_id,
											ip.addresses,
											ip.entries
										FROM ipni_chunks ipc
											LEFT JOIN ipni ip ON ip.piece_cid = ipc.piece_cid
											LEFT JOIN ipni_peerid ipp ON ip.provider = ipp.peer_id
										WHERE ipc.cid = $1`, adCid.String())
		if err != nil {
			return nil, fmt.Errorf("failed to fetch the ad details from DB: %w", err)
		}

		if len(ads) == 0 {
			return nil, xerrors.Errorf("no ad found for ad cid: %s", adCid)
		}
	}

	details := ads[0]

	var pi abi.PieceInfo
	err = pi.UnmarshalCBOR(bytes.NewReader(details.ContextID))
	if err != nil {
		return nil, xerrors.Errorf("failed to unmarshal piece info: %w", err)
	}

	details.PieceCid = pi.PieceCID.String()
	size := int64(pi.Size)
	details.PieceSize = size

	maddr, err := address.NewIDAddress(uint64(details.SpID))
	if err != nil {
		return nil, err
	}
	details.Miner = maddr.String()

	if !details.PreviousAd.Valid {
		details.Previous = ""
	} else {
		details.Previous = details.PreviousAd.String
	}

	if !details.Addresses.Valid {
		details.AddressesString = ""
	} else {
		details.AddressesString = details.Addresses.String
	}

	var adEntryInfo []struct {
		EntryCount int64 `db:"entry_count"`
		CIDCount   int64 `db:"cid_count"`
	}

	err = a.deps.DB.Select(ctx, &adEntryInfo, `SELECT count(1) as entry_count, sum(num_blocks) as cid_count from ipni_chunks where piece_cid=$1`, details.PieceCid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch the ad entry count from DB: %w", err)
	}

	if adEntryInfo[0].EntryCount > 0 {
		details.EntryCount = adEntryInfo[0].EntryCount
		details.CIDCount = adEntryInfo[0].CIDCount
	}

	for _, ipniAd := range ads {
		details.AdCids = append(details.AdCids, ipniAd.AdCid)
	}

	return &details, nil
}

type IPNI struct {
	SpId       int64            `db:"sp_id" json:"sp_id"`
	PeerID     string           `db:"peer_id" json:"peer_id"`
	Head       string           `db:"head" json:"head"`
	Miner      string           `json:"miner"`
	SyncStatus []IpniSyncStatus `json:"sync_status"`
}

type IpniSyncStatus struct {
	Service               string    `json:"service"`
	RemoteAd              string    `json:"remote_ad"`
	PublisherAddress      string    `json:"publisher_address"`
	Address               string    `json:"address"`
	LastAdvertisementTime time.Time `json:"last_advertisement_time"`
	Error                 string    `json:"error"`
}

type AddrInfo struct {
	ID    string   `json:"ID"`
	Addrs []string `json:"Addrs"`
}

type Advertisement struct {
	Slash string `json:"/"`
}

type ParsedResponse struct {
	AddrInfo              AddrInfo       `json:"AddrInfo"`
	LastAdvertisement     Advertisement  `json:"LastAdvertisement"`
	LastAdvertisementTime time.Time      `json:"LastAdvertisementTime"`
	Publisher             AddrInfo       `json:"Publisher"`
	ExtendedProviders     map[string]any `json:"ExtendedProviders"`
	FrozenAt              string         `json:"FrozenAt"`
	LastError             string         `json:"LastError"`
}

func (a *WebRPC) IPNISummary(ctx context.Context) ([]*IPNI, error) {
	var summary []*IPNI

	err := a.deps.DB.Select(ctx, &summary, `SELECT 
												ipp.sp_id,
												ipp.peer_id,
												ih.head
												FROM ipni_peerid ipp
												LEFT JOIN ipni_head ih ON ipp.peer_id = ih.provider`)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the provider details from DB: %w", err)
	}

	for i := range summary {
		maddr, err := address.NewIDAddress(uint64(summary[i].SpId))
		if err != nil {
			return nil, fmt.Errorf("failed to convert ID address: %w", err)
		}
		summary[i].Miner = maddr.String()
	}

	type minimalIpniInfo struct {
		Market struct {
			StorageMarketConfig struct {
				IPNI struct {
					ServiceURL []string
				}
			}
		}
	}

	var services []string

	err = forEachConfig[minimalIpniInfo](a, func(name string, info minimalIpniInfo) error {
		services = append(services, info.Market.StorageMarketConfig.IPNI.ServiceURL...)
		return nil
	})

	if len(services) == 0 {
		services = append(services, "https://cid.contact")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to fetch IPNI configuration: %w", err)
	}

	for _, service := range services {
		for _, d := range summary {
			url := service + "/providers/" + d.PeerID
			resp, err := http.Get(url)
			if err != nil {
				return nil, xerrors.Errorf("Error fetching data from IPNI service: %w", err)
			}
			defer func(Body io.ReadCloser) {
				_ = Body.Close()
			}(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to fetch data from IPNI service: %s", resp.Status)
			}
			out, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}
			var parsed ParsedResponse
			err = json.Unmarshal(out, &parsed)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal IPNI service response: %w", err)
			}
			sync := IpniSyncStatus{
				Service:               service,
				Error:                 parsed.LastError,
				LastAdvertisementTime: parsed.LastAdvertisementTime,
				RemoteAd:              parsed.LastAdvertisement.Slash,
				Address:               strings.Join(parsed.AddrInfo.Addrs, ","),
				PublisherAddress:      strings.Join(parsed.Publisher.Addrs, ","),
			}
			if parsed.LastAdvertisement.Slash != d.Head {
				var diff int64
				err := a.deps.DB.QueryRow(ctx, `WITH cte AS (
															SELECT ad_cid, order_number
															FROM ipni
															WHERE provider = $1
															AND ad_cid IN ($2, $3)
															)
															SELECT COUNT(*)
															FROM ipni
															WHERE provider = $1
															AND order_number BETWEEN (SELECT MIN(order_number) FROM cte) 
																				  AND (SELECT MAX(order_number) FROM cte) - 1;`,
					d.PeerID, d.Head, parsed.LastAdvertisement.Slash).Scan(&diff)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch the being count: %w", err)
				}
				sync.RemoteAd = sync.RemoteAd + fmt.Sprintf(" (%d behind)", diff)
			}
			d.SyncStatus = append(d.SyncStatus, sync)
		}
	}
	return summary, nil
}

type EntryInfo struct {
	PieceCID string `db:"piece_cid"`
	FromCar  bool   `db:"from_car"`

	FirstCID    *string `db:"first_cid"`
	StartOffset *int64  `db:"start_offset"`
	NumBlocks   int64   `db:"num_blocks"`

	PrevCID *string `db:"prev_cid"`

	Err  *string
	Size int64
}

func (a *WebRPC) IPNIEntry(ctx context.Context, block cid.Cid) (*EntryInfo, error) {
	var ipniChunks []EntryInfo

	err := a.deps.DB.Select(ctx, &ipniChunks, `SELECT 
			current.piece_cid, 
			current.from_car, 
			current.first_cid, 
			current.start_offset, 
			current.num_blocks, 
			prev.cid AS prev_cid
		FROM 
			ipni_chunks current
		LEFT JOIN 
			ipni_chunks prev 
		ON 
			current.piece_cid = prev.piece_cid AND
			current.chunk_num = prev.chunk_num + 1
		WHERE 
			current.cid = $1
		LIMIT 1;`, block.String())
	if err != nil {
		return nil, xerrors.Errorf("querying chunks with entry link %s: %w", block, err)
	}

	if len(ipniChunks) == 0 {
		return nil, xerrors.Errorf("no entry found for %s", block)
	}

	entry := ipniChunks[0]

	b, err := a.deps.ServeChunker.GetEntry(ctx, block)
	if err != nil {
		estr := err.Error()
		entry.Err = &estr
	} else {
		entry.Size = int64(len(b))
	}

	return &entry, nil
}

func (a *WebRPC) IPNISetSkip(ctx context.Context, adCid cid.Cid, skip bool) error {
	n, err := a.deps.DB.Exec(ctx, `UPDATE ipni SET is_skip = $1 WHERE ad_cid = $2`, skip, adCid.String())
	if err != nil {
		return xerrors.Errorf("updating ipni set: %w", err)
	}

	if n == 0 {
		return xerrors.Errorf("ipni set is zero")
	}

	return nil
}
