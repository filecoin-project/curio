// This is an example round-robin scheduler.
// It can be used by modifying Curio's base configuration Subsystems.UserSchedule
// to point to a machine named myscheduler with URL:
// http://myscheduler:7654/userschedule
// Be sure to open the selected port on the machine running this scheduler.
//
// Usage:
//
//	Fork the repo from Github and clone it to your local machine,
//	Edit this file as needed to implement your own scheduling logic,
//	build with 'make userschedule' then run with ./userschedule
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"runtime/pprof"
	"sync"
	"syscall"

	"golang.org/x/xerrors"
)

const WorkerBusyTimeout = 60 // Seconds until Curio asks again for this task.
func sched(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TaskID   string   `json:"task_id"`
		TaskType string   `json:"task_type"`
		Workers  []string `json:"workers"`
	}
	// Parse the request
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		OrHTTPFail(w, xerrors.Errorf("failed to parse request: %s", err))
	}

	// Scheduler Logic goes here
	selectedWorker := roundRobin(input.TaskType, input.Workers)

	// Respond to Curio
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(struct {
		Worker  string `json:"worker"`
		Timeout int    `json:"timeout"`
	}{selectedWorker, WorkerBusyTimeout})
	if err != nil {
		OrHTTPFail(w, err)
	}
}

// ///////// Round Robin Scheduler ///////// //
var mx sync.Mutex
var m = make(map[string]int)

func roundRobin(taskType string, workers []string) string {
	mx.Lock()
	defer mx.Unlock()
	selectedWorker := workers[m[taskType]%len(workers)]
	m[taskType]++
	return selectedWorker
}

// ///////////////////////////////////
// Everything below this line is boilerplate code.
// ///////////////////////////////////

func main() {
	setupCloseHandler()
	mux := http.NewServeMux()
	mux.HandleFunc("/userschedule", func(w http.ResponseWriter, r *http.Request) {
		defer recover()
		sched(w, r)
	})
	http.ListenAndServe(":7654", mux)
}

// Intentionally inlined dependencies to make it easy to copy-paste into your own codebase.
func OrHTTPFail(w http.ResponseWriter, err error) {
	if err != nil {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(err.Error()))
		log.Printf("http fail. err %s, stack %s", err, string(debug.Stack()))
		panic(1)
	}
}

func setupCloseHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\r- Ctrl+C pressed in Terminal")
		_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
		panic(1)
	}()
}
