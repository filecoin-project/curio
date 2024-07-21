package hapi

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"text/template"

	"github.com/gorilla/mux"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/chain/actors/adt"
)

type app struct {
	db   *harmonydb.DB
	deps *deps.Deps
	t    *template.Template
	stor adt.Store

	actorInfoLk sync.Mutex
	actorInfos  []actorInfo
}

type actorInfo struct {
	Address string
	CLayers []string

	QualityAdjustedPower string
	RawBytePower         string

	ActorBalance, ActorAvailable, WorkerBalance string

	Win1, Win7, Win30 int64

	Deadlines []actorDeadline
}

type actorDeadline struct {
	Empty      bool
	Current    bool
	Proven     bool
	PartFaulty bool
	Faulty     bool
}

func (a *app) actorSummary(w http.ResponseWriter, r *http.Request) {
	a.actorInfoLk.Lock()
	defer a.actorInfoLk.Unlock()

	a.executeTemplate(w, "actor_summary", a.actorInfos)
}

func (a *app) sectorResume(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	id, ok := params["id"]
	if !ok {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	intid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	sp, ok := params["sp"]
	if !ok {
		http.Error(w, "missing sp", http.StatusBadRequest)
		return
	}

	maddr, err := address.NewFromString(sp)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	// call CREATE OR REPLACE FUNCTION unset_task_id(sp_id_param bigint, sector_number_param bigint)

	_, err = a.db.Exec(r.Context(), `SELECT unset_task_id($1, $2)`, spid, intid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to resume sector: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	// redir back to /hapi/sector/{sp}/{id}

	http.Redirect(w, r, fmt.Sprintf("/hapi/sector/%s/%d", sp, intid), http.StatusSeeOther)
}

func (a *app) sectorRemove(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	id, ok := params["id"]
	if !ok {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	intid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	sp, ok := params["sp"]
	if !ok {
		http.Error(w, "missing sp", http.StatusBadRequest)
		return
	}

	maddr, err := address.NewFromString(sp)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	_, err = a.db.Exec(r.Context(), `DELETE FROM sectors_sdr_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to resume sector: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	_, err = a.db.Exec(r.Context(), `INSERT INTO storage_removal_marks (sp_id, sector_num, sector_filetype, storage_id, created_at, approved, approved_at)
		SELECT miner_id, sector_num, sector_filetype, storage_id, current_timestamp, FALSE, NULL FROM sector_location
		WHERE miner_id = $1 AND sector_num = $2`, spid, intid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to mark sector for removal: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	// redir back to /pipeline_porep.html

	http.Redirect(w, r, "/pipeline_porep.html", http.StatusSeeOther)
}

var templateDev = os.Getenv("CURIO_WEB_DEV") == "1"

func (a *app) executeTemplate(w http.ResponseWriter, name string, data interface{}) {
	if templateDev {
		fs := os.DirFS("./curiosrc/web/hapi/web")
		a.t = template.Must(makeTemplate().ParseFS(fs, "*"))
	}
	if err := a.t.ExecuteTemplate(w, name, data); err != nil {
		log.Errorf("execute template %s: %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func (a *app) executePageTemplate(w http.ResponseWriter, name, title string, data interface{}) {
	if templateDev {
		fs := os.DirFS("./curiosrc/web/hapi/web")
		a.t = template.Must(makeTemplate().ParseFS(fs, "*"))
	}
	var contentBuf bytes.Buffer
	if err := a.t.ExecuteTemplate(&contentBuf, name, data); err != nil {
		log.Errorf("execute template %s: %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
	a.executeTemplate(w, "root", map[string]interface{}{
		"PageTitle": title,
		"Content":   contentBuf.String(),
	})
}

type taskSummary struct {
	Name           string
	SincePosted    string
	Owner, OwnerID *string
	ID             int64
}
