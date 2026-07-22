package pdp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/pdp/explore"
)

const (
	explorePiecesDefaultLimit = 50
	explorePiecesMaxLimit     = 100
)

type explorePieceRow struct {
	PieceID    uint64    `db:"piece_id"`
	PieceCID   string    `db:"piece"`
	Size       uint64    `db:"size"`
	UploadedAt time.Time `db:"uploaded_at"`
}

type explorePieceJSON struct {
	PieceID    uint64    `json:"pieceId"`
	PieceCID   string    `json:"pieceCid"`
	Size       uint64    `json:"size"`
	UploadedAt time.Time `json:"uploadedAt"`
}

type explorePiecesResponse struct {
	DataSetID uint64             `json:"dataSetId"`
	Total     int64              `json:"total"`
	Pieces    []explorePieceJSON `json:"pieces"`
}

func mountExploreRoutes(r chi.Router, p *PDPService) {
	staticFS, err := fs.Sub(explore.StaticFS, "static")
	if err != nil {
		panic("pdp explore static fs: " + err.Error())
	}

	r.Route("/explore", func(r chi.Router) {
		r.Handle("/static/*", http.StripPrefix("/explore/static/", http.FileServer(http.FS(staticFS))))
		r.Get("/data-sets/{dataSetId}", p.handleExploreDataSetPage)
		r.Get("/data-sets/{dataSetId}/pieces", p.handleExploreDataSetPieces)
	})
}

func (p *PDPService) handleExploreDataSetPage(w http.ResponseWriter, r *http.Request) {
	dataSetID := chi.URLParam(r, "dataSetId")
	if dataSetID == "" {
		http.Error(w, "Missing data set ID", http.StatusBadRequest)
		return
	}
	if _, err := strconv.ParseUint(dataSetID, 10, 64); err != nil {
		http.Error(w, "Invalid data set ID", http.StatusBadRequest)
		return
	}

	body, err := explore.StaticFS.ReadFile("static/index.html")
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to load explorer page", err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

// handleExploreDataSetPieces lists pieces for the dataset explorer UI.
// Soft-gated: valid service JWT or Referer from /explore/data-sets/.
func (p *PDPService) handleExploreDataSetPieces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !p.allowExploreList(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	dataSetIDStr := chi.URLParam(r, "dataSetId")
	dataSetID, err := strconv.ParseUint(dataSetIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid data set ID", http.StatusBadRequest)
		return
	}

	limit := explorePiecesDefaultLimit
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "Invalid limit", http.StatusBadRequest)
			return
		}
		if n > explorePiecesMaxLimit {
			n = explorePiecesMaxLimit
		}
		limit = n
	}

	offset := 0
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, "Invalid offset", http.StatusBadRequest)
			return
		}
		offset = n
	}

	var exists bool
	err = p.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_data_sets WHERE id = $1)`, dataSetID).Scan(&exists)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to look up data set", err)
		return
	}
	if !exists {
		http.Error(w, "Data set not found", http.StatusNotFound)
		return
	}

	var total int64
	err = p.db.QueryRow(ctx, `
		SELECT COUNT(DISTINCT piece_id)
		FROM pdp_data_set_pieces
		WHERE data_set = $1 AND removed IS NOT TRUE
	`, dataSetID).Scan(&total)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to count pieces", err)
		return
	}

	var rows []explorePieceRow
	err = p.db.Select(ctx, &rows, `
		SELECT
			dsp.piece_id,
			MIN(dsp.piece) AS piece,
			COALESCE(SUM(pp.piece_raw_size), 0) AS size,
			MIN(ppr.created_at) AS uploaded_at
		FROM pdp_data_set_pieces dsp
		JOIN pdp_piecerefs ppr ON ppr.id = dsp.pdp_pieceref
		JOIN parked_piece_refs pprf ON pprf.ref_id = ppr.piece_ref
		JOIN parked_pieces pp ON pp.id = pprf.piece_id
		WHERE dsp.data_set = $1 AND dsp.removed IS NOT TRUE
		GROUP BY dsp.piece_id
		ORDER BY uploaded_at DESC, dsp.piece_id DESC
		LIMIT $2 OFFSET $3
	`, dataSetID, limit, offset)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpServerError(w, http.StatusInternalServerError, "Failed to list pieces", err)
		return
	}

	out := explorePiecesResponse{
		DataSetID: dataSetID,
		Total:     total,
		Pieces:    make([]explorePieceJSON, 0, len(rows)),
	}
	for _, row := range rows {
		pcInfo, err := PieceCidV2FromV1Str(row.PieceCID, row.Size)
		if err != nil {
			httpServerError(w, http.StatusInternalServerError, "Invalid piece CID", err)
			return
		}
		out.Pieces = append(out.Pieces, explorePieceJSON{
			PieceID:    row.PieceID,
			PieceCID:   pcInfo.CidV2.String(),
			Size:       row.Size,
			UploadedAt: row.UploadedAt.UTC(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}
