package pdp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"time"

	logger "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var logIdempotency = logger.Logger("pdp/idempotency")

// Ensure harmonydb import is used
var _ = (*harmonydb.DB)(nil)

// Pre-compiled regex for idempotency key validation
var idempotencyKeyRegex = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// IdempotencyKey represents a validated idempotency key
type IdempotencyKey string

// UnmarshalJSON implements custom JSON unmarshaling with validation
func (ik *IdempotencyKey) UnmarshalJSON(data []byte) error {
	// Handle null case
	if string(data) == "null" {
		*ik = ""
		return nil
	}

	// Unmarshal raw string
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	// Empty string is allowed (optional field)
	if str == "" {
		*ik = ""
		return nil
	}

	// Validate length
	if len(str) > 255 {
		return errors.New("idempotency key must be 255 characters or less")
	}

	// Validate format using pre-compiled regex
	if !idempotencyKeyRegex.MatchString(str) {
		return errors.New("idempotency key can only contain letters, numbers, hyphens, and underscores")
	}

	*ik = IdempotencyKey(str)
	return nil
}

// IdempotencyResult represents the result of an idempotency check
type IdempotencyResult struct {
	Exists     bool
	TxHash     *string
	IsReserved bool // true if key exists but tx_hash is NULL
}

// checkOrReserveIdempotencyKey atomically checks for existing key or reserves it
func (p *PDPService) checkOrReserveIdempotencyKey(ctx context.Context, idempotencyKey IdempotencyKey) (IdempotencyResult, error) {
	if idempotencyKey == "" {
		return IdempotencyResult{Exists: false}, nil
	}

	var txHash sql.NullString
	err := p.db.QueryRow(ctx, `
        INSERT INTO pdp_idempotency (idempotency_key, tx_hash) 
        VALUES ($1, NULL) 
        ON CONFLICT (idempotency_key) 
        DO UPDATE SET tx_hash = EXCLUDED.tx_hash
        RETURNING tx_hash
    `, string(idempotencyKey)).Scan(&txHash)

	if err != nil {
		return IdempotencyResult{}, fmt.Errorf("failed to check/reserve idempotency key: %w", err)
	}

	result := IdempotencyResult{
		Exists:     true, // Key exists (either just inserted or already existed)
		TxHash:     nil,
		IsReserved: true, // tx_hash is NULL means we just reserved it
	}

	if txHash.Valid {
		txHashStr := txHash.String
		result.TxHash = &txHashStr
		result.IsReserved = false // Key exists with tx_hash, operation completed
	}

	return result, nil
}

// updateIdempotencyKey updates a reserved key with actual transaction hash
func (p *PDPService) updateIdempotencyKey(tx *harmonydb.Tx, idempotencyKey IdempotencyKey, txHash string) error {
	if idempotencyKey == "" {
		return nil
	}

	_, err := tx.Exec(`
        UPDATE pdp_idempotency 
        SET tx_hash = $1 
        WHERE idempotency_key = $2 AND tx_hash IS NULL
    `, txHash, string(idempotencyKey))
	if err != nil {
		return fmt.Errorf("failed to update idempotency key: %w", err)
	}

	return nil
}

// cleanupReservedIdempotencyKey removes a reserved key on operation failure
func (p *PDPService) cleanupReservedIdempotencyKey(ctx context.Context, idempotencyKey IdempotencyKey) error {
	if idempotencyKey == "" {
		return nil
	}

	_, err := p.db.Exec(ctx, `
        DELETE FROM pdp_idempotency 
        WHERE idempotency_key = $1 AND tx_hash IS NULL
    `, string(idempotencyKey))

	if err != nil {
		return fmt.Errorf("failed to cleanup idempotency key: %w", err)
	}

	return nil
}

// handleCreateIdempotencyResponse handles HTTP response for create operations
func (p *PDPService) handleCreateIdempotencyResponse(w http.ResponseWriter, result *IdempotencyResult) {
	if result.IsReserved {
		// Another request is processing this operation
		w.WriteHeader(http.StatusAccepted) // 202 - Processing
		return
	}

	if result.TxHash != nil && *result.TxHash != "" {
		// Operation already completed
		location := path.Join("/pdp/data-sets/created", *result.TxHash)
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusCreated) // 201 - Operation completed
		return
	}

	http.Error(w, "Invalid idempotency state", http.StatusInternalServerError)
}

// handleAddIdempotencyResponse handles HTTP response for add operations
func (p *PDPService) handleAddIdempotencyResponse(w http.ResponseWriter, result *IdempotencyResult, dataSetIdStr string) {
	if result.IsReserved {
		// Another request is processing this operation
		w.WriteHeader(http.StatusAccepted) // 202 - Processing
		return
	}

	if result.TxHash != nil && *result.TxHash != "" {
		// Operation already completed
		location := path.Join("/pdp/data-sets", dataSetIdStr, "pieces/added", *result.TxHash)
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusCreated) // 201 - Operation completed
		return
	}

	http.Error(w, "Invalid idempotency state", http.StatusInternalServerError)
}

// startIdempotencyCleanup starts background cleanup of old records
func (p *PDPService) startIdempotencyCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.cleanupOldIdempotencyRecords(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// cleanupOldIdempotencyRecords removes old records
func (p *PDPService) cleanupOldIdempotencyRecords(ctx context.Context) {
	// Clean up stuck reserved records (NULL tx_hash for > 1 hour)
	count, err := p.db.Exec(ctx, `
        DELETE FROM pdp_idempotency 
        WHERE tx_hash IS NULL 
        AND created_at < NOW() - INTERVAL '1 hour'
    `)
	if err != nil {
		logIdempotency.Errorw("Failed to cleanup old reserved idempotency records", "error", err)
	} else if count > 0 {
		logIdempotency.Infow("Cleaned up old reserved idempotency records", "count", count)
	}

	// Clean up old completed records (older than 24 hours)
	count, err = p.db.Exec(ctx, `
        DELETE FROM pdp_idempotency 
        WHERE tx_hash IS NOT NULL 
        AND created_at < NOW() - INTERVAL '24 hours'
    `)
	if err != nil {
		logIdempotency.Errorw("Failed to cleanup old completed idempotency records", "error", err)
	} else if count > 0 {
		logIdempotency.Infow("Cleaned up old completed idempotency records", "count", count)
	}
}
