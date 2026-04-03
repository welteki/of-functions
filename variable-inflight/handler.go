package function

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	random          *rand.Rand
	defaultDuration time.Duration = time.Second * 2
)

var (
	config = &InflightConfig{
		MinInflight: 50,
		MaxInflight: 100,
		Period:      time.Minute * 2,
	}

	getMaxInfligtht     func() float64
	thresholdStatusCode int

	inflight int
	ready    bool = true
	mux           = http.NewServeMux()
	mu       sync.Mutex
)

type InflightConfig struct {
	MinInflight int           `json:"min_inflight"`
	MaxInflight int           `json:"max_inflight"`
	Period      time.Duration `json:"period"`
}

func init() {
	random = rand.New(rand.NewSource(time.Now().Unix()))

	if val, ok := os.LookupEnv("sleep_duration"); ok && len(val) > 0 {
		var err error
		defaultDuration, err = time.ParseDuration(val)
		if err != nil {
			log.Fatalf("Error parsing sleep_duration environment variable: %v", err)
		}
	}

	thresholdStatusCode = http.StatusTooManyRequests // default value (429)
	if val, ok := os.LookupEnv("status_code"); ok && len(val) > 0 {
		statusCode, err := strconv.Atoi(val)
		if err != nil {
			log.Fatalf("Error parsing status_code environment variable: %v", err)
		}
		thresholdStatusCode = statusCode
	}

	getMaxInfligtht = Oscillator(float64(config.MinInflight), float64(config.MaxInflight), config.Period)
	mux.HandleFunc("/_/ready", readiness)
	mux.HandleFunc("/", variableInflight)
}

func Handle(w http.ResponseWriter, r *http.Request) {
	mux.ServeHTTP(w, r)
}

func variableInflight(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	inflight++
	mu.Unlock()

	defer func() {
		mu.Lock()
		inflight--
		mu.Unlock()
	}()

	maxInflight := getMaxInfligtht()
	fmt.Printf("Max inflight: %.0f\n", maxInflight)

	if inflight > int(maxInflight) {
		mu.Lock()
		ready = false
		mu.Unlock()

		w.WriteHeader(thresholdStatusCode)
		fmt.Fprintf(w, "Threshold exceeded: inflight=%d, max=%.0f", inflight, maxInflight)
		return
	} else if !ready {
		mu.Lock()
		ready = true
		mu.Unlock()
	}

	sleep(w, r)
}

func readiness(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	if ready {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusServiceUnavailable)
}

func sleep(w http.ResponseWriter, r *http.Request) {
	if minV := r.Header.Get("X-Min-Sleep"); len(minV) > 0 {
		if maxV := r.Header.Get("X-Max-Sleep"); len(maxV) > 0 {
			minSleep, err := time.ParseDuration(minV)
			if err != nil {
				log.Printf("Error parsing X-Min-Sleep header: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "Error parsing X-Min-Sleep header: %v", err)
				return
			}
			maxSleep, err := time.ParseDuration(maxV)
			if err != nil {
				log.Printf("Error parsing X-Max-Sleep header: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "Error parsing X-Max-Sleep header: %v", err)
				return
			}

			minMs := minSleep.Milliseconds()
			maxMs := maxSleep.Milliseconds()

			// Normalize and handle edge cases to avoid panic in Int63n
			if maxMs < minMs {
				minMs, maxMs = maxMs, minMs
			}

			var randMs int64
			rangeMs := maxMs - minMs
			if rangeMs <= 0 {
				randMs = minMs // min == max, use fixed duration
			} else {
				randMs = random.Int63n(rangeMs+1) + minMs // inclusive of max
			}

			sleepDuration, _ := time.ParseDuration(fmt.Sprintf("%dms", randMs))

			log.Printf("Start sleep for: %fs\n", sleepDuration.Seconds())
			time.Sleep(sleepDuration)
			log.Printf("Sleep done for: %fs\n", sleepDuration.Seconds())

			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Slept for: %fs", sleepDuration.Seconds())
			return
		}
	}

	sleepDuration := defaultDuration

	var err error
	if val := r.Header.Get("X-Sleep"); len(val) > 0 {
		sleepDuration, err = time.ParseDuration(val)
		if err != nil {
			log.Printf("Error parsing X-Sleep header: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error parsing X-Sleep header: %v", err)
			return
		}
	}

	log.Printf("Start sleep for: %fs\n", sleepDuration.Seconds())
	time.Sleep(sleepDuration)
	log.Printf("Sleep done for: %fs\n", sleepDuration.Seconds())

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Slept for: %fs", sleepDuration.Seconds())
}

// Oscillator returns a function that gives the current oscillating value over time.
// The value oscillates between min and max, completing one full cycle every 'period' duration.
func Oscillator(min, max float64, period time.Duration) func() float64 {
	amplitude := (max - min) / 2
	midpoint := min + amplitude
	start := time.Now()

	return func() float64 {
		elapsed := time.Since(start).Seconds()
		// Normalize elapsed time to radians (2π per period)
		angle := (elapsed / period.Seconds()) * 2 * math.Pi
		// Sine oscillates between -1 and 1 → scale to [min, max]
		return midpoint + amplitude*math.Sin(angle)
	}
}
