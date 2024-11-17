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
	PreviousAd      sql.NullString `db:"previous"`
	Previous        string         `json:"previous"`
	SpID            int64          `db:"sp_id" json:"sp_id"`
	Addresses       sql.NullString `db:"addresses"`
	AddressesString string         `json:"addresses"`
	Entries         string         `db:"entries" json:"entries"`
	PieceCid        string         `json:"piece_cid"`
	PieceSize       int64          `json:"piece_size"`
	Miner           string         `json:"miner"`
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
		return nil, nil
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

func (a *WebRPC) IPNISummary(ctx context.Context) ([]IPNI, error) {
	var summary []IPNI

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
		IPNIConfig struct {
			ServiceURL []string
		}
	}

	var services []string

	err = forEachConfig[minimalIpniInfo](a, func(name string, info minimalIpniInfo) error {
		if len(info.IPNIConfig.ServiceURL) == 0 {
			return nil
		}

		services = append(services, info.IPNIConfig.ServiceURL...)
		return nil
	})

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
				sync.RemoteAd = sync.RemoteAd + fmt.Sprintf(" (%d beind)", diff)
			}
			d.SyncStatus = append(d.SyncStatus, sync)
		}
	}
	return summary, nil
}
