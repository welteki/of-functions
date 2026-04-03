# OpenFaaS Test Functions

A collection of utility functions for testing and demonstrating OpenFaaS capabilities.

| Function | Description |
|---|---|
| [overload-simulator](#overload-simulator) | Simulates an overloaded function that rejects requests when inflight request thresholds are exceeded |
| [variable-inflight](#variable-inflight) | Simulates oscillating inflight capacity using a sine wave with ready-check-based load shedding |
| [transient-chaos](#transient-chaos) | Simulates transient failures that succeed after a configurable number of retries |
| [retry-after](#retry-after) | Returns 429 with an optional Retry-After header for a configurable number of invocations before succeeding |
| [async-load](#async-load) | Generates continuous async invocation load via self-referencing callbacks |
| [callback-counter](#callback-counter) | Counts callback invocations for measuring async throughput |
| [ndjson](#ndjson) | Streams newline-delimited JSON events at 1 event/second |
| [ping](#ping) | Simple ping function for basic connectivity testing |

## Functions

### overload-simulator

Simulates an overloaded function that starts returning errors when inflight request thresholds are exceeded.

**Use Cases:**
- Testing circuit breakers and retry logic
- Load testing error handling scenarios
- Demonstrating backpressure mechanisms
- Validating rate limiting strategies

**Configuration (Environment Variables):**

- `inflight_threshold` (default: `5`): Number of concurrent requests before overload simulation begins
- `failure_mode` (default: `constant`): How failures are triggered when over threshold
  - `constant`: Returns errors 100% of the time when over threshold
  - `intermittent`: Returns errors 50% of the time when over threshold
- `status_code` (default: `500`): HTTP status code to return when threshold is exceeded
- `sleep_duration` (default: `100ms`): Time each request takes to process
- `use_ready_endpoint` (default: `false`): Enable ready endpoint for load shedding
  - When `true`, the `/_/ready` endpoint returns 503 Service Unavailable when inflight requests reach the threshold
  - When `false`, the ready endpoint always returns 200 OK

**Endpoints:**

- `POST /`: Main handler that simulates processing
  - Returns 200 OK when under threshold or passes intermittent check
  - Returns configured status code (default 500) when overloaded
  - Accepts `X-Sleep` header to override sleep duration per request
- `GET /_/config`: Returns current configuration and inflight request count
- `GET /ready`: Health check endpoint (requires OpenFaaS ready check annotation)
  - Returns 200 OK when ready (inflight < threshold) or when `use_ready_endpoint` is false
  - Returns 503 Service Unavailable when at or above threshold (only when `use_ready_endpoint` is true)

**Example Usage:**

```bash
# Deploy the function
faas-cli deploy -f stack.yaml --filter overload-simulator

# Check current configuration and inflight count
curl http://127.0.0.1:8080/function/overload-simulator/_/config

# Single request (should succeed)
curl http://127.0.0.1:8080/function/overload-simulator

# Simulate overload with parallel requests (some may fail when > 5 concurrent)
for i in {1..10}; do
  curl http://127.0.0.1:8080/function/overload-simulator &
done
wait

# Override sleep duration
curl -H "X-Sleep: 2s" http://127.0.0.1:8080/function/overload-simulator
```

**Example Configuration:**

```yaml
overload-simulator:
  annotations:
    com.openfaas.ready.http.path: "/ready"  # Enable ready check
  environment:
    inflight_threshold: "10"        # Increase threshold to 10 concurrent requests
    failure_mode: "constant"        # Always fail when over threshold
    status_code: "429"              # Return 429 Too Many Requests instead of 500
    sleep_duration: "500ms"         # Slower processing time
    use_ready_endpoint: "true"      # Enable ready endpoint load shedding
```

### variable-inflight

Simulates a function whose maximum inflight request capacity oscillates over time using a sine wave. When inflight requests exceed the current oscillating threshold, new requests are rejected and the function reports itself as not ready via the `/_/ready` endpoint.

**Use Cases:**
- Testing autoscaler behavior under varying capacity
- Simulating degraded service performance that fluctuates over time
- Validating ready-check-based load shedding

**Configuration (Environment Variables):**

- `sleep_duration` (default: `2s`): Base time each request takes to process
- `status_code` (default: `429`): HTTP status code returned when the inflight threshold is exceeded

The oscillation parameters are hardcoded: capacity oscillates between 50 and 100 inflight requests with a 2-minute period.

**Endpoints:**

- `POST /`: Main handler that sleeps for a configurable duration
  - Returns 200 OK when under the current oscillating threshold
  - Returns configured status code (default 429) when threshold is exceeded
  - Accepts `X-Sleep` header to override sleep duration per request
  - Accepts `X-Min-Sleep` and `X-Max-Sleep` headers together for a random sleep duration within the range
- `GET /_/ready`: Readiness endpoint
  - Returns 200 OK when ready
  - Returns 503 Service Unavailable when overloaded

**Example Usage:**

```bash
# Deploy the function
faas-cli deploy -f stack.yaml --filter variable-inflight

# Single request
curl http://127.0.0.1:8080/function/variable-inflight

# Override sleep duration
curl -H "X-Sleep: 5s" http://127.0.0.1:8080/function/variable-inflight

# Random sleep between 1s and 3s
curl -H "X-Min-Sleep: 1s" -H "X-Max-Sleep: 3s" http://127.0.0.1:8080/function/variable-inflight
```

### transient-chaos

Simulates transient failures that succeed after a configurable number of retries. Each unique request (identified by `X-Call-ID` header) will fail a set number of times before succeeding, allowing you to test retry logic end-to-end.

**Use Cases:**
- Testing retry policies and exponential backoff
- Validating that async retry mechanisms work correctly
- Simulating flaky upstream dependencies

**Configuration:**

Configuration is set via the `POST /_/config` endpoint (JSON body):

- `status` (default: `429`): HTTP status code to return on failure
- `failure_count` (default: `2`): Number of times a call ID must fail before succeeding
- `success_code` (default: `200`): HTTP status code to return on success
- `delay` (default: `2s`): Artificial delay added to each request

**Endpoints:**

- `POST /`: Main handler — requires `X-Call-ID` header
  - Returns the configured failure status code for the first N attempts
  - Returns the success status code on attempt N+1
- `GET /_/config`: Returns the current configuration
- `POST /_/config`: Updates the configuration (JSON body)

**Example Usage:**

```bash
# Deploy the function
faas-cli deploy -f stack.yaml --filter transient-chaos

# Check current configuration
curl http://127.0.0.1:8080/function/transient-chaos/_/config

# Update configuration
curl -X POST http://127.0.0.1:8080/function/transient-chaos/_/config \
  -d '{"status": 500, "failure_count": 3, "success_code": 200, "delay": "1s"}'

# First call fails (attempt 1 of 2)
curl -H "X-Call-ID: test-1" http://127.0.0.1:8080/function/transient-chaos

# Second call fails (attempt 2 of 2)
curl -H "X-Call-ID: test-1" http://127.0.0.1:8080/function/transient-chaos

# Third call succeeds
curl -H "X-Call-ID: test-1" http://127.0.0.1:8080/function/transient-chaos
```

### retry-after

Returns a `429 Too Many Requests` response with an optional `Retry-After` header for a configurable number of invocations, then succeeds with `200 OK`. The invocation counter can be reset at runtime, making it possible to repeat scenarios without redeploying.

**Use Cases:**
- Testing `Retry-After` header support in the queue-worker
- Simulating rate-limited APIs that advertise a retry delay
- Validating minimum retry wait clamping behaviour

**Configuration (Environment Variables):**

- `fail_count` (default: `3`): Number of invocations that return 429 before the function starts returning 200
- `retry_after_secs` (default: none): Value of the `Retry-After` header to include on 429 responses. When empty, the header is omitted and the queue-worker falls back to exponential backoff

**Endpoints:**

- `POST /`: Main handler
  - Returns `429 Too Many Requests` for the first `fail_count` invocations, with `Retry-After` set if configured
  - Returns `200 OK` for all subsequent invocations
- `POST /_/reset`: Resets the invocation counter to zero so scenarios can be repeated

**Example Usage:**

```bash
# Deploy the function
faas-cli deploy -f stack.yaml --filter retry-after

# Invoke asynchronously — the queue-worker will retry on 429
curl -i http://127.0.0.1:8080/async-function/retry-after -d ""

# Watch the queue-worker logs
kubectl logs deploy/queue-worker -n openfaas -f

# Reset the counter between test runs
curl -X POST http://127.0.0.1:8080/function/retry-after/_/reset
```

**Example Configuration:**

```yaml
retry-after:
  environment:
    fail_count: "5"           # Fail 5 times before succeeding
    retry_after_secs: "10"    # Advertise a 10-second retry delay
```

### async-load

Generates asynchronous invocation load by invoking a target function via the OpenFaaS gateway with a callback URL pointing back to itself. Each callback triggers another async invocation, creating a continuous loop of async calls. Supports invocation counting with configurable limits.

**Use Cases:**
- Load testing the async invocation pipeline
- Testing callback-based async patterns
- Benchmarking the OpenFaaS queue worker under sustained load

**Configuration (Environment Variables):**

- `OPENFAAS_URL` (default: `http://127.0.0.1:8080`): Gateway URL for invoking functions

**Endpoints:**

- `POST /`: Main handler — requires `X-Function-Name` header in the format `function.namespace`
  - Invokes the target function asynchronously with a callback URL pointing to itself
  - Returns 429 when `max-invocations` limit is reached
- `GET /config?max-invocations=N`: Set the maximum number of invocations per function (default: unlimited)
- `GET /reset`: Reset all invocation counters
- `GET /reset?function=name`: Reset invocation counter for a specific function
- `GET /stop`: Stop processing new invocations
- `GET /start`: Resume processing invocations

**Example Usage:**

```bash
# Deploy the function
faas-cli deploy -f stack.yaml --filter async-load

# Set a max invocation limit
curl "http://127.0.0.1:8080/function/async-load/config?max-invocations=100"

# Trigger async load for a target function
curl -H "X-Function-Name: sleep.openfaas-fn" \
  http://127.0.0.1:8080/function/async-load

# Stop the load loop
curl http://127.0.0.1:8080/function/async-load/stop

# Reset counters
curl http://127.0.0.1:8080/function/async-load/reset
```

### callback-counter

A simple function that counts the number of callback invocations it receives. Each invocation atomically increments a counter and logs the current count.

**Use Cases:**
- Counting async callback invocations
- Verifying that callbacks are being delivered
- Used together with `async-load` to measure throughput

**Endpoints:**

- `POST /`: Increments the callback counter and returns 200 OK

**Example Usage:**

```bash
# Deploy the function
faas-cli deploy -f stack.yaml --filter callback-counter

# Send a callback
curl -X POST http://127.0.0.1:8080/function/callback-counter

# Check the logs for the current count
faas-cli logs callback-counter
```

### ndjson

Streams newline-delimited JSON (NDJSON) events to the client. Emits one JSON object per second for up to 100 events, with each event containing a timestamp, message, and counter.

**Use Cases:**
- Testing NDJSON streaming support through the gateway
- Validating long-running streaming connections
- Testing timeout configurations for streaming responses

**Configuration (Environment Variables):**

The following timeouts should be set high enough to allow the full stream to complete:
- `exec_timeout` (default in stack: `10m`)
- `write_timeout` (default in stack: `10m`)
- `read_timeout` (default in stack: `10m`)

**Endpoints:**

- `POST /`: Streams up to 100 NDJSON events at 1 event per second
  - Content-Type: `application/x-ndjson`
  - Each line is a JSON object: `{"timestamp":"...","message":"...","counter":N}`
  - Stream stops early if the client disconnects

**Example Usage:**

```bash
# Deploy the function
faas-cli deploy -f stack.yaml --filter ndjson

# Stream events (will receive one JSON object per second)
curl http://127.0.0.1:8080/function/ndjson
```

### ping

Simple ping function that runs `ping -c 1 8.8.8.8` using the OpenFaaS classic watchdog. Returns the ping output as the response body.

**Use Cases:**
- Basic connectivity and health testing
- Verifying that the OpenFaaS gateway can invoke functions
- Testing the classic watchdog mode

**Endpoints:**

- `POST /`: Pings `8.8.8.8` once and returns the output

**Example Usage:**

```bash
# Deploy the function
faas-cli deploy -f stack.yaml --filter ping

# Invoke
curl http://127.0.0.1:8080/function/ping
```
