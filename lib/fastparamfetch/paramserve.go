package fastparamfetch

import (
	"context"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

type ParamServe struct {
	db *harmonydb.DB

	lk    sync.Mutex
	allow map[string]bool // file CIDs

	cidToFile map[string]string // mapping from CID string to file path

	machineID int
}

func NewParamServe(db *harmonydb.DB, machineID int) *ParamServe {
	return &ParamServe{
		db:        db,
		allow:     make(map[string]bool),
		cidToFile: make(map[string]string),
		machineID: machineID,
	}
}

func (ps *ParamServe) allowCid(ctx context.Context, c cid.Cid, path string) {
	ps.lk.Lock()
	defer ps.lk.Unlock()

	ps.allow[c.String()] = true
	ps.cidToFile[c.String()] = path

	// Insert into the database that this machine has this CID
	err := ps.insertCidForMachine(ctx, c.String())
	if err != nil {
		log.Errorf("Failed to insert CID %s for machine: %v", c.String(), err)
	}
}

func (ps *ParamServe) insertCidForMachine(ctx context.Context, cidStr string) error {
	// Insert into paramfetch_urls (machine, cid)
	_, err := ps.db.Exec(ctx, `INSERT INTO paramfetch_urls (machine, cid) VALUES ($1, $2) ON CONFLICT DO NOTHING`, ps.machineID, cidStr)
	return err
}

func (ps *ParamServe) urlsForCid(ctx context.Context, c cid.Cid) ([]string, error) {
	rows, err := ps.db.Query(ctx, `SELECT harmony_machines.host_and_port FROM harmony_machines
        JOIN paramfetch_urls ON harmony_machines.id = paramfetch_urls.machine
        WHERE paramfetch_urls.cid = $1`, c.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var hostAndPort string
		if err := rows.Scan(&hostAndPort); err != nil {
			return nil, err
		}
		urls = append(urls, hostAndPort)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return urls, nil
}

func (ps *ParamServe) hostsWithAllCids(ctx context.Context, cids []string) ([]string, error) {
	if len(cids) == 0 {
		return nil, nil
	}

	rows, err := ps.db.Query(ctx, `SELECT hm.host_and_port
FROM harmony_machines hm
JOIN paramfetch_urls pu ON hm.id = pu.machine
WHERE pu.cid = ANY($1)
GROUP BY hm.id, hm.host_and_port
HAVING COUNT(DISTINCT pu.cid) = $2`, cids, len(cids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []string
	for rows.Next() {
		var hostAndPort string
		if err := rows.Scan(&hostAndPort); err != nil {
			return nil, err
		}
		hosts = append(hosts, hostAndPort)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return hosts, nil
}

func (ps *ParamServe) getFilePathForCid(c cid.Cid) (string, error) {
	ps.lk.Lock()
	defer ps.lk.Unlock()

	filePath, ok := ps.cidToFile[c.String()]
	if !ok {
		return "", xerrors.Errorf("file path for CID %s not found", c.String())
	}
	return filePath, nil
}

func (ps *ParamServe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cidStr := vars["cid"]
	if cidStr == "" {
		http.Error(w, "CID not specified", http.StatusBadRequest)
		return
	}

	// Parse the CID
	c, err := cid.Parse(cidStr)
	if err != nil {
		http.Error(w, "Invalid CID", http.StatusBadRequest)
		return
	}

	ps.lk.Lock()
	allowed := ps.allow[c.String()]
	ps.lk.Unlock()
	if !allowed {
		http.Error(w, "CID not allowed", http.StatusNotFound)
		return
	}

	filePath, err := ps.getFilePathForCid(c)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, filePath)
}

func Routes(r *mux.Router, deps *deps.Deps, serve *ParamServe) {
	r.Methods("GET", "HEAD").Path("/params/ipfs/{cid}").HandlerFunc(serve.ServeHTTP)
}
