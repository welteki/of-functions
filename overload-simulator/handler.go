package function

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	random          *rand.Rand
	defaultDuration time.Duration = time.Millisecond * 100
)

var (
	// Configuration from environment variables
	inflightThreshold   int
	failureMode         string // "constant" or "intermittent"
	useReadyEndpoint    bool   // Enable ready endpoint for load shedding
	thresholdStatusCode int    // HTTP status code to return when threshold is exceeded

	inflight int
	mux      = http.NewServeMux()
	mu       sync.Mutex
)

const (
	ModeConstant     = "constant"
	ModeIntermittent = "intermittent"
)

func init() {
	random = rand.New(rand.NewSource(time.Now().Unix()))

	// Read inflight threshold from environment variable
	inflightThreshold = 5 // default value
	if val, ok := os.LookupEnv("inflight_threshold"); ok && len(val) > 0 {
		threshold, err := strconv.Atoi(val)
		if err != nil {
			log.Fatalf("Error parsing inflight_threshold environment variable: %v", err)
		}
		inflightThreshold = threshold
	}

	// Read failure mode from environment variable
	failureMode = ModeConstant // default value
	if val, ok := os.LookupEnv("failure_mode"); ok && len(val) > 0 {
		if val != ModeConstant && val != ModeIntermittent {
			log.Fatalf("Invalid failure_mode: %s. Must be 'constant' or 'intermittent'", val)
		}
		failureMode = val
	}

	// Read sleep duration from environment variable
	if val, ok := os.LookupEnv("sleep_duration"); ok && len(val) > 0 {
		var err error
		defaultDuration, err = time.ParseDuration(val)
		if err != nil {
			log.Fatalf("Error parsing sleep_duration environment variable: %v", err)
		}
	}

	// Read use_ready_endpoint from environment variable
	useReadyEndpoint = false // default value
	if val, ok := os.LookupEnv("use_ready_endpoint"); ok && len(val) > 0 {
		useReady, err := strconv.ParseBool(val)
		if err != nil {
			log.Fatalf("Error parsing use_ready_endpoint environment variable: %v", err)
		}
		useReadyEndpoint = useReady
	}

	// Read threshold_status_code from environment variable
	thresholdStatusCode = http.StatusInternalServerError // default value (500)
	if val, ok := os.LookupEnv("status_code"); ok && len(val) > 0 {
		statusCode, err := strconv.Atoi(val)
		if err != nil {
			log.Fatalf("Error parsing status_code environment variable: %v", err)
		}
		thresholdStatusCode = statusCode
	}

	log.Printf("Overload Simulator initialized with threshold=%d, mode=%s, sleep=%s, use_ready=%v, status_code=%d",
		inflightThreshold, failureMode, defaultDuration, useReadyEndpoint, thresholdStatusCode)

	mux.HandleFunc("GET /_/config", getConfig)
	mux.HandleFunc("GET /ready", getReady)
	mux.HandleFunc("/", overloadSimulator)
}

func Handle(w http.ResponseWriter, r *http.Request) {
	mux.ServeHTTP(w, r)
}

type ConfigResponse struct {
	InflightThreshold   int           `json:"inflight_threshold"`
	FailureMode         string        `json:"failure_mode"`
	SleepDuration       time.Duration `json:"sleep_duration"`
	CurrentInflight     int           `json:"current_inflight"`
	UseReadyEndpoint    bool          `json:"use_ready_endpoint"`
	ThresholdStatusCode int           `json:"threshold_status_code"`
}

func getConfig(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	currentInflight := inflight
	mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	config := ConfigResponse{
		InflightThreshold:   inflightThreshold,
		FailureMode:         failureMode,
		SleepDuration:       defaultDuration,
		CurrentInflight:     currentInflight,
		UseReadyEndpoint:    useReadyEndpoint,
		ThresholdStatusCode: thresholdStatusCode,
	}

	fmt.Fprintf(w, `{"inflight_threshold":%d,"failure_mode":"%s","sleep_duration":"%s","current_inflight":%d,"use_ready_endpoint":%v,"threshold_status_code":%d}`,
		config.InflightThreshold, config.FailureMode, config.SleepDuration, config.CurrentInflight, config.UseReadyEndpoint, config.ThresholdStatusCode)
}

func getReady(w http.ResponseWriter, r *http.Request) {
	if !useReadyEndpoint {
		// Ready endpoint is disabled, always return ready
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
		return
	}

	mu.Lock()
	currentInflight := inflight
	mu.Unlock()

	// Report not ready when at or above the inflight threshold
	if currentInflight >= inflightThreshold {
		log.Printf("Ready check: NOT READY (inflight: %d, threshold: %d)", currentInflight, inflightThreshold)
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "Not ready: inflight=%d, threshold=%d", currentInflight, inflightThreshold)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK: inflight=%d, threshold=%d", currentInflight, inflightThreshold)
}

func overloadSimulator(w http.ResponseWriter, r *http.Request) {
	// Check threshold before incrementing (hard limit)
	mu.Lock()
	currentInflight := inflight

	// Check if we're at or over the threshold
	if currentInflight >= inflightThreshold {
		mu.Unlock()

		shouldFail := false

		switch failureMode {
		case ModeConstant:
			// Always fail when at threshold
			shouldFail = true
		case ModeIntermittent:
			// Fail 50% of the time when at threshold
			shouldFail = random.Float64() < 0.5
		}

		if shouldFail {
			log.Printf("Rejecting request at threshold (inflight: %d, threshold: %d, mode: %s)",
				currentInflight, inflightThreshold, failureMode)
			w.WriteHeader(thresholdStatusCode)
			fmt.Fprintf(w, "Simulated overload: inflight=%d, threshold=%d, mode=%s",
				currentInflight, inflightThreshold, failureMode)
			return
		}
	}

	// Increment inflight counter (only if under threshold or passed intermittent check)
	inflight++
	currentInflight = inflight
	mu.Unlock()

	// Always decrement inflight counter when done
	defer func() {
		mu.Lock()
		inflight--
		mu.Unlock()
	}()

	log.Printf("Processing request (inflight: %d, threshold: %d)", currentInflight, inflightThreshold)

	// Process the request successfully
	sleepDuration := defaultDuration

	// Allow override via header
	if val := r.Header.Get("X-Sleep"); len(val) > 0 {
		var err error
		sleepDuration, err = time.ParseDuration(val)
		if err != nil {
			log.Printf("Error parsing X-Sleep header: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error parsing X-Sleep header: %v", err)
			return
		}
	}

	log.Printf("Processing request for: %s", sleepDuration)
	time.Sleep(sleepDuration)

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK: processed in %s (inflight was: %d)", sleepDuration, currentInflight)
}
