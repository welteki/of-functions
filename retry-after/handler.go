package function

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
)

var (
	invocations atomic.Int64

	failCount      int64  = 3
	retryAfterSecs string = ""

	mux = http.NewServeMux()
)

func init() {
	if v, ok := os.LookupEnv("fail_count"); ok {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil || parsed < 0 {
			log.Fatalf("invalid fail_count %q: must be a non-negative integer", v)
		}
		failCount = parsed
	}

	retryAfterSecs = os.Getenv("retry_after_secs")

	mux.HandleFunc("POST /_/reset", resetHandler)
	mux.HandleFunc("/", mainHandler)
}

func Handle(w http.ResponseWriter, r *http.Request) {
	mux.ServeHTTP(w, r)
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
	n := invocations.Add(1)

	w.Header().Set("Content-Type", "text/plain")

	if n <= failCount {
		if retryAfterSecs != "" {
			w.Header().Set("Retry-After", retryAfterSecs)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintf(w, "invocation %d: 429\n", n)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "invocation %d: 200 OK\n", n)
}

func resetHandler(w http.ResponseWriter, r *http.Request) {
	invocations.Store(0)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "invocation counter reset")
}
