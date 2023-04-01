## Dialing In gRPC

`grpc.Dial` is a function in the gRPC library that creates a virtual connection
from the gRPC client to the gRPC server.  It takes a server URI (which can
represent the name of a logical backend service and could resolve to multiple
actual addresses) and a list of options, and returns a `ClientConn` object that
represents the connection to the server. The `ClientConn` contains one or more
actual connections to real server backends and attempts to keep these
connections healthy by automatically reconnecting to them (with an exponential
backoff) when they break.

The `grpc.Dial` function can also be configured with various options to
customize the behavior of the client connection. For example, developers could
use options such a `WithTransportCredentials()` to configure the transport
credentials to use.

While `grpc.Dial` is commonly referred to as a "dialing" function, it doesn't
actually perform the low-level network dialing operation like a typical TCP
dialing function would. Instead, it creates a virtual connection from the gRPC
client to the gRPC server.

`grpc.Dial` does initiate the process of connecting to the server, but it uses
the ClientConn object to manage and maintain that connection over time. This is
why errors encountered during the initial connection are no different from those
that occur later on, and why it's important to handle errors from RPCs rather
than relying on options like `FailOnNonTempDialError`, `WithBlock`, and
`WithReturnConnectionError`.

### Using `FailOnNonTempDialError`, `WithBlock`, and `WithReturnConnectionError`

The gRPC API provides several options that can be used to configure the behavior
of dialing and connecting to a gRPC server. Some of these options, such as
`FailOnNonTempDialError`, `WithBlock`, and `WithReturnConnectionError`, rely on
failures at dial time. However, we strongly discourage developers from using
these options, as they can introduce race conditions and result in unreliable
and difficult-to-debug code.

###  Why we discourage using `FailOnNonTempDialError`, `WithBlock`, and `WithReturnConnectionError`

When a client attempts to connect to a gRPC server, it can encounter a variety
of errors, including network connectivity issues, server-side errors, and
incorrect usage of the gRPC API. The options `FailOnNonTempDialError`,
`WithBlock`, and `WithReturnConnectionError` are designed to handle some of
these errors, but they do so by relying on failures at dial time. This means
that they may not provide reliable or accurate information about the status of
the connection.

For example, if a client uses `WithBlock` to wait for a connection to be
established, it may end up waiting indefinitely if the server is not responding.
Similarly, if a client uses `WithReturnConnectionError` to return a connection
error if dialing fails, it may miss opportunities to recover from transient
network issues that are resolved shortly after the initial dial attempt.

## Best practices for error handling in gRPC

Instead of relying on failures at dial time, we strongly encourage developers to
rely on errors from RPCs. When a client makes an RPC, it can receive an error
response from the server. These errors can provide valuable information about
what went wrong, including information about network issues, server-side errors,
and incorrect usage of the gRPC API.

By handling errors from RPCs correctly, developers can write more reliable and
robust gRPC applications. Here are some best practices for error handling in
gRPC:

- Always check for error responses from RPCs and handle them appropriately.
- Use the `status` field of the error response to determine the type of error that
  occurred.
- When retrying failed RPCs, use a backoff strategy to avoid
  overwhelming the server or exacerbating network issues.
- Avoid using `FailOnNonTempDialError`, `WithBlock`, and `WithReturnConnectionError`,
  as these options can introduce race conditions and result in unreliable and
  difficult-to-debug code.

## Example: Handling errors from an RPC

The following code snippet demonstrates how to handle errors from an RPC in
gRPC:

```go 
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()

res, err := client.MyRPC(ctx, &MyRequest{})
if err != nil {
    // Handle the error appropriately,
    // log it & return an error to the caller, etc.
    log.Printf("Error calling MyRPC: %v", err)
    return nil, err
}

// Use the response as appropriate 
log.Printf("MyRPC response: %v", res)

```

To determine the type of error that occurred, you can use the status field of
the error response:


```go 
resp, err := client.MakeRPC(context.Background(), request) 
if err != nil {
  status, ok := status.FromError(err) 
  if ok {
    // Handle the error based on its status code 
    if status.Code() == codes.NotFound {
      log.Println("Requested resource not found")
    } else {
      log.Printf("RPC error: %v", status.Message())
    }
  } else {
    //Handle non-RPC errors 
    log.Printf("Non-RPC error: %v", err)
  }
  return
}        

// Use the response as needed 
log.Printf("Response received: %v", resp) 
```

## Example: Using a backoff strategy


When retrying failed RPCs, use a backoff strategy to avoid overwhelming the
server or exacerbating network issues:


```go 
var res *MyResponse
var err error

// Retry the RPC call a maximum number of times
for i := 0; i < maxRetries; i++ {
    // Create a context with a timeout of one second
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()
    
    // Make the RPC call
    res, err = client.MyRPC(ctx, &MyRequest{})
    
    // Check if the RPC call was successful
    if err == nil {
        // The RPC was successful, so break out of the loop
        break
    }
    
    // The RPC failed, so wait for a backoff period before retrying
    backoff := time.Duration(i) * time.Second
    log.Printf("Error calling MyRPC: %v; retrying in %v", err, backoff)
    time.Sleep(backoff)
}

// Check if the RPC call was successful after all retries
if err != nil {
    // All retries failed, so handle the error appropriately
    log.Printf("Error calling MyRPC: %v", err)
    return nil, err
}

// Use the response as appropriate
log.Printf("MyRPC response: %v", res)

```


## Conclusion

The `FailOnNonTempDialError`, `WithBlock`, and `WithReturnConnectionError`
options are designed to handle errors at dial time, but they can introduce race
conditions and result in unreliable and difficult-to-debug code. Instead of
relying on these options, we strongly encourage developers to rely on errors
from RPCs for error handling. By following best practices for error handling in
gRPC, developers can write more reliable and robust gRPC applications.
