# Retry

This example shows how to enable and configure retry on gRPC clients.

## Documentation

[gRFC for client-side retry support](https://github.com/grpc/proposal/blob/master/A6-client-retries.md)

## Try it

This example includes a service implementation that fails requests three times with status
code `Unavailable`, then passes the fourth.  The client is configured to make four retry attempts
when receiving an `Unavailable` status code.

First start the server:

```bash
go run server/main.go
```

Then run the client:

```bash
go run client/main.go
```

## Usage

### Define your retry policy

Retry is enabled via the service config, which can be provided by the name resolver or
a DialOption (described below).  In the below config, we set retry policy for the
"grpc.example.echo.Echo" method.

MaxAttempts: how many times to attempt the RPC before failing.
InitialBackoff, MaxBackoff, BackoffMultiplier: configures delay between attempts.
RetryableStatusCodes: Retry only when receiving these status codes.

```go
        var retryPolicy = `{
            "methodConfig": [{
                // config per method or all methods under service
                "name": [{"service": "grpc.examples.echo.Echo"}],

                "retryPolicy": {
                    "MaxAttempts": 4,
                    "InitialBackoff": ".01s",
                    "MaxBackoff": ".01s",
                    "BackoffMultiplier": 1.0,
                    // this value is grpc code
                    "RetryableStatusCodes": [ "UNAVAILABLE" ]
                }
            }]
        }`
```

### Backoff Logic

The backoff duration is calculated based on the following logic:

1. **Initial Backoff**: The initial delay before the first retry.
2. **Exponential Backoff**: The delay increases exponentially based on the number of retries, using the formula:
   ```go
   backoffDuration := InitialBackoff * time.Duration(math.Pow(BackoffMultiplier, float64(numRetries)))
   ```
3. **Max Backoff**: The calculated backoff duration is capped at `MaxBackoff` to prevent excessively long delays.
4. **Jitter**: A random factor between 0.8 and 1.2 is applied to the backoff duration to avoid thundering herd problems. The final backoff duration is calculated as:
   ```go
   jitter := 0.8 + rand.Float64()*0.4 // Random value between 0.8 and 1.2
   dur = time.Duration(float64(backoffDuration) * jitter)
   ```

This means that the backoff delay may be slightly lower than `InitialBackoff` or slightly higher than `MaxBackoff`, allowing for a more distributed retry behavior across clients.

### Providing the retry policy as a DialOption

To use the above service config, pass it with `grpc.WithDefaultServiceConfig` to
`grpc.NewClient`.

```go
conn, err := grpc.NewClient(ctx,grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(retryPolicy))
```
