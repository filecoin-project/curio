// vast-manager — Management service for Vast.ai cuzk/curio proving workers.
//
// Two HTTP listeners:
//   - API port (--listen, default :1235): instance-facing APIs + log push
//   - UI port  (--ui-listen, default 0.0.0.0:1236): web dashboard + management APIs
//
// SQLite state, background vast monitor, in-memory log buffers.
// See vast-cuzk-plan.md for the full spec.
package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ── Embedded UI ─────────────────────────────────────────────────────────

//go:embed ui.html
var uiFS embed.FS

// ── Log Buffer ──────────────────────────────────────────────────────────

type LogLine struct {
	Source string `json:"src"`
	Text   string `json:"text"`
	Time   int64  `json:"ts"`
}

type LogBuffer struct {
	mu    sync.Mutex
	lines []LogLine
	max   int
}

func NewLogBuffer(max int) *LogBuffer {
	return &LogBuffer{max: max, lines: make([]LogLine, 0, min(max, 1024))}
}

func (lb *LogBuffer) Append(source, text string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	now := time.Now().UnixMilli()
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		lb.lines = append(lb.lines, LogLine{Source: source, Text: line, Time: now})
		if len(lb.lines) > lb.max {
			drop := lb.max / 10
			if drop < 1 {
				drop = 1
			}
			lb.lines = lb.lines[drop:]
		}
	}
}

func (lb *LogBuffer) Lines(filter string, tail int) []LogLine {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	var src []LogLine
	if filter == "" || filter == "all" {
		src = lb.lines
	} else {
		for _, l := range lb.lines {
			if l.Source == filter {
				src = append(src, l)
			}
		}
	}
	if tail > 0 && len(src) > tail {
		src = src[len(src)-tail:]
	}
	result := make([]LogLine, len(src))
	copy(result, src)
	return result
}

func (lb *LogBuffer) Len() int {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return len(lb.lines)
}

// ── Manager Log Capture ─────────────────────────────────────────────────

type managerLogWriter struct {
	buf    *LogBuffer
	stdout io.Writer
}

func (w *managerLogWriter) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\n")
	if text != "" {
		w.buf.Append("manager", text)
	}
	return w.stdout.Write(p)
}

// ── Schema ──────────────────────────────────────────────────────────────

const schema = `
CREATE TABLE IF NOT EXISTS counters (
	key   TEXT PRIMARY KEY,
	value INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS instances (
	uuid           TEXT PRIMARY KEY,
	label          TEXT NOT NULL,
	runner_id      INTEGER UNIQUE NOT NULL,
	state          TEXT NOT NULL DEFAULT 'registered',
	min_rate       REAL NOT NULL DEFAULT 50,
	registered_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	param_done_at  TIMESTAMP,
	bench_done_at  TIMESTAMP,
	bench_rate     REAL,
	killed_at      TIMESTAMP,
	kill_reason    TEXT,
	vast_id        INTEGER,
	host_id        INTEGER,
	machine_id     INTEGER,
	gpu_name       TEXT,
	num_gpus       INTEGER,
	dph_total      REAL,
	geolocation    TEXT,
	cpu_name       TEXT,
	cpu_ram_mb     INTEGER,
	gpu_ram_mb     INTEGER,
	ssh_cmd        TEXT,
	public_ip      TEXT
);

CREATE TABLE IF NOT EXISTS bad_hosts (
	machine_id TEXT PRIMARY KEY,
	reason     TEXT NOT NULL,
	added_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS host_perf (
	machine_id  INTEGER NOT NULL,
	gpu_name    TEXT NOT NULL,
	num_gpus    INTEGER NOT NULL,
	bench_rate  REAL NOT NULL,
	cpu_ram_mb  INTEGER,
	measured_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (machine_id, gpu_name, num_gpus)
);
`

// ── Types ───────────────────────────────────────────────────────────────

type Instance struct {
	UUID         string   `json:"uuid"`
	Label        string   `json:"label"`
	RunnerID     int64    `json:"runner_id"`
	State        string   `json:"state"`
	MinRate      float64  `json:"min_rate"`
	RegisteredAt string   `json:"registered_at"`
	ParamDoneAt  *string  `json:"param_done_at,omitempty"`
	BenchDoneAt  *string  `json:"bench_done_at,omitempty"`
	BenchRate    *float64 `json:"bench_rate,omitempty"`
	KilledAt     *string  `json:"killed_at,omitempty"`
	KillReason   *string  `json:"kill_reason,omitempty"`

	// Persisted vast metadata
	VastID      *int     `json:"vast_id,omitempty"`
	HostID      *int     `json:"host_id,omitempty"`
	MachineID   *int     `json:"machine_id,omitempty"`
	GPUName     *string  `json:"gpu_name,omitempty"`
	NumGPUs     *int     `json:"num_gpus,omitempty"`
	DPHTotal    *float64 `json:"dph_total,omitempty"`
	Geolocation *string  `json:"geolocation,omitempty"`
	CPUName     *string  `json:"cpu_name,omitempty"`
	CPURAM      *int     `json:"cpu_ram_mb,omitempty"`
	GPURAM      *int     `json:"gpu_ram_mb,omitempty"`
	SSHCmd      *string  `json:"ssh_cmd,omitempty"`
	PublicIP    *string  `json:"public_ip,omitempty"`
}

type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type VastInstance struct {
	ID            int                      `json:"id"`
	Label         string                   `json:"label"`
	MachineID     int                      `json:"machine_id"`
	HostID        int                      `json:"host_id"`
	ActualStatus  string                   `json:"actual_status"`
	StartDate     float64                  `json:"start_date"`
	GPUName       string                   `json:"gpu_name"`
	NumGPUs       int                      `json:"num_gpus"`
	DPHTotal      float64                  `json:"dph_total"`
	DPHBase       float64                  `json:"dph_base"`
	CPUName       string                   `json:"cpu_name"`
	CPURAM        int                      `json:"cpu_ram"` // MB
	Geolocation   string                   `json:"geolocation"`
	CountryCode   string                   `json:"country_code"`
	InetDown      float64                  `json:"inet_down"` // Mbps
	InetUp        float64                  `json:"inet_up"`   // Mbps
	GPUUtil       float64                  `json:"gpu_util"`
	GPUTemp       float64                  `json:"gpu_temp"`
	GPURAM        int                      `json:"gpu_totalram"` // MB total
	GPUMemBW      float64                  `json:"gpu_mem_bw"`
	DiskSpace     float64                  `json:"disk_space"` // GB
	DiskUsage     float64                  `json:"disk_usage"`
	CUDAMaxGood   float64                  `json:"cuda_max_good"`
	DriverVersion string                   `json:"driver_version"`
	PublicIPAddr  string                   `json:"public_ipaddr"`
	Ports         map[string][]PortBinding `json:"ports"`
	MemUsage      float64                  `json:"mem_usage"` // GB
	MemLimit      float64                  `json:"mem_limit"` // GB
	CPUUtil       float64                  `json:"cpu_util"`
	Reliability   float64                  `json:"reliability2"`
	Duration      float64                  `json:"duration"` // seconds
	GPUFrac       float64                  `json:"gpu_frac"`
	StatusMsg     string                   `json:"status_msg"`

	// Computed
	SSHCmd string `json:"-"`
}

func (vi *VastInstance) computeSSH() {
	if vi.PublicIPAddr == "" {
		return
	}
	port := ""
	if bindings, ok := vi.Ports["22/tcp"]; ok && len(bindings) > 0 {
		port = bindings[0].HostPort
	}
	if port != "" {
		vi.SSHCmd = fmt.Sprintf("ssh -p %s root@%s", port, vi.PublicIPAddr)
	}
}

// Dashboard types for the UI API
type DashboardInstance struct {
	// DB fields
	UUID         string   `json:"uuid"`
	Label        string   `json:"label"`
	RunnerID     int64    `json:"runner_id"`
	State        string   `json:"state"`
	MinRate      float64  `json:"min_rate"`
	RegisteredAt string   `json:"registered_at"`
	ParamDoneAt  *string  `json:"param_done_at,omitempty"`
	BenchDoneAt  *string  `json:"bench_done_at,omitempty"`
	BenchRate    *float64 `json:"bench_rate,omitempty"`
	KilledAt     *string  `json:"killed_at,omitempty"`
	KillReason   *string  `json:"kill_reason,omitempty"`

	// Vast fields
	VastID        int     `json:"vast_id"`
	GPUName       string  `json:"gpu_name"`
	NumGPUs       int     `json:"num_gpus"`
	DPHTotal      float64 `json:"dph_total"`
	PricePerProof float64 `json:"price_per_proof"`
	SSHCmd        string  `json:"ssh_cmd"`
	HostID        int     `json:"host_id"`
	MachineID     int     `json:"machine_id"`
	Geolocation   string  `json:"geolocation"`
	CPURAM        int     `json:"cpu_ram_mb"`
	GPUUtil       float64 `json:"gpu_util"`
	GPUTemp       float64 `json:"gpu_temp"`
	GPURAM        int     `json:"gpu_ram_mb"`
	CUDAVersion   float64 `json:"cuda_version"`
	InetDown      float64 `json:"inet_down_mbps"`
	InetUp        float64 `json:"inet_up_mbps"`
	CPUUtil       float64 `json:"cpu_util"`
	MemUsageGB    float64 `json:"mem_usage_gb"`
	MemLimitGB    float64 `json:"mem_limit_gb"`
	DiskSpaceGB   float64 `json:"disk_space_gb"`
	GPUMemBW      float64 `json:"gpu_mem_bw"`
	DriverVersion string  `json:"driver_version"`
	Reliability   float64 `json:"reliability"`
	StartDate     float64 `json:"start_date"`
	GPUFrac       float64 `json:"gpu_frac"`
	StatusMsg     string  `json:"status_msg"`
	PublicIP      string  `json:"public_ip"`
	HasLogs       bool    `json:"has_logs"`
	LogLines      int     `json:"log_lines"`
}

type BadHostEntry struct {
	MachineID string `json:"machine_id"`
	Reason    string `json:"reason"`
	AddedAt   string `json:"added_at"`
}

// VastOffer represents a search result from `vastai search offers`
type VastOffer struct {
	ID          int     `json:"id"`
	HostID      int     `json:"host_id"`
	MachineID   int     `json:"machine_id"`
	GPUName     string  `json:"gpu_name"`
	NumGPUs     int     `json:"num_gpus"`
	GPURAM      int     `json:"gpu_ram"`       // MB per GPU
	GPUTotalRAM int     `json:"gpu_total_ram"` // MB total
	GPUMemBW    float64 `json:"gpu_mem_bw"`    // GB/s
	GPULanes    int     `json:"gpu_lanes"`     // PCIe lanes
	CPURAM      int     `json:"cpu_ram"`       // MB
	CPUCores    int     `json:"cpu_cores"`
	CPUCoresEff float64 `json:"cpu_cores_effective"`
	CPUName     string  `json:"cpu_name"`
	CPUGhz      float64 `json:"cpu_ghz"`
	DPHBase     float64 `json:"dph_base"`
	DPHTotal    float64 `json:"dph_total"`
	DiskSpace   float64 `json:"disk_space"`
	DiskBW      float64 `json:"disk_bw"`
	InetDown    float64 `json:"inet_down"`
	InetUp      float64 `json:"inet_up"`
	Geolocation string  `json:"geolocation"`
	CountryCode string  `json:"country_code"`
	CUDAMaxGood float64 `json:"cuda_max_good"`
	Reliability float64 `json:"reliability2"`
	DLPerf      float64 `json:"dlperf"`
	ComputeCap  int     `json:"compute_cap"`
	PCIeBW      float64 `json:"pcie_bw"` // GB/s
	HostRunTime float64 `json:"host_run_time"`
}

type HostPerf struct {
	MachineID  int     `json:"machine_id"`
	GPUName    string  `json:"gpu_name"`
	NumGPUs    int     `json:"num_gpus"`
	BenchRate  float64 `json:"bench_rate"`
	CPURAMMB   *int    `json:"cpu_ram_mb,omitempty"`
	MeasuredAt string  `json:"measured_at"`
}

type OfferWithPerf struct {
	VastOffer
	KnownPerf *HostPerf `json:"known_perf,omitempty"`
	IsBadHost bool      `json:"is_bad_host"`
	BadReason string    `json:"bad_reason,omitempty"`
}

type OffersResponse struct {
	Offers   []OfferWithPerf `json:"offers"`
	Total    int             `json:"total"`
	Filtered int             `json:"filtered"`
}

type DeployRequest struct {
	OfferID   int     `json:"offer_id"`
	MachineID int     `json:"machine_id"` // optional; if set, checked against bad_hosts
	MinRate   float64 `json:"min_rate"`
	Disk      int     `json:"disk"` // GB, default 250
}

type DeployResponse struct {
	OK         bool   `json:"ok"`
	InstanceID int    `json:"instance_id"`
	Message    string `json:"message,omitempty"`
}

type DashboardSummary struct {
	TotalInstances   int     `json:"total"`
	RunningCount     int     `json:"running"`
	BenchingCount    int     `json:"benching"`
	FetchingCount    int     `json:"fetching"`
	LoadingCount     int     `json:"loading"`
	KilledCount      int     `json:"killed"`
	TotalDPH         float64 `json:"total_dph"`
	TotalProofsHour  float64 `json:"total_proofs_hour"`
	AvgPricePerProof float64 `json:"avg_price_per_proof"`
	TotalGPUs        int     `json:"total_gpus"`
}

type DashboardResponse struct {
	Instances    []DashboardInstance `json:"instances"`
	BadHosts     []BadHostEntry      `json:"bad_hosts"`
	Summary      DashboardSummary    `json:"summary"`
	UpdatedAt    string              `json:"updated_at"`
	VastCacheAge float64             `json:"vast_cache_age_s"`
}

// ── Server ──────────────────────────────────────────────────────────────

type Server struct {
	db           *sql.DB
	mu           sync.Mutex // serialize DB writes
	managerLog   *LogBuffer
	instanceLogs sync.Map // uuid (string) -> *LogBuffer
	vastCache    []VastInstance
	vastCacheMu  sync.RWMutex
	vastCacheAt  time.Time
}

func NewServer(dbPath string) (*Server, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}

	_, err = db.Exec(`INSERT OR IGNORE INTO counters (key, value) VALUES ('runner_id_seq', 0)`)
	if err != nil {
		return nil, fmt.Errorf("seed counter: %w", err)
	}

	// Migrate: add vast metadata columns if they don't exist (idempotent)
	migrateCols := []string{
		"vast_id INTEGER", "host_id INTEGER", "machine_id INTEGER",
		"gpu_name TEXT", "num_gpus INTEGER", "dph_total REAL",
		"geolocation TEXT", "cpu_name TEXT", "cpu_ram_mb INTEGER",
		"gpu_ram_mb INTEGER", "ssh_cmd TEXT", "public_ip TEXT",
	}
	for _, col := range migrateCols {
		db.Exec("ALTER TABLE instances ADD COLUMN " + col) // ignore errors (column already exists)
	}

	// Migrate bad_hosts and host_perf: rename host_id -> machine_id
	// If old tables have host_id column, recreate them with machine_id
	db.Exec(`ALTER TABLE bad_hosts RENAME COLUMN host_id TO machine_id`) // ignore error if already renamed or new table
	db.Exec(`ALTER TABLE host_perf RENAME COLUMN host_id TO machine_id`) // ignore error if already renamed or new table

	return &Server{
		db:         db,
		managerLog: NewLogBuffer(5000),
	}, nil
}

func (s *Server) getInstanceLog(uuid string) *LogBuffer {
	if v, ok := s.instanceLogs.Load(uuid); ok {
		return v.(*LogBuffer)
	}
	lb := NewLogBuffer(10000)
	actual, _ := s.instanceLogs.LoadOrStore(uuid, lb)
	return actual.(*LogBuffer)
}

// ── Instance API Handlers ───────────────────────────────────────────────

type RegisterReq struct {
	Label   string  `json:"label"`
	MinRate float64 `json:"min_rate"`
}

type RegisterResp struct {
	UUID     string `json:"uuid"`
	RunnerID int64  `json:"runner_id"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Label == "" {
		httpError(w, "label is required", http.StatusBadRequest)
		return
	}
	if req.MinRate <= 0 {
		req.MinRate = 50
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var existing Instance
	err := s.db.QueryRow(
		`SELECT uuid, runner_id FROM instances WHERE label = ? AND state != 'killed'`,
		req.Label,
	).Scan(&existing.UUID, &existing.RunnerID)
	if err == nil {
		log.Printf("[register] re-register label=%s uuid=%s runner_id=%d", req.Label, existing.UUID, existing.RunnerID)
		jsonResp(w, RegisterResp{UUID: existing.UUID, RunnerID: existing.RunnerID})
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		httpError(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var runnerID int64
	err = tx.QueryRow(`UPDATE counters SET value = value + 1 WHERE key = 'runner_id_seq' RETURNING value`).Scan(&runnerID)
	if err != nil {
		httpError(w, "counter error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	uuid := generateUUID()
	_, err = tx.Exec(
		`INSERT INTO instances (uuid, label, runner_id, state, min_rate) VALUES (?, ?, ?, 'registered', ?)`,
		uuid, req.Label, runnerID, req.MinRate,
	)
	if err != nil {
		httpError(w, "insert error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		httpError(w, "commit error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[register] new label=%s uuid=%s runner_id=%d min_rate=%.1f", req.Label, uuid, runnerID, req.MinRate)
	jsonResp(w, RegisterResp{UUID: uuid, RunnerID: runnerID})
}

type UUIDReq struct {
	UUID string `json:"uuid"`
}

type OKResp struct {
	OK bool `json:"ok"`
}

func (s *Server) handleParamDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req UUIDReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var state string
	err := s.db.QueryRow(`SELECT state FROM instances WHERE uuid = ?`, req.UUID).Scan(&state)
	if err == sql.ErrNoRows {
		httpError(w, "instance not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpError(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if state != "registered" {
		log.Printf("[param-done] uuid=%s already in state=%s, no-op", req.UUID, state)
		jsonResp(w, OKResp{OK: true})
		return
	}

	_, err = s.db.Exec(
		`UPDATE instances SET state = 'params_done', param_done_at = CURRENT_TIMESTAMP WHERE uuid = ?`,
		req.UUID,
	)
	if err != nil {
		httpError(w, "update error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[param-done] uuid=%s", req.UUID)
	jsonResp(w, OKResp{OK: true})
}

type BenchDoneReq struct {
	UUID string  `json:"uuid"`
	Rate float64 `json:"rate"`
}

type BenchDoneResp struct {
	OK     bool `json:"ok"`
	Passed bool `json:"passed"`
}

func (s *Server) handleBenchDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req BenchDoneReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var minRate float64
	var state, label string
	err := s.db.QueryRow(
		`SELECT state, min_rate, label FROM instances WHERE uuid = ?`, req.UUID,
	).Scan(&state, &minRate, &label)
	if err == sql.ErrNoRows {
		httpError(w, "instance not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpError(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	passed := req.Rate >= minRate

	if passed {
		_, err = s.db.Exec(
			`UPDATE instances SET state = 'bench_done', bench_done_at = CURRENT_TIMESTAMP, bench_rate = ? WHERE uuid = ?`,
			req.Rate, req.UUID,
		)
	} else {
		_, err = s.db.Exec(
			`UPDATE instances SET state = 'killed', bench_done_at = CURRENT_TIMESTAMP, bench_rate = ?, killed_at = CURRENT_TIMESTAMP, kill_reason = ? WHERE uuid = ?`,
			req.Rate, fmt.Sprintf("bench_rate %.1f below min_rate %.1f", req.Rate, minRate), req.UUID,
		)
		// Destroy the vast instance immediately
		if vastID, ok := vastIDFromLabel(label); ok {
			go s.destroyInstance(vastID)
		}
	}
	if err != nil {
		httpError(w, "update error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[bench-done] uuid=%s rate=%.1f min_rate=%.1f passed=%v", req.UUID, req.Rate, minRate, passed)

	// Record host performance if we can resolve the vast instance
	if req.Rate > 0 {
		if vastID, ok := vastIDFromLabel(label); ok {
			s.vastCacheMu.RLock()
			for _, vi := range s.vastCache {
				if vi.ID == vastID {
					s.db.Exec(
						`INSERT INTO host_perf (machine_id, gpu_name, num_gpus, bench_rate, cpu_ram_mb)
					 VALUES (?, ?, ?, ?, ?)
					 ON CONFLICT(machine_id, gpu_name, num_gpus)
					 DO UPDATE SET bench_rate = MAX(bench_rate, excluded.bench_rate), cpu_ram_mb = ?, measured_at = CURRENT_TIMESTAMP`,
						vi.MachineID, vi.GPUName, vi.NumGPUs, req.Rate, vi.CPURAM,
						vi.CPURAM,
					)
					log.Printf("[host-perf] machine=%d gpu=%s×%d rate=%.1f", vi.MachineID, vi.GPUName, vi.NumGPUs, req.Rate)
					break
				}
			}
			s.vastCacheMu.RUnlock()
		}
	}

	jsonResp(w, BenchDoneResp{OK: true, Passed: passed})
}

func (s *Server) handleRunnerID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	uuid := r.URL.Query().Get("uuid")
	if uuid == "" {
		httpError(w, "uuid query param required", http.StatusBadRequest)
		return
	}

	var runnerID int64
	err := s.db.QueryRow(`SELECT runner_id FROM instances WHERE uuid = ?`, uuid).Scan(&runnerID)
	if err == sql.ErrNoRows {
		httpError(w, "instance not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpError(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, map[string]int64{"runner_id": runnerID})
}

func (s *Server) handleRunning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req UUIDReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE instances SET state = 'running' WHERE uuid = ? AND state != 'killed'`,
		req.UUID,
	)
	if err != nil {
		httpError(w, "update error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		httpError(w, "instance not found or already killed", http.StatusNotFound)
		return
	}

	log.Printf("[running] uuid=%s", req.UUID)
	jsonResp(w, OKResp{OK: true})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := s.db.Query(`SELECT uuid, label, runner_id, state, min_rate, registered_at,
		param_done_at, bench_done_at, bench_rate, killed_at, kill_reason
		FROM instances ORDER BY runner_id DESC`)
	if err != nil {
		httpError(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var instances []Instance
	for rows.Next() {
		var inst Instance
		if err := rows.Scan(&inst.UUID, &inst.Label, &inst.RunnerID, &inst.State,
			&inst.MinRate, &inst.RegisteredAt, &inst.ParamDoneAt, &inst.BenchDoneAt,
			&inst.BenchRate, &inst.KilledAt, &inst.KillReason); err != nil {
			httpError(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		instances = append(instances, inst)
	}
	if instances == nil {
		instances = []Instance{}
	}

	jsonResp(w, instances)
}

// ── Bad Host Handlers ───────────────────────────────────────────────────

type BadHostReq struct {
	MachineID string `json:"machine_id"`
	Reason    string `json:"reason"`
}

func (s *Server) handleBadHost(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req BadHostReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.MachineID == "" {
			httpError(w, "machine_id required", http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		_, err := s.db.Exec(
			`INSERT OR REPLACE INTO bad_hosts (machine_id, reason) VALUES (?, ?)`,
			req.MachineID, req.Reason,
		)
		if err != nil {
			httpError(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[bad-host] added machine_id=%s reason=%s", req.MachineID, req.Reason)
		jsonResp(w, OKResp{OK: true})

	case http.MethodGet:
		jsonResp(w, s.getBadHosts())

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBadHostDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	machineID := ""
	for i, p := range parts {
		if p == "bad-host" && i+1 < len(parts) {
			machineID = parts[i+1]
			break
		}
	}
	if machineID == "" {
		httpError(w, "machine_id required in path", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM bad_hosts WHERE machine_id = ?`, machineID)
	if err != nil {
		httpError(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[bad-host] removed machine_id=%s", machineID)
	jsonResp(w, OKResp{OK: true})
}

func (s *Server) getBadHosts() []BadHostEntry {
	rows, err := s.db.Query(`SELECT machine_id, reason, added_at FROM bad_hosts ORDER BY added_at DESC`)
	if err != nil {
		return []BadHostEntry{}
	}
	defer rows.Close()

	var hosts []BadHostEntry
	for rows.Next() {
		var h BadHostEntry
		if err := rows.Scan(&h.MachineID, &h.Reason, &h.AddedAt); err != nil {
			continue
		}
		hosts = append(hosts, h)
	}
	if hosts == nil {
		hosts = []BadHostEntry{}
	}
	return hosts
}

// ── Dashboard API Handlers ──────────────────────────────────────────────

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := uiFS.ReadFile("ui.html")
	if err != nil {
		http.Error(w, "ui not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Load DB instances
	rows, err := s.db.Query(`SELECT uuid, label, runner_id, state, min_rate, registered_at,
		param_done_at, bench_done_at, bench_rate, killed_at, kill_reason,
		vast_id, host_id, machine_id, gpu_name, num_gpus, dph_total,
		geolocation, cpu_name, cpu_ram_mb, gpu_ram_mb, ssh_cmd, public_ip
		FROM instances ORDER BY runner_id DESC`)
	if err != nil {
		httpError(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var dbInstances []Instance
	for rows.Next() {
		var inst Instance
		if err := rows.Scan(&inst.UUID, &inst.Label, &inst.RunnerID, &inst.State,
			&inst.MinRate, &inst.RegisteredAt, &inst.ParamDoneAt, &inst.BenchDoneAt,
			&inst.BenchRate, &inst.KilledAt, &inst.KillReason,
			&inst.VastID, &inst.HostID, &inst.MachineID, &inst.GPUName, &inst.NumGPUs,
			&inst.DPHTotal, &inst.Geolocation, &inst.CPUName, &inst.CPURAM, &inst.GPURAM,
			&inst.SSHCmd, &inst.PublicIP); err != nil {
			continue
		}
		dbInstances = append(dbInstances, inst)
	}

	// Load vast cache — build both label and ID maps for matching
	s.vastCacheMu.RLock()
	vastMap := make(map[string]VastInstance)
	vastIDMap := make(map[int]VastInstance)
	for _, vi := range s.vastCache {
		if vi.Label != "" {
			vastMap[vi.Label] = vi
		}
		vastIDMap[vi.ID] = vi
	}
	cacheAge := time.Since(s.vastCacheAt).Seconds()
	s.vastCacheMu.RUnlock()

	// Merge
	var summary DashboardSummary
	var dashInstances []DashboardInstance
	matchedVastIDs := make(map[int]bool)

	for _, db := range dbInstances {
		di := DashboardInstance{
			UUID:         db.UUID,
			Label:        db.Label,
			RunnerID:     db.RunnerID,
			State:        db.State,
			MinRate:      db.MinRate,
			RegisteredAt: db.RegisteredAt,
			ParamDoneAt:  db.ParamDoneAt,
			BenchDoneAt:  db.BenchDoneAt,
			BenchRate:    db.BenchRate,
			KilledAt:     db.KilledAt,
			KillReason:   db.KillReason,
		}

		if vi, ok := lookupVast(db.Label, vastMap, vastIDMap); ok {
			matchedVastIDs[vi.ID] = true
			di.VastID = vi.ID
			di.GPUName = vi.GPUName
			di.NumGPUs = vi.NumGPUs
			di.DPHTotal = vi.DPHTotal
			di.SSHCmd = vi.SSHCmd
			di.HostID = vi.HostID
			di.MachineID = vi.MachineID
			di.Geolocation = vi.Geolocation
			di.CPURAM = vi.CPURAM
			di.GPUUtil = vi.GPUUtil
			di.GPUTemp = vi.GPUTemp
			di.GPURAM = vi.GPURAM
			di.CUDAVersion = vi.CUDAMaxGood
			di.InetDown = vi.InetDown
			di.InetUp = vi.InetUp
			di.CPUUtil = vi.CPUUtil
			di.MemUsageGB = vi.MemUsage
			di.MemLimitGB = vi.MemLimit
			di.DiskSpaceGB = vi.DiskSpace
			di.GPUMemBW = vi.GPUMemBW
			di.DriverVersion = vi.DriverVersion
			di.Reliability = vi.Reliability
			di.StartDate = vi.StartDate
			di.GPUFrac = vi.GPUFrac
			di.StatusMsg = vi.StatusMsg
			di.PublicIP = vi.PublicIPAddr

			if db.BenchRate != nil && *db.BenchRate > 0 {
				di.PricePerProof = vi.DPHTotal / *db.BenchRate
			}
		} else {
			// Fallback to persisted DB metadata (for killed/disappeared instances)
			if db.VastID != nil {
				di.VastID = *db.VastID
			}
			if db.HostID != nil {
				di.HostID = *db.HostID
			}
			if db.MachineID != nil {
				di.MachineID = *db.MachineID
			}
			if db.GPUName != nil {
				di.GPUName = *db.GPUName
			}
			if db.NumGPUs != nil {
				di.NumGPUs = *db.NumGPUs
			}
			if db.DPHTotal != nil {
				di.DPHTotal = *db.DPHTotal
				if db.BenchRate != nil && *db.BenchRate > 0 {
					di.PricePerProof = *db.DPHTotal / *db.BenchRate
				}
			}
			if db.Geolocation != nil {
				di.Geolocation = *db.Geolocation
			}
			if db.CPURAM != nil {
				di.CPURAM = *db.CPURAM
			}
			if db.GPURAM != nil {
				di.GPURAM = *db.GPURAM
			}
			if db.SSHCmd != nil {
				di.SSHCmd = *db.SSHCmd
			}
			if db.PublicIP != nil {
				di.PublicIP = *db.PublicIP
			}
		}

		// Check if instance has logs
		if v, ok := s.instanceLogs.Load(db.UUID); ok {
			lb := v.(*LogBuffer)
			n := lb.Len()
			di.HasLogs = n > 0
			di.LogLines = n
		}

		dashInstances = append(dashInstances, di)

		// Summarize
		summary.TotalInstances++
		switch db.State {
		case "running":
			summary.RunningCount++
			summary.TotalDPH += di.DPHTotal
			summary.TotalGPUs += di.NumGPUs
			if db.BenchRate != nil {
				summary.TotalProofsHour += *db.BenchRate
			}
		case "bench_done", "params_done":
			summary.BenchingCount++
		case "registered":
			summary.FetchingCount++
		case "killed":
			summary.KilledCount++
		}
	}

	// Add unmatched vast instances as "loading" — deployed but not yet registered
	s.vastCacheMu.RLock()
	for _, vi := range s.vastCache {
		if matchedVastIDs[vi.ID] {
			continue
		}
		vi.computeSSH()
		di := DashboardInstance{
			State:         "loading",
			Label:         vi.Label,
			VastID:        vi.ID,
			GPUName:       vi.GPUName,
			NumGPUs:       vi.NumGPUs,
			DPHTotal:      vi.DPHTotal,
			SSHCmd:        vi.SSHCmd,
			HostID:        vi.HostID,
			MachineID:     vi.MachineID,
			Geolocation:   vi.Geolocation,
			CPURAM:        vi.CPURAM,
			GPUUtil:       vi.GPUUtil,
			GPUTemp:       vi.GPUTemp,
			GPURAM:        vi.GPURAM,
			CUDAVersion:   vi.CUDAMaxGood,
			InetDown:      vi.InetDown,
			InetUp:        vi.InetUp,
			CPUUtil:       vi.CPUUtil,
			MemUsageGB:    vi.MemUsage,
			MemLimitGB:    vi.MemLimit,
			DiskSpaceGB:   vi.DiskSpace,
			GPUMemBW:      vi.GPUMemBW,
			DriverVersion: vi.DriverVersion,
			Reliability:   vi.Reliability,
			StartDate:     vi.StartDate,
			GPUFrac:       vi.GPUFrac,
			StatusMsg:     vi.StatusMsg,
			PublicIP:      vi.PublicIPAddr,
		}
		dashInstances = append(dashInstances, di)
		summary.TotalInstances++
		summary.LoadingCount++
	}
	s.vastCacheMu.RUnlock()

	if summary.TotalProofsHour > 0 {
		summary.AvgPricePerProof = summary.TotalDPH / summary.TotalProofsHour
	}

	if dashInstances == nil {
		dashInstances = []DashboardInstance{}
	}

	resp := DashboardResponse{
		Instances:    dashInstances,
		BadHosts:     s.getBadHosts(),
		Summary:      summary,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		VastCacheAge: cacheAge,
	}

	jsonResp(w, resp)
}

func (s *Server) handleManagerLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tail := 500
	if t := r.URL.Query().Get("tail"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			tail = n
		}
	}

	lines := s.managerLog.Lines("", tail)
	jsonResp(w, lines)
}

func (s *Server) handleInstanceLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract UUID from path: /api/instance-logs/{uuid}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	uuid := ""
	for i, p := range parts {
		if p == "instance-logs" && i+1 < len(parts) {
			uuid = parts[i+1]
			break
		}
	}
	if uuid == "" {
		httpError(w, "uuid required in path", http.StatusBadRequest)
		return
	}

	filter := r.URL.Query().Get("source")
	tail := 1000
	if t := r.URL.Query().Get("tail"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			tail = n
		}
	}

	lb := s.getInstanceLog(uuid)
	lines := lb.Lines(filter, tail)
	if lines == nil {
		lines = []LogLine{}
	}
	jsonResp(w, lines)
}

// ── Log Push Handler (instances push logs here) ─────────────────────────

func (s *Server) handleLogPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	uuid := r.Header.Get("X-Instance-UUID")
	if uuid == "" {
		httpError(w, "X-Instance-UUID header required", http.StatusBadRequest)
		return
	}

	source := r.Header.Get("X-Log-Source")
	if source == "" {
		source = "unknown"
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024)) // 256KB max
	if err != nil {
		httpError(w, "read error: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(body) > 0 {
		lb := s.getInstanceLog(uuid)
		lb.Append(source, string(body))
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Kill Handler ────────────────────────────────────────────────────────

func (s *Server) handleKill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		VastID int    `json:"vast_id"`
		UUID   string `json:"uuid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.VastID > 0 {
		log.Printf("[kill] manually destroying vast instance %d (uuid=%s)", req.VastID, req.UUID)
		s.destroyInstance(req.VastID)
	}

	if req.UUID != "" {
		s.mu.Lock()
		s.db.Exec(
			`UPDATE instances SET state = 'killed', killed_at = CURRENT_TIMESTAMP, kill_reason = 'manually killed via UI' WHERE uuid = ? AND state != 'killed'`,
			req.UUID,
		)
		s.mu.Unlock()
	}

	jsonResp(w, OKResp{OK: true})
}

// ── Offers + Deploy ────────────────────────────────────────────────────

func (s *Server) searchVastOffers(filter string) ([]VastOffer, error) {
	cmd := exec.Command("vastai", "search", "offers", filter, "--raw")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("vastai search offers: %w", err)
	}

	var offers []VastOffer
	if err := json.Unmarshal(out, &offers); err != nil {
		return nil, fmt.Errorf("parse vast offers: %w (output: %s)", err, string(out[:min(len(out), 200)]))
	}
	return offers, nil
}

func (s *Server) getHostPerfs() map[int]HostPerf {
	perfs := make(map[int]HostPerf)
	rows, err := s.db.Query(`SELECT machine_id, gpu_name, num_gpus, bench_rate, cpu_ram_mb, measured_at FROM host_perf`)
	if err != nil {
		return perfs
	}
	defer rows.Close()
	for rows.Next() {
		var hp HostPerf
		rows.Scan(&hp.MachineID, &hp.GPUName, &hp.NumGPUs, &hp.BenchRate, &hp.CPURAMMB, &hp.MeasuredAt)
		perfs[hp.MachineID] = hp
	}
	return perfs
}

func (s *Server) handleOffers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Build vast filter from defaults + query params
	// Note: vast CLI filter uses GB for gpu_ram and cpu_ram, cuda_vers for CUDA version
	filter := "disk_space>=250 dph<=0.9 gpu_ram>12.5 cpu_ram>=240 cpu_cores>25 inet_down>100 cuda_vers>=13.0"
	if custom := r.URL.Query().Get("filter"); custom != "" {
		filter = custom
	}

	offers, err := s.searchVastOffers(filter)
	if err != nil {
		httpError(w, "search failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Load host perf and bad hosts
	perfs := s.getHostPerfs()
	badHosts := make(map[int]string)
	bhRows, err := s.db.Query(`SELECT machine_id, reason FROM bad_hosts`)
	if err == nil {
		defer bhRows.Close()
		for bhRows.Next() {
			var id int
			var reason string
			bhRows.Scan(&id, &reason)
			badHosts[id] = reason
		}
	}

	// Merge
	total := len(offers)
	result := make([]OfferWithPerf, 0, len(offers))
	for _, o := range offers {
		owp := OfferWithPerf{VastOffer: o}
		if hp, ok := perfs[o.MachineID]; ok {
			owp.KnownPerf = &hp
		}
		if reason, bad := badHosts[o.MachineID]; bad {
			owp.IsBadHost = true
			owp.BadReason = reason
		}
		result = append(result, owp)
	}

	jsonResp(w, OffersResponse{Offers: result, Total: total, Filtered: len(result)})
}

func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.OfferID <= 0 {
		httpError(w, "offer_id required", http.StatusBadRequest)
		return
	}
	if req.Disk <= 0 {
		req.Disk = 250
	}
	if req.MinRate <= 0 {
		req.MinRate = 30
	}

	// Check bad hosts if machine_id is provided
	if req.MachineID > 0 {
		var reason string
		err := s.db.QueryRow(`SELECT reason FROM bad_hosts WHERE machine_id = ?`, req.MachineID).Scan(&reason)
		if err == nil {
			httpError(w, fmt.Sprintf("machine %d is on bad hosts list: %s", req.MachineID, reason), http.StatusConflict)
			return
		}
	}

	// Read PAVAIL secret and server from env or use defaults
	pavailSecret := os.Getenv("PAVAIL_SECRET")
	if pavailSecret == "" {
		pavailSecret = "portavail1:QvjIWPmn1zHLMdVsOfHVswRCutIGffDKXMjw37GXJBo"
	}
	pavailServer := os.Getenv("PAVAIL_SERVER")
	if pavailServer == "" {
		pavailServer = "94.124.7.226:22222"
	}

	envArg := fmt.Sprintf("-e PAVAIL=%s -e PAVAIL_SERVER=%s -e MIN_RATE=%.1f",
		pavailSecret, pavailServer, req.MinRate)
	onstart := "nohup /usr/local/bin/entrypoint.sh > /var/log/entrypoint.log 2>&1 &"

	cmd := exec.Command("vastai", "create", "instance", strconv.Itoa(req.OfferID),
		"--image", "magik6k/curio-cuzk:latest",
		"--disk", strconv.Itoa(req.Disk),
		"--env", envArg,
		"--ssh", "--direct",
		"--onstart-cmd", onstart,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		httpError(w, "deploy failed: "+err.Error()+" output: "+strings.TrimSpace(string(out)), http.StatusInternalServerError)
		return
	}

	// Parse the instance ID from the output. Expected: "Started. {'success': True, 'new_contract': 12345, ...}"
	outStr := string(out)
	log.Printf("[deploy] offer=%d output: %s", req.OfferID, strings.TrimSpace(outStr))

	instanceID := 0
	// Try to extract new_contract from the Python-dict-like output
	if idx := strings.Index(outStr, "'new_contract':"); idx >= 0 {
		rest := outStr[idx+len("'new_contract':"):]
		rest = strings.TrimSpace(rest)
		// Read digits
		numStr := ""
		for _, c := range rest {
			if c >= '0' && c <= '9' {
				numStr += string(c)
			} else {
				break
			}
		}
		if n, err := strconv.Atoi(numStr); err == nil {
			instanceID = n
		}
	}

	jsonResp(w, DeployResponse{OK: true, InstanceID: instanceID, Message: strings.TrimSpace(outStr)})
}

func (s *Server) handleHostPerf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	perfs := s.getHostPerfs()
	result := make([]HostPerf, 0, len(perfs))
	for _, hp := range perfs {
		result = append(result, hp)
	}
	jsonResp(w, result)
}

// ── Background Monitor ─────────────────────────────────────────────────

func (s *Server) runMonitor() {
	// Run first cycle after 10s (quick startup check)
	time.Sleep(10 * time.Second)
	if err := s.monitorCycle(); err != nil {
		log.Printf("[monitor] error: %v (will retry next cycle)", err)
	}

	for {
		time.Sleep(60 * time.Second)
		if err := s.monitorCycle(); err != nil {
			log.Printf("[monitor] error: %v (will retry next cycle)", err)
		}
	}
}

func (s *Server) monitorCycle() error {
	vastInstances, err := s.getVastInstances()
	if err != nil {
		return fmt.Errorf("enumerate vast instances: %w", err)
	}

	// Compute SSH commands
	for i := range vastInstances {
		vastInstances[i].computeSSH()
	}

	// Update cache
	s.vastCacheMu.Lock()
	s.vastCache = vastInstances
	s.vastCacheAt = time.Now()
	s.vastCacheMu.Unlock()

	log.Printf("[monitor] cached %d vast instances", len(vastInstances))

	// Build label map and ID map for matching DB instances to vast instances.
	// The vast API label may be null even though VAST_CONTAINERLABEL is set
	// inside the container (the API label requires `vastai label instance`).
	// Our DB labels follow the pattern "C.<vast_id>", so we also index by ID.
	labelMap := make(map[string]VastInstance)
	idMap := make(map[int]VastInstance)
	for _, vi := range vastInstances {
		if vi.Label != "" {
			labelMap[vi.Label] = vi
		}
		idMap[vi.ID] = vi
	}

	// Load bad hosts (keyed by machine_id)
	badHosts := make(map[string]string)
	bhRows, err := s.db.Query(`SELECT machine_id, reason FROM bad_hosts`)
	if err != nil {
		return fmt.Errorf("load bad hosts: %w", err)
	}
	defer bhRows.Close()
	for bhRows.Next() {
		var id, reason string
		bhRows.Scan(&id, &reason)
		badHosts[id] = reason
	}

	now := time.Now()

	// Step 0: Kill instances on bad hosts
	for _, vi := range vastInstances {
		hostID := strconv.Itoa(vi.MachineID)
		if reason, bad := badHosts[hostID]; bad {
			log.Printf("[monitor] killing instance %d (label=%s) — bad host %s: %s", vi.ID, vi.Label, hostID, reason)
			s.destroyInstance(vi.ID)
			// Try vast API label first, fall back to C.<id> pattern
			dbLabel := vi.Label
			if dbLabel == "" {
				dbLabel = fmt.Sprintf("C.%d", vi.ID)
			}
			s.killInstanceByLabel(dbLabel, fmt.Sprintf("bad host %s: %s", hostID, reason))
		}
	}

	// Step 0.5: Persist vast metadata for all active DB instances matched to vast
	metaRows, err := s.db.Query(`SELECT uuid, label FROM instances WHERE state != 'killed'`)
	if err == nil {
		defer metaRows.Close()
		for metaRows.Next() {
			var uuid, label string
			metaRows.Scan(&uuid, &label)
			if vi, ok := lookupVast(label, labelMap, idMap); ok {
				vi.computeSSH()
				s.db.Exec(`UPDATE instances SET vast_id=?, host_id=?, machine_id=?, gpu_name=?, num_gpus=?,
					dph_total=?, geolocation=?, cpu_name=?, cpu_ram_mb=?, gpu_ram_mb=?, ssh_cmd=?, public_ip=?
					WHERE uuid=?`,
					vi.ID, vi.HostID, vi.MachineID, vi.GPUName, vi.NumGPUs,
					vi.DPHTotal, vi.Geolocation, vi.CPUName, vi.CPURAM, vi.GPURAM, vi.SSHCmd, vi.PublicIPAddr,
					uuid,
				)
			}
		}
	}

	// Step 1: Kill unregistered instances (15 min grace)
	// Build a set of registered labels AND extract vast IDs from C.<id> labels
	registeredLabels := make(map[string]bool)
	registeredVastIDs := make(map[int]bool)
	instRows, err := s.db.Query(`SELECT label FROM instances WHERE state != 'killed'`)
	if err != nil {
		return fmt.Errorf("load registered labels: %w", err)
	}
	defer instRows.Close()
	for instRows.Next() {
		var label string
		instRows.Scan(&label)
		registeredLabels[label] = true
		if id, ok := vastIDFromLabel(label); ok {
			registeredVastIDs[id] = true
		}
	}

	for _, vi := range vastInstances {
		// Check by vast API label and by vast instance ID (C.<id> pattern)
		if vi.Label != "" && registeredLabels[vi.Label] {
			continue
		}
		if registeredVastIDs[vi.ID] {
			continue
		}
		if vi.Label == "" {
			// No vast API label and not registered by ID — skip (grace for very new instances)
			continue
		}
		age := now.Sub(time.Unix(int64(vi.StartDate), 0))
		if age > 15*time.Minute {
			log.Printf("[monitor] killing unregistered instance %d (label=%s, age=%s)", vi.ID, vi.Label, age.Round(time.Second))
			s.destroyInstance(vi.ID)
		} else {
			log.Printf("[monitor] unregistered instance %d (label=%s, age=%s) — within grace period", vi.ID, vi.Label, age.Round(time.Second))
		}
	}

	// Step 2: Kill slow param fetches (90 min timeout)
	s.killTimedOut("registered", "registered_at", 90*time.Minute, "param fetch timeout (90min)", labelMap, idMap)

	// Step 3: Kill slow benchmarks (45 min timeout)
	// Benchmark flow: warmup (5-7min) + daemon restart/preload (1-2min) + 12 proofs (~20-30min)
	s.killTimedOut("params_done", "param_done_at", 45*time.Minute, "benchmark timeout (45min)", labelMap, idMap)

	// Step 4: Kill failed benchmarks
	failRows, err := s.db.Query(
		`SELECT uuid, label, bench_rate, min_rate FROM instances WHERE state = 'bench_done' AND bench_rate < min_rate`,
	)
	if err != nil {
		return fmt.Errorf("check failed benchmarks: %w", err)
	}
	defer failRows.Close()
	for failRows.Next() {
		var uuid, label string
		var rate, minRate float64
		failRows.Scan(&uuid, &label, &rate, &minRate)
		log.Printf("[monitor] killing failed benchmark uuid=%s rate=%.1f < min=%.1f", uuid, rate, minRate)
		if vi, ok := lookupVast(label, labelMap, idMap); ok {
			s.destroyInstance(vi.ID)
		}
		s.killInstanceByLabel(label, fmt.Sprintf("bench_rate %.1f below min_rate %.1f", rate, minRate))
	}

	// Step 5: Cleanup
	s.db.Exec(`DELETE FROM instances WHERE state = 'killed' AND killed_at < datetime('now', '-7 days')`)

	activeRows, err := s.db.Query(`SELECT uuid, label, state FROM instances WHERE state != 'killed'`)
	if err != nil {
		return fmt.Errorf("check active: %w", err)
	}
	defer activeRows.Close()
	for activeRows.Next() {
		var uuid, label, state string
		activeRows.Scan(&uuid, &label, &state)
		if _, ok := lookupVast(label, labelMap, idMap); !ok {
			log.Printf("[monitor] instance disappeared from vast uuid=%s label=%s state=%s", uuid, label, state)
			s.db.Exec(
				`UPDATE instances SET state = 'killed', killed_at = CURRENT_TIMESTAMP, kill_reason = 'instance disappeared from vast' WHERE uuid = ?`,
				uuid,
			)
		}
	}

	return nil
}

func (s *Server) killTimedOut(state, tsCol string, timeout time.Duration, reason string, labelMap map[string]VastInstance, idMap map[int]VastInstance) {
	query := fmt.Sprintf(`SELECT uuid, label, %s FROM instances WHERE state = ?`, tsCol)
	rows, err := s.db.Query(query, state)
	if err != nil {
		log.Printf("[monitor] killTimedOut query error: %v", err)
		return
	}
	defer rows.Close()

	now := time.Now()
	for rows.Next() {
		var uuid, label, ts string
		rows.Scan(&uuid, &label, &ts)
		t, err := time.Parse("2006-01-02 15:04:05", ts)
		if err != nil {
			t, err = time.Parse(time.RFC3339, ts)
			if err != nil {
				log.Printf("[monitor] bad timestamp for uuid=%s: %s", uuid, ts)
				continue
			}
		}
		if now.Sub(t) > timeout {
			log.Printf("[monitor] killing uuid=%s (state=%s, age=%s): %s", uuid, state, now.Sub(t).Round(time.Second), reason)
			if vi, ok := lookupVast(label, labelMap, idMap); ok {
				s.destroyInstance(vi.ID)
			}
			s.killInstanceByLabel(label, reason)
		}
	}
}

// vastIDFromLabel extracts the vast instance ID from a "C.<id>" label.
func vastIDFromLabel(label string) (int, bool) {
	if strings.HasPrefix(label, "C.") {
		id, err := strconv.Atoi(label[2:])
		if err == nil {
			return id, true
		}
	}
	return 0, false
}

// lookupVast finds a vast instance by DB label, trying the label map first,
// then falling back to extracting the vast ID from the "C.<id>" pattern.
func lookupVast(label string, labelMap map[string]VastInstance, idMap map[int]VastInstance) (VastInstance, bool) {
	if vi, ok := labelMap[label]; ok {
		return vi, true
	}
	if id, ok := vastIDFromLabel(label); ok {
		if vi, ok := idMap[id]; ok {
			return vi, true
		}
	}
	return VastInstance{}, false
}

func (s *Server) killInstanceByLabel(label, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(
		`UPDATE instances SET state = 'killed', killed_at = CURRENT_TIMESTAMP, kill_reason = ? WHERE label = ? AND state != 'killed'`,
		reason, label,
	)
}

// ── Vast CLI Helpers ────────────────────────────────────────────────────

func (s *Server) getVastInstances() ([]VastInstance, error) {
	cmd := exec.Command("vastai", "show", "instances", "--raw")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("vastai show instances: %w", err)
	}

	var instances []VastInstance
	if err := json.Unmarshal(out, &instances); err != nil {
		return nil, fmt.Errorf("parse vast output: %w (output: %s)", err, string(out[:min(len(out), 200)]))
	}
	return instances, nil
}

func (s *Server) destroyInstance(vastID int) {
	cmd := exec.Command("vastai", "destroy", "instance", strconv.Itoa(vastID))
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[monitor] failed to destroy vast instance %d: %v (%s)", vastID, err, strings.TrimSpace(string(out)))
	} else {
		log.Printf("[monitor] destroyed vast instance %d", vastID)
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────

func generateUUID() string {
	b := make([]byte, 16)
	f, err := os.Open("/dev/urandom")
	if err != nil {
		t := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(t >> (i * 4))
		}
	} else {
		f.Read(b)
		f.Close()
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ── cuzk Status via SSH ─────────────────────────────────────────────────

func (s *Server) handleCuzkStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract UUID from path: /api/cuzk-status/{uuid}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	uuid := ""
	for i, p := range parts {
		if p == "cuzk-status" && i+1 < len(parts) {
			uuid = parts[i+1]
			break
		}
	}
	if uuid == "" {
		httpError(w, "uuid required in path", http.StatusBadRequest)
		return
	}

	// Look up SSH info for this instance from the dashboard data.
	sshCmd := s.lookupSSHCmd(uuid)
	if sshCmd == "" {
		httpError(w, "no SSH info for instance", http.StatusNotFound)
		return
	}

	// Parse "ssh -p PORT root@HOST" into host and port
	sshHost, sshPort := parseSSHCmd(sshCmd)
	if sshHost == "" {
		httpError(w, "cannot parse SSH command: "+sshCmd, http.StatusInternalServerError)
		return
	}

	// SSH with ControlMaster for connection reuse. The first call
	// establishes the connection (~1s), subsequent calls reuse it (~50ms).
	controlPath := fmt.Sprintf("/tmp/vast-ssh-%%r@%%h:%%p")
	args := []string{
		"-p", sshPort,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + controlPath,
		"-o", "ControlPersist=120",
		"-o", "LogLevel=ERROR",
		"root@" + sshHost,
		"curl", "-sf", "--max-time", "3", "http://localhost:9821/status",
	}
	ctx := r.Context()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.Output()
	if err != nil {
		// Return a structured error so the UI can distinguish "not reachable" from "no status API"
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("ssh exec failed: %v", err),
		})
		return
	}

	// Pass through the raw JSON from the cuzk status endpoint
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(out)
}

// lookupSSHCmd returns the SSH command for an instance, checking
// both the live vast cache and the persisted DB value.
func (s *Server) lookupSSHCmd(uuid string) string {
	// First try vast cache (live data)
	s.vastCacheMu.RLock()
	for _, vi := range s.vastCache {
		if vi.Label == uuid && vi.SSHCmd != "" {
			s.vastCacheMu.RUnlock()
			return vi.SSHCmd
		}
	}
	s.vastCacheMu.RUnlock()

	// Fall back to DB persisted SSH command
	var sshCmd sql.NullString
	err := s.db.QueryRow(`SELECT ssh_cmd FROM instances WHERE uuid = ?`, uuid).Scan(&sshCmd)
	if err == nil && sshCmd.Valid {
		return sshCmd.String
	}
	return ""
}

// parseSSHCmd extracts host and port from "ssh -p PORT root@HOST"
func parseSSHCmd(cmd string) (host, port string) {
	parts := strings.Fields(cmd)
	port = "22"
	for i, p := range parts {
		if p == "-p" && i+1 < len(parts) {
			port = parts[i+1]
		}
		if strings.HasPrefix(p, "root@") {
			host = strings.TrimPrefix(p, "root@")
		}
	}
	return host, port
}

func jsonResp(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ── Router ──────────────────────────────────────────────────────────────

func (s *Server) setupRoutes() http.Handler {
	mux := http.NewServeMux()

	// Instance-facing APIs
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/param-done", s.handleParamDone)
	mux.HandleFunc("/bench-done", s.handleBenchDone)
	mux.HandleFunc("/runner-id", s.handleRunnerID)
	mux.HandleFunc("/running", s.handleRunning)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/bad-host", s.handleBadHost)
	mux.HandleFunc("/bad-host/", s.handleBadHostDelete)

	// Log push (instances → manager)
	mux.HandleFunc("/api/log-push", s.handleLogPush)

	// Dashboard APIs (UI-facing)
	mux.HandleFunc("/api/dashboard", s.handleDashboard)
	mux.HandleFunc("/api/manager-logs", s.handleManagerLogs)
	mux.HandleFunc("/api/instance-logs/", s.handleInstanceLogs)
	mux.HandleFunc("/api/kill", s.handleKill)
	mux.HandleFunc("/api/offers", s.handleOffers)
	mux.HandleFunc("/api/deploy", s.handleDeploy)
	mux.HandleFunc("/api/host-perf", s.handleHostPerf)
	mux.HandleFunc("/api/cuzk-status/", s.handleCuzkStatus)

	// Serve UI at root
	mux.HandleFunc("/", s.handleUI)

	return mux
}

// ── Main ────────────────────────────────────────────────────────────────

func main() {
	listen := ":1235"
	uiListen := "0.0.0.0:1236"
	dbPath := "/var/lib/vast-manager/state.db"

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen":
			if i+1 < len(args) {
				listen = args[i+1]
				i++
			}
		case "--ui-listen":
			if i+1 < len(args) {
				uiListen = args[i+1]
				i++
			}
		case "--db":
			if i+1 < len(args) {
				dbPath = args[i+1]
				i++
			}
		case "--help", "-h":
			fmt.Fprintf(os.Stderr, "Usage: vast-manager [--listen :1235] [--ui-listen 0.0.0.0:1236] [--db /path/to/state.db]\n")
			os.Exit(0)
		}
	}

	srv, err := NewServer(dbPath)
	if err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	// Capture manager logs
	logWriter := &managerLogWriter{buf: srv.managerLog, stdout: os.Stderr}
	log.SetOutput(logWriter)

	// Start background monitor
	go srv.runMonitor()

	handler := srv.setupRoutes()

	// Start API server
	go func() {
		log.Printf("API server listening on %s", listen)
		if err := http.ListenAndServe(listen, handler); err != nil {
			log.Fatalf("API server failed: %v", err)
		}
	}()

	// Start UI server
	log.Printf("Web UI listening on %s", uiListen)
	if err := http.ListenAndServe(uiListen, handler); err != nil {
		log.Fatalf("UI server failed: %v", err)
	}
}
