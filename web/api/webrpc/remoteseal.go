package webrpc

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/xerrors"
)

// RSealPartner maps to rseal_delegated_partners (provider side).
type RSealPartner struct {
	ID                 int64     `db:"id" json:"id"`
	PartnerName        string    `db:"partner_name" json:"partner_name"`
	PartnerURL         string    `db:"partner_url" json:"partner_url"`
	PartnerToken       string    `db:"partner_token" json:"partner_token"`
	AllowanceRemaining int64     `db:"allowance_remaining" json:"allowance_remaining"`
	AllowanceTotal     int64     `db:"allowance_total" json:"allowance_total"`
	CreatedAt          time.Time `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time `db:"updated_at" json:"updated_at"`
}

// RSealProvider maps to rseal_client_providers (client side).
type RSealProvider struct {
	ID            int64     `db:"id" json:"id"`
	SpID          int64     `db:"sp_id" json:"sp_id"`
	ProviderURL   string    `db:"provider_url" json:"provider_url"`
	ProviderToken string    `db:"provider_token" json:"provider_token"`
	ProviderName  string    `db:"provider_name" json:"provider_name"`
	Enabled       bool      `db:"enabled" json:"enabled"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time `db:"updated_at" json:"updated_at"`
}

// RSealProvPipelineRow is a summary view of a provider-side pipeline row.
type RSealProvPipelineRow struct {
	SpID         int64  `db:"sp_id" json:"sp_id"`
	SectorNumber int64  `db:"sector_number" json:"sector_number"`
	PartnerName  string `db:"partner_name" json:"partner_name"`

	AfterSDR        bool   `db:"after_sdr" json:"after_sdr"`
	AfterTreeD      bool   `db:"after_tree_d" json:"after_tree_d"`
	AfterTreeC      bool   `db:"after_tree_c" json:"after_tree_c"`
	AfterTreeR      bool   `db:"after_tree_r" json:"after_tree_r"`
	AfterNotify     bool   `db:"after_notify_client" json:"after_notify_client"`
	AfterC1         bool   `db:"after_c1_supplied" json:"after_c1_supplied"`
	AfterFinalize   bool   `db:"after_finalize" json:"after_finalize"`
	AfterCleanup    bool   `db:"after_cleanup" json:"after_cleanup"`
	Failed          bool   `db:"failed" json:"failed"`
	FailedReasonMsg string `db:"failed_reason_msg" json:"failed_reason_msg"`

	CreateTime time.Time `db:"create_time" json:"create_time"`
}

// RSealClientPipelineRow is a summary view of a client-side pipeline row.
type RSealClientPipelineRow struct {
	SpID         int64  `db:"sp_id" json:"sp_id"`
	SectorNumber int64  `db:"sector_number" json:"sector_number"`
	ProviderName string `db:"provider_name" json:"provider_name"`

	AfterSDR        bool   `db:"after_sdr" json:"after_sdr"`
	AfterTreeD      bool   `db:"after_tree_d" json:"after_tree_d"`
	AfterTreeC      bool   `db:"after_tree_c" json:"after_tree_c"`
	AfterTreeR      bool   `db:"after_tree_r" json:"after_tree_r"`
	AfterFetch      bool   `db:"after_fetch" json:"after_fetch"`
	AfterCleanup    bool   `db:"after_cleanup" json:"after_cleanup"`
	Failed          bool   `db:"failed" json:"failed"`
	FailedReasonMsg string `db:"failed_reason_msg" json:"failed_reason_msg"`

	CreateTime time.Time `db:"create_time" json:"create_time"`
}

// RSealListPartners returns all remote seal partners (provider side).
func (a *WebRPC) RSealListPartners(ctx context.Context) ([]RSealPartner, error) {
	var partners []RSealPartner
	err := a.deps.DB.Select(ctx, &partners, `SELECT id, partner_name, partner_url, partner_token, allowance_remaining, allowance_total, created_at, updated_at FROM rseal_delegated_partners ORDER BY id`)
	if err != nil {
		return nil, xerrors.Errorf("listing partners: %w", err)
	}
	if partners == nil {
		partners = []RSealPartner{}
	}
	return partners, nil
}

// RSealAddPartner creates a new remote seal partner with a generated token.
func (a *WebRPC) RSealAddPartner(ctx context.Context, name string, url string, allowance int64) (*RSealPartner, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, xerrors.Errorf("generating token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	var partner RSealPartner
	err := a.deps.DB.QueryRow(ctx, `INSERT INTO rseal_delegated_partners (partner_name, partner_url, partner_token, allowance_remaining, allowance_total)
		VALUES ($1, $2, $3, $4, $4) RETURNING id, partner_name, partner_url, partner_token, allowance_remaining, allowance_total, created_at, updated_at`,
		name, url, token, allowance).Scan(
		&partner.ID, &partner.PartnerName, &partner.PartnerURL, &partner.PartnerToken,
		&partner.AllowanceRemaining, &partner.AllowanceTotal, &partner.CreatedAt, &partner.UpdatedAt)
	if err != nil {
		return nil, xerrors.Errorf("inserting partner: %w", err)
	}
	return &partner, nil
}

// RSealRemovePartner deletes a remote seal partner. Rejects if active pipeline rows exist.
func (a *WebRPC) RSealRemovePartner(ctx context.Context, id int64) error {
	var count int64
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM rseal_provider_pipeline WHERE partner_id = $1 AND after_cleanup = FALSE`, id).Scan(&count)
	if err != nil {
		return xerrors.Errorf("checking active pipelines: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("cannot remove partner: %d active pipeline rows exist", count)
	}

	_, err = a.deps.DB.Exec(ctx, `DELETE FROM rseal_delegated_partners WHERE id = $1`, id)
	if err != nil {
		return xerrors.Errorf("deleting partner: %w", err)
	}
	return nil
}

// RSealUpdatePartnerAllowance updates the allowance fields for a partner.
func (a *WebRPC) RSealUpdatePartnerAllowance(ctx context.Context, id int64, totalAllowance int64, remainingAllowance int64) error {
	_, err := a.deps.DB.Exec(ctx, `UPDATE rseal_delegated_partners SET allowance_total = $2, allowance_remaining = $3, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
		id, totalAllowance, remainingAllowance)
	if err != nil {
		return xerrors.Errorf("updating allowance: %w", err)
	}
	return nil
}

// connectStringPayload is the JSON structure encoded in the connect string.
type connectStringPayload struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// RSealGetConnectString returns a base64-encoded connect string for a partner.
func (a *WebRPC) RSealGetConnectString(ctx context.Context, id int64) (string, error) {
	var token string
	err := a.deps.DB.QueryRow(ctx, `SELECT partner_token FROM rseal_delegated_partners WHERE id = $1`, id).Scan(&token)
	if err != nil {
		return "", xerrors.Errorf("querying partner token: %w", err)
	}

	// Build the base URL from HTTP config
	baseURL := fmt.Sprintf("https://%s", a.deps.Cfg.HTTP.DomainName)

	payload := connectStringPayload{
		URL:   baseURL,
		Token: token,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return "", xerrors.Errorf("marshaling connect string: %w", err)
	}

	return base64.StdEncoding.EncodeToString(jsonBytes), nil
}

// RSealListProviders returns all remote seal providers (client side).
func (a *WebRPC) RSealListProviders(ctx context.Context) ([]RSealProvider, error) {
	var providers []RSealProvider
	err := a.deps.DB.Select(ctx, &providers, `SELECT id, sp_id, provider_url, provider_token, provider_name, enabled, created_at, updated_at FROM rseal_client_providers ORDER BY id`)
	if err != nil {
		return nil, xerrors.Errorf("listing providers: %w", err)
	}
	if providers == nil {
		providers = []RSealProvider{}
	}
	return providers, nil
}

// RSealAddProvider adds a remote seal provider from a connect string.
func (a *WebRPC) RSealAddProvider(ctx context.Context, spID int64, connectString string) (*RSealProvider, error) {
	// Decode connect string
	jsonBytes, err := base64.StdEncoding.DecodeString(connectString)
	if err != nil {
		return nil, xerrors.Errorf("decoding connect string: %w", err)
	}

	var payload connectStringPayload
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		return nil, xerrors.Errorf("parsing connect string: %w", err)
	}

	if payload.URL == "" || payload.Token == "" {
		return nil, fmt.Errorf("connect string missing url or token")
	}

	var provider RSealProvider
	err = a.deps.DB.QueryRow(ctx, `INSERT INTO rseal_client_providers (sp_id, provider_url, provider_token, provider_name)
		VALUES ($1, $2, $3, $4) RETURNING id, sp_id, provider_url, provider_token, provider_name, enabled, created_at, updated_at`,
		spID, payload.URL, payload.Token, "").Scan(
		&provider.ID, &provider.SpID, &provider.ProviderURL, &provider.ProviderToken,
		&provider.ProviderName, &provider.Enabled, &provider.CreatedAt, &provider.UpdatedAt)
	if err != nil {
		return nil, xerrors.Errorf("inserting provider: %w", err)
	}
	return &provider, nil
}

// RSealRemoveProvider deletes a remote seal provider. Rejects if active pipeline rows exist.
func (a *WebRPC) RSealRemoveProvider(ctx context.Context, id int64) error {
	var count int64
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM rseal_client_pipeline WHERE provider_id = $1 AND after_cleanup = FALSE`, id).Scan(&count)
	if err != nil {
		// Table might not exist if no sectors have been delegated yet
		if err != sql.ErrNoRows {
			return xerrors.Errorf("checking active pipelines: %w", err)
		}
	}
	if count > 0 {
		return fmt.Errorf("cannot remove provider: %d active pipeline rows exist", count)
	}

	_, err = a.deps.DB.Exec(ctx, `DELETE FROM rseal_client_providers WHERE id = $1`, id)
	if err != nil {
		return xerrors.Errorf("deleting provider: %w", err)
	}
	return nil
}

// RSealToggleProvider enables or disables a remote seal provider.
func (a *WebRPC) RSealToggleProvider(ctx context.Context, id int64, enabled bool) error {
	_, err := a.deps.DB.Exec(ctx, `UPDATE rseal_client_providers SET enabled = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, id, enabled)
	if err != nil {
		return xerrors.Errorf("toggling provider: %w", err)
	}
	return nil
}

// RSealProviderPipeline returns active provider-side pipeline rows.
func (a *WebRPC) RSealProviderPipeline(ctx context.Context) ([]RSealProvPipelineRow, error) {
	var rows []RSealProvPipelineRow
	err := a.deps.DB.Select(ctx, &rows, `SELECT p.sp_id, p.sector_number, d.partner_name,
		p.after_sdr, p.after_tree_d, p.after_tree_c, p.after_tree_r,
		p.after_notify_client, p.after_c1_supplied, p.after_finalize, p.after_cleanup,
		p.failed, p.failed_reason_msg, p.create_time
		FROM rseal_provider_pipeline p
		JOIN rseal_delegated_partners d ON p.partner_id = d.id
		ORDER BY p.create_time DESC
		LIMIT 100`)
	if err != nil {
		return nil, xerrors.Errorf("querying provider pipeline: %w", err)
	}
	if rows == nil {
		rows = []RSealProvPipelineRow{}
	}
	return rows, nil
}

// RSealClientPipeline returns active client-side pipeline rows.
func (a *WebRPC) RSealClientPipeline(ctx context.Context) ([]RSealClientPipelineRow, error) {
	var rows []RSealClientPipelineRow
	err := a.deps.DB.Select(ctx, &rows, `SELECT c.sp_id, c.sector_number, COALESCE(p.provider_name, p.provider_url) AS provider_name,
		c.after_sdr, c.after_tree_d, c.after_tree_c, c.after_tree_r,
		c.after_fetch, c.after_cleanup,
		c.failed, c.failed_reason_msg, c.create_time
		FROM rseal_client_pipeline c
		JOIN rseal_client_providers p ON c.provider_id = p.id
		ORDER BY c.create_time DESC
		LIMIT 100`)
	if err != nil {
		return nil, xerrors.Errorf("querying client pipeline: %w", err)
	}
	if rows == nil {
		rows = []RSealClientPipelineRow{}
	}
	return rows, nil
}
