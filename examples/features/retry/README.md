# Retry

This example shows how to enable and configure retry on gRPC client.

# Read

[Document about this example](https://github.com/grpc/proposal/blob/master/A6-client-retries.md#retry-policy)


# Try it

we had set server service that will always return status code Unavailable until the every fourth Request.To demonstrate retry.

that means client should retry 4 times and 3 times received Unavailable status.

```bash
go run server/main.go
```

set environment otherwise retry will not work.
```bash
GRPC_GO_RETRY=on 
```

we had set retry policy to client to enable per RPC retry at most 4 times when response status is Unavailable.
```bash
go run client/main.go
```

# Usage

### Define your retry policy

In below, we set retry policy for "grpc.example.echo.Echo" methods.

and retry policy :
MaxAttempts: how many times to retry until one of them succeeded.
InitialBackoff and MaxBackoff and BackoffMultiplier: time settings for delaying retries.
RetryableStatusCodes: Retry when received following response codes.

set this as a variable:
```go
        var retryPolicy = `{
            "methodConfig": [{
                // config per method or all methods under service
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
        }`
```

### Use retry policy as DialOption

now you can use `grpc.WithDefaultCallOptions` as `grpc.DialOption` in `grpc.Dial`
to set all clientConnection retry policy

```go
conn, err := grpc.Dial(ctx,grpc.WithInsecure(),grpc.WithDefaultCallOptions(retryPolicy))
```

### Call 

then your gRPC would retry transparent
```go
c := pb.NewEchoClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	reply, err := c.UnaryEcho(ctx, &pb.EchoRequest{Message: "Try and Success"})
	if err != nil {
		log.Println(err)
    }
```