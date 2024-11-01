package webrpc

import (
	"context"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v15/market"
)

type PriceFilter struct {
	Number int64 `db:"number" json:"number"`

	MinDur int `db:"min_duration_days" json:"min_dur"`
	MaxDur int `db:"max_duration_days" json:"max_dur"`

	MinSize int64 `db:"min_size" json:"min_size"`
	MaxSize int64 `db:"max_size" json:"max_size"`

	Price    int64 `db:"price" json:"price"`
	Verified bool  `db:"verified" json:"verified"`
}

type ClientFilter struct {
	Name   string `db:"name" json:"name"`
	Active bool   `db:"active" json:"active"`

	Wallets []string `db:"wallets" json:"wallets"`
	Peers   []string `db:"peer_ids" json:"peers"`

	PricingFilters []int64 `db:"pricing_filters" json:"pricing_filters"`

	MaxDealsPerHour    int64 `db:"max_deals_per_hour" json:"max_deals_per_hour"`
	MaxDealSizePerHour int64 `db:"max_deal_size_per_hour" json:"max_deal_size_per_hour"`

	Info string `db:"additional_info" json:"info"`
}

type AllowDeny struct {
	Wallet string `db:"wallet" json:"wallet"`
	Status bool   `db:"status" json:"status"`
}

func (a *WebRPC) GetClientFilters(ctx context.Context) ([]ClientFilter, error) {
	var filters []ClientFilter
	err := a.deps.DB.Select(ctx, &filters, `SELECT 
											name, 
											active, 
											wallets, 
											peer_ids, 
											pricing_filters, 
											max_deals_per_hour, 
											max_deal_size_per_hour, 
											additional_info 
										FROM market_mk12_client_filters ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return filters, nil
}

func (a *WebRPC) GetPriceFilters(ctx context.Context) ([]PriceFilter, error) {
	var filters []PriceFilter
	err := a.deps.DB.Select(ctx, &filters, `SELECT 
											number, 
											min_duration_days, 
											max_duration_days, 
											min_size, 
											max_size, 
											price, 
											verified
										FROM market_mk12_pricing_filters ORDER BY number`)
	if err != nil {
		return nil, err
	}
	return filters, nil
}

func (a *WebRPC) GetAllowDenyList(ctx context.Context) ([]AllowDeny, error) {
	var adList []AllowDeny
	err := a.deps.DB.Select(ctx, &adList, `SELECT 
											wallet, 
											status
										FROM market_allow_list ORDER BY wallet`)
	if err != nil {
		return nil, err
	}
	return adList, nil
}

func (a *WebRPC) SetClientFilters(ctx context.Context, name string, active bool, wallets, peers []string, filters []int64, maxDealPerHour, maxDealSizePerHour int64) error {
	for i := range wallets {
		if wallets[i] == "" {
			return xerrors.Errorf("wallet address cannot be empty")
		}
		_, err := address.NewFromString(wallets[i])
		if err != nil {
			return xerrors.Errorf("invalid wallet address: %w", err)
		}
	}

	for i := range peers {
		if peers[i] == "" {
			return xerrors.Errorf("peer ID cannot be empty")
		}
		_, err := peer.Decode(peers[i])
		if err != nil {
			return xerrors.Errorf("invalid peer ID: %w", err)
		}
	}

	if maxDealPerHour < 0 {
		return xerrors.Errorf("maxDealPerHour cannot be negative")
	}

	if maxDealSizePerHour < 0 {
		return xerrors.Errorf("maxDealSizePerHour cannot be negative")
	}

	if len(filters) == 0 {
		return xerrors.Errorf("pricing filters cannot be empty")
	}

	if len(wallets) == 0 && len(peers) == 0 {
		return xerrors.Errorf("either wallets or peers must be provided")
	}

	var all int
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk12_pricing_filters WHERE number = ANY($1)`, filters).Scan(&all)
	if err != nil {
		return xerrors.Errorf("failed to check existing pricing filters: %w", err)
	}
	if all != len(filters) {
		return xerrors.Errorf("not all pricing filters exist")
	}
	n, err := a.deps.DB.Exec(ctx, `UPDATE market_mk12_client_filters SET active = $2, wallets = $3, peer_ids = $4, pricing_filters = $5, 
                                      max_deals_per_hour = $6, max_deal_size_per_hour = $7 WHERE name = $1`, name,
		active, wallets, peers, filters, maxDealPerHour, maxDealSizePerHour)
	if err != nil {
		return xerrors.Errorf("updating client filter: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("unexpected number of rows affected: expected 1 and got %d", n)
	}
	return nil
}

func (a *WebRPC) SetPriceFilters(ctx context.Context, number int64, minDur, maxDur int, minSize, maxSize int64, price int64, verified bool) error {
	if abi.ChainEpoch(minDur*builtin.EpochsInDay) < market.DealMinDuration {
		return xerrors.Errorf("minimum duration must be greater than or equal to %d days", market.DealMinDuration/builtin.EpochsInDay)
	}

	if minDur > maxDur {
		return xerrors.Errorf("minimum duration cannot be greater than maximum duration")
	}

	if abi.ChainEpoch(maxDur*builtin.EpochsInDay) > market.DealMaxDuration {
		return xerrors.Errorf("maximum duration must be less than or equal to %d days", market.DealMaxDuration/builtin.EpochsInDay)
	}

	if price < 0 {
		return xerrors.Errorf("price cannot be negative")
	}

	if minSize > maxSize {
		return xerrors.Errorf("minimum size cannot be greater than maximum size")
	}

	if minSize < 0 || maxSize < 0 {
		return xerrors.Errorf("minimum size and maximum size cannot be negative")
	}

	n, err := a.deps.DB.Exec(ctx, `UPDATE market_mk12_pricing_filters SET min_duration_days = $2, max_duration_days = $3, 
                                       min_size = $4, max_size = $5, price= $6, verified = $7 WHERE number = $1`,
		number, minDur, maxDur, minSize, maxSize, price, verified)
	if err != nil {
		return xerrors.Errorf("updating price filter: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("unexpected number of rows affected: expected 1 and got %d", n)
	}
	return nil
}

func (a *WebRPC) SetAllowDenyList(ctx context.Context, wallet string, status bool) error {
	_, err := address.NewFromString(wallet)
	if err != nil {
		return xerrors.Errorf("invalid wallet address: %w", err)
	}

	n, err := a.deps.DB.Exec(ctx, `UPDATE market_allow_list SET status = $2 WHERE wallet = $1`, wallet, status)
	if err != nil {
		return xerrors.Errorf("updating allow deny list: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("unexpected number of rows affected: expected 1 and got %d", n)
	}
	return nil
}

func (a *WebRPC) AddClientFilters(ctx context.Context, name string, active bool, wallets, peers []string, filters []int64, maxDealPerHour, maxDealSizePerHour int64) error {
	for i := range wallets {
		if wallets[i] == "" {
			return xerrors.Errorf("wallet address cannot be empty")
		}
		_, err := address.NewFromString(wallets[i])
		if err != nil {
			return xerrors.Errorf("invalid wallet address: %w", err)
		}
	}

	for i := range peers {
		if peers[i] == "" {
			return xerrors.Errorf("peer ID cannot be empty")
		}
		_, err := peer.Decode(peers[i])
		if err != nil {
			return xerrors.Errorf("invalid peer ID: %w", err)
		}
	}

	if maxDealPerHour < 0 {
		return xerrors.Errorf("maxDealPerHour cannot be negative")
	}

	if maxDealSizePerHour < 0 {
		return xerrors.Errorf("maxDealSizePerHour cannot be negative")
	}

	if len(filters) == 0 {
		return xerrors.Errorf("pricing filters cannot be empty")
	}

	if len(wallets) == 0 && len(peers) == 0 {
		return xerrors.Errorf("either wallets or peers must be provided")
	}

	var all int
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk12_pricing_filters WHERE number = ANY($1)`, filters).Scan(&all)
	if err != nil {
		return xerrors.Errorf("failed to check existing pricing filters: %w", err)
	}
	if all != len(filters) {
		return xerrors.Errorf("not all pricing filters exist")
	}

	n, err := a.deps.DB.Exec(ctx, `INSERT INTO market_mk12_client_filters (name, active, wallets, peer_ids, pricing_filters, max_deals_per_hour, max_deal_size_per_hour) 
										VALUES ($1, $2, $3, $4, $5, $6, $7)`, name, active, wallets, peers, filters, maxDealPerHour, maxDealSizePerHour)
	if err != nil {
		return xerrors.Errorf("failed to add client filters: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("unexpected number of rows affected: expected 1 and got %d", n)
	}
	return nil
}

func (a *WebRPC) AddPriceFilters(ctx context.Context, minDur, maxDur int, minSize, maxSize int64, price int64, verified bool) error {
	if abi.ChainEpoch(minDur*builtin.EpochsInDay) < market.DealMinDuration {
		return xerrors.Errorf("minimum duration must be greater than or equal to %d days", market.DealMinDuration/builtin.EpochsInDay)
	}

	if minDur > maxDur {
		return xerrors.Errorf("minimum duration cannot be greater than maximum duration")
	}

	if abi.ChainEpoch(maxDur*builtin.EpochsInDay) > market.DealMaxDuration {
		return xerrors.Errorf("maximum duration must be less than or equal to %d days", market.DealMaxDuration/builtin.EpochsInDay)
	}

	if price < 0 {
		return xerrors.Errorf("price cannot be negative")
	}

	if minSize > maxSize {
		return xerrors.Errorf("minimum size cannot be greater than maximum size")
	}

	if minSize < 0 || maxSize < 0 {
		return xerrors.Errorf("minimum size and maximum size cannot be negative")
	}

	n, err := a.deps.DB.Exec(ctx, `INSERT INTO market_mk12_pricing_filters (min_duration_days, max_duration_days, min_size, max_size, price, verified) VALUES ($1, $2, $3, $4, $5, $6)`, minDur, maxDur, minSize, maxSize, price, verified)
	if err != nil {
		return xerrors.Errorf("failed to add price filters: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("unexpected number of rows affected: expected 1 and got %d", n)
	}
	return nil
}

func (a *WebRPC) AddAllowDenyList(ctx context.Context, wallet string, status bool) error {
	_, err := address.NewFromString(wallet)
	if err != nil {
		return xerrors.Errorf("invalid wallet address: %w", err)
	}

	n, err := a.deps.DB.Exec(ctx, "INSERT INTO market_allow_list (wallet, status) VALUES ($1, $2)", wallet, status)
	if err != nil {
		return err
	}
	if n != 1 {
		return xerrors.Errorf("unexpected number of rows affected: expected 1 and got %d", n)
	}
	return nil
}

func (a *WebRPC) RemovePricingFilter(ctx context.Context, number int64) error {
	_, err := a.deps.DB.Exec(ctx, `SELECT remove_pricing_filter($1)`, number)
	if err != nil {
		return err
	}
	return nil
}
func (a *WebRPC) RemoveClientFilter(ctx context.Context, name string) error {
	_, err := a.deps.DB.Exec(ctx, `DELETE FROM market_mk12_client_filters WHERE name = $1`, name)
	if err != nil {
		return err
	}
	return nil
}
func (a *WebRPC) RemoveAllowFilter(ctx context.Context, wallet string) error {
	_, err := a.deps.DB.Exec(ctx, `DELETE FROM market_allow_list WHERE wallet = $1`, wallet)
	if err != nil {
		return err
	}
	return nil
}

func (a *WebRPC) DefaultAllowBehaviour(ctx context.Context) *bool {
	ret := a.deps.Cfg.Market.StorageMarketConfig.MK12.DenyUnknownClients
	return &ret
}
