# OpenFaaS Test Functions

A collection of utility functions for testing and demonstrating OpenFaaS capabilities.

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
  - `constant`: Returns 500 errors 100% of the time when over threshold
  - `intermittent`: Returns 500 errors 50% of the time when over threshold
- `sleep_duration` (default: `100ms`): Time each request takes to process

**Endpoints:**

- `POST /`: Main handler that simulates processing
  - Returns 200 OK when under threshold or passes intermittent check
  - Returns 500 Internal Server Error when overloaded
  - Accepts `X-Sleep` header to override sleep duration per request
- `GET /_/config`: Returns current configuration and inflight request count

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
  environment:
    inflight_threshold: "10"      # Increase threshold to 10 concurrent requests
    failure_mode: "constant"      # Always fail when over threshold
    sleep_duration: "500ms"       # Slower processing time
```

### variable-inflight

Simulates a function with oscillating inflight request capacity over time.

### transient-chaos

Simulates transient failures that succeed after a configurable number of retries.

### async-load

Function for testing asynchronous invocation patterns.

### callback-counter

Tracks callback invocations for testing async patterns.

### ndjson

Handles newline-delimited JSON streaming.

### ping

Simple ping function for connectivity testing.
