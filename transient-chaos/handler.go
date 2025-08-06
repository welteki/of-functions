package function

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

var (
	config = &ChaosConfig{
		FailureCode:  http.StatusTooManyRequests,
		FailureCount: 2,
		SuccessCode:  http.StatusOK,
		Delay:        time.Second * 2,
	}

	retryCounter = make(map[string]int)
	mux          = http.NewServeMux()
	mu           sync.RWMutex
)

type ChaosConfig struct {
	FailureCode  int           `json:"status"`
	FailureCount int           `json:"failure_count"`
	SuccessCode  int           `json:"success_code"`
	Delay        time.Duration `json:"delay"`
}

func init() {
	mux.HandleFunc("GET /_/config", getConfig)
	mux.HandleFunc("POST /_/config", setConfig)
	mux.HandleFunc("/", transientChaos)
}

func Handle(w http.ResponseWriter, r *http.Request) {
	mux.ServeHTTP(w, r)
}

func getConfig(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(config); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func setConfig(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "No body", http.StatusBadRequest)
		return
	}

	defer r.Body.Close()

	var cfg *ChaosConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mu.Lock()
	config = cfg
	mu.Unlock()

	w.WriteHeader(http.StatusAccepted)
}

func transientChaos(w http.ResponseWriter, r *http.Request) {
	callID := r.Header.Get("X-Call-ID")
	if len(callID) == 0 {
		http.Error(w, "Missing X-Call-ID header", http.StatusBadRequest)
		return
	}

	if config.Delay > 0 {
		time.Sleep(config.Delay)
	}

	mu.Lock()
	retryCounter[callID]++
	if retryCounter[callID] > config.FailureCount {
		w.WriteHeader(config.SuccessCode)
		delete(retryCounter, callID)
	} else {
		w.WriteHeader(config.FailureCode)
	}
	mu.Unlock()
}
