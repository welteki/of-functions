package function

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openfaas/go-sdk"
)

var (
	gatewayURL *url.URL
	ofName     string
	client     *sdk.Client

	maxInvocations  int
	invocationCount = make(map[string]int)

	lock sync.RWMutex

	stopped bool
)

func init() {
	ofName = os.Getenv("OPENFAAS_NAME")

	rawGatewayURL := os.Getenv("OPENFAAS_URL")
	if len(rawGatewayURL) > 0 {
		var err error
		gatewayURL, err = url.Parse(rawGatewayURL)
		if err != nil {
			log.Fatalf("Failed to parse gateway URL: %v", err)
		}
	} else {
		gatewayURL, _ = url.Parse("http://127.0.0.1:8080")
	}

	log.Printf("Gateway URL: %s", gatewayURL.String())

	httpClient := newHTTPClient(time.Second*20, 1024, 1024, false)

	client = sdk.NewClient(gatewayURL, nil, httpClient)
	maxInvocations = -1
}

func Handle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if strings.HasPrefix(r.URL.Path, "/reset") {
		if fn := r.URL.Query().Get("function"); fn != "" {
			lock.Lock()
			invocationCount[fn] = 0
			lock.Unlock()
			return
		}

		lock.Lock()
		invocationCount = map[string]int{}
		lock.Unlock()
		return
	}

	if strings.HasPrefix(r.URL.Path, "/config") {
		if val := r.URL.Query().Get("max-invocations"); val != "" {
			parsedVal, err := strconv.Atoi(val)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(fmt.Sprintf("Invalid max-invocations value: %v", err)))
				return
			}
			maxInvocations = parsedVal
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Max Invocations: %d", maxInvocations)))
		return
	}

	if strings.HasPrefix(r.URL.Path, "/stop") {
		stopped = true
		return
	}

	if strings.HasPrefix(r.URL.Path, "/start") {
		stopped = false
		return
	}

	if stopped {
		log.Printf("Service is stopped")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Service is stopped"))
		return
	}

	fnName := r.Header.Get("X-Function-Name")
	if fnName == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Missing function name"))
		return
	}

	parts := strings.Split(fnName, ".")
	if len(parts) != 2 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid function name"))
		return
	}

	lock.Lock()
	log.Printf("Received callback from %s, count: %d", fnName, invocationCount[fnName])
	if maxInvocations >= 0 && invocationCount[fnName] >= maxInvocations {
		log.Printf("Maximum invocations reached for %s", fnName)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("Maximum invocations reached"))
		lock.Unlock()
		return
	}
	invocationCount[fnName]++
	lock.Unlock()

	fn := parts[0]
	namespace := parts[1]

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Error creating request"))
		return
	}

	callbackURL := gatewayURL.JoinPath(fmt.Sprintf("/function/%s", ofName))
	req.Header.Set("X-Callback-URL", callbackURL.String())

	async := true
	authenticate := false

	res, err := client.InvokeFunction(fn, namespace, async, authenticate, req)
	if err != nil {
		log.Printf("Error invoking function: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Error invoking function"))
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusAccepted && res.StatusCode != http.StatusOK {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Error invoking function"))
		return
	}

	w.WriteHeader(http.StatusOK)
}

func newHTTPClient(timeout time.Duration, maxIdleConns int, maxIdleConnsPerHost int, tlsInsecureVerify bool) *http.Client {
	return &http.Client{
		// these Transport values ensure that the http Client will eventually timeout and prevents
		// infinite retries. The default http.Client configure these timeouts.  The specific
		// values tuned via performance testing/benchmarking
		//
		// Additional context can be found at
		// - https://medium.com/@nate510/don-t-use-go-s-default-http-client-4804cb19f779
		// - https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/
		//
		// Additionally, these overrides for the default client enable re-use of connections and prevent
		// CoreDNS from rate limiting under high traffic
		//
		// See also two similar projects where this value was updated:
		// https://github.com/prometheus/prometheus/pull/3592
		// https://github.com/minio/minio/pull/5860
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: tlsInsecureVerify,
			},
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   timeout,
				KeepAlive: 1 * time.Second,
				DualStack: true,
			}).DialContext,

			MaxIdleConns:          maxIdleConns,
			MaxIdleConnsPerHost:   maxIdleConnsPerHost,
			IdleConnTimeout:       120 * time.Millisecond,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1500 * time.Millisecond,
		},

		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
