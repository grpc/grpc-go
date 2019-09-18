# Reflection

This example shows how enabling and configuring retry on gRPC client.

# Read

[Document about this example](https://github.com/grpc/proposal/blob/master/A6-client-retries.md#retry-policy)


# Try it

```go
go run server/main.go
```

```go
go run client/main.go
```

# Usage

        {
            "methodConfig": [{
                // config per method or all methods, this is for all methods under grpc.example.echo.Echo 
                // service
                "name": [{"service": "grpc.examples.echo.Echo"}],
                "waitForReady": true,

                "retryPolicy": {
                    "MaxAttempts": 4,
                    "InitialBackoff": ".01s",
                    "MaxBackoff": ".01s",
                    "BackoffMultiplier": 1.0,
                    // this value is grpc code
                    "RetryableStatusCodes": [ "UNAVAILABLE" ]
                }
            }]
        }

# Know all retry policies Integration with Service Config
From [grpc/proposal](https://github.com/grpc/proposal/blob/master/A6-client-retries.md#integration-with-service-config)

The retry policy is transmitted to the client through the service config mechanism. The following is what the JSON configuration file would look like:

        {
        "loadBalancingPolicy": string,

        "methodConfig": [
            {
            "name": [
                {
                "service": string,
                "method": string,
                }
            ],

            // Only one of retryPolicy or hedgingPolicy may be set. If neither is set,
            // RPCs will not be retried or hedged.

            "retryPolicy": {
                // The maximum number of RPC attempts, including the original RPC.
                //
                // This field is required and must be two or greater.
                "maxAttempts": number,

                // Exponential backoff parameters. The initial retry attempt will occur at
                // random(0, initialBackoff). In general, the nth attempt since the last
                // server pushback response (if any), will occur at random(0,
                //   min(initialBackoff*backoffMultiplier**(n-1), maxBackoff)).
                // The following two fields take their form from:
                // https://developers.google.com/protocol-buffers/docs/proto3#json
                // They are representations of the proto3 Duration type. Note that the
                // numeric portion of the string must be a valid JSON number.
                // They both must be greater than zero.
                "initialBackoff": string,  // Required. Long decimal with "s" appended
                "maxBackoff": string,  // Required. Long decimal with "s" appended
                "backoffMultiplier": number  // Required. Must be greater than zero.

                // The set of status codes which may be retried.
                //
                // Status codes are specified in the integer form or the case-insensitive
                // string form (eg. [14], ["UNAVAILABLE"] or ["unavailable"])
                //
                // This field is required and must be non-empty.
                "retryableStatusCodes": []
            }

            "hedgingPolicy": {
                // The hedging policy will send up to maxAttempts RPCs.
                // This number represents the all RPC attempts, including the
                // original and all the hedged RPCs.
                //
                // This field is required and must be two or greater.
                "maxAttempts": number,

                // The original RPC will be sent immediately, but the maxAttempts-1
                // subsequent hedged RPCs will be sent at intervals of every hedgingDelay.
                // Set this to "0s", or leave unset, to immediately send all maxAttempts RPCs.
                // hedgingDelay takes its form from:
                // https://developers.google.com/protocol-buffers/docs/proto3#json
                // It is a representation of the proto3 Duration type. Note that the
                // numeric portion of the string must be a valid JSON number.
                "hedgingDelay": string,

                // The set of status codes which indicate other hedged RPCs may still
                // succeed. If a non-fatal status code is returned by the server, hedged
                // RPCs will continue. Otherwise, outstanding requests will be canceled and
                // the error returned to the client application layer.
                //
                // Status codes are specified in the integer form or the case-insensitive
                // string form (eg. [14], ["UNAVAILABLE"] or ["unavailable"])
                //
                // This field is optional.
                "nonFatalStatusCodes": []
            }

            "waitForReady": bool,
            "timeout": string,
            "maxRequestMessageBytes": number,
            "maxResponseMessageBytes": number
            }
        ]

        // If a RetryThrottlingPolicy is provided, gRPC will automatically throttle
        // retry attempts and hedged RPCs when the client’s ratio of failures to
        // successes exceeds a threshold.
        //
        // For each server name, the gRPC client will maintain a token_count which is
        // initially set to maxTokens, and can take values between 0 and maxTokens.
        //
        // Every outgoing RPC (regardless of service or method invoked) will change
        // token_count as follows:
        //
        //   - Every failed RPC will decrement the token_count by 1.
        //   - Every successful RPC will increment the token_count by tokenRatio.
        //
        // If token_count is less than or equal to maxTokens / 2, then RPCs will not
        // be retried and hedged RPCs will not be sent.
        "retryThrottling": {
            // The number of tokens starts at maxTokens. The token_count will always be
            // between 0 and maxTokens.
            //
            // This field is required and must be in the range (0, 1000].  Up to 3
            // decimal places are supported
            "maxTokens": number,

            // The amount of tokens to add on each successful RPC. Typically this will
            // be some number between 0 and 1, e.g., 0.1.
            //
            // This field is required and must be greater than zero. Up to 3 decimal
            // places are supported.
            "tokenRatio": number
        }
        }