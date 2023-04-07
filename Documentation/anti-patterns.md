## Anti-Patterns

### Dialing in gRPC
[`grpc.Dial`](https://pkg.go.dev/google.golang.org/grpc#Dial) is a function in
the gRPC library that creates a virtual connection from the gRPC client to the
gRPC server.  It takes a target URI (which can represent the name of a logical
backend service and could resolve to multiple actual addresses) and a list of
options, and returns a
[`ClientConn`](https://pkg.go.dev/google.golang.org/grpc#ClientConn) object that
represents the connection to the server. The `ClientConn` contains one or more
actual connections to real server backends and attempts to keep these
connections healthy by automatically reconnecting to them when they break.

The `Dial` function can also be configured with various options to customize the
behavior of the client connection. For example, developers could use options
such a
[`WithTransportCredentials`](https://pkg.go.dev/google.golang.org/grpc#WithTransportCredentials)
to configure the transport credentials to use.

While `Dial` is commonly referred to as a "dialing" function, it doesn't
actually perform the low-level network dialing operation like
[`net.Dial`](https://pkg.go.dev/net#Dial) would.  Instead, it creates a virtual
connection from the gRPC client to the gRPC server.

`Dial` does initiate the process of connecting to the server, but it uses the
ClientConn object to manage and maintain that connection over time. This is why
errors encountered during the initial connection are no different from those
that occur later on, and why it's important to handle errors from RPCs rather
than relying on options like
[`FailOnNonTempDialError`](https://pkg.go.dev/google.golang.org/grpc#FailOnNonTempDialError),
[`WithBlock`](https://pkg.go.dev/google.golang.org/grpc#WithBlock), and
[`WithReturnConnectionError`](https://pkg.go.dev/google.golang.org/grpc#WithReturnConnectionError).
In fact, `Dial` does not always establish a connection to servers by default.
The connection behavior is determined by the load balancing policy being used.
For instance, an "active" load balancing policy such as Round Robin attempts to
maintain a constant connection, while the default "pick first" policy delays
connection until an RPC is executed. Instead of using the WithBlock option, which
may not be recommended in some cases, you can call the
[`ClientConn.Connect`](https://pkg.go.dev/google.golang.org/grpc#ClientConn.Connect)
method to explicitly initiate a connection.

### Using `FailOnNonTempDialError`, `WithBlock`, and `WithReturnConnectionError`

The gRPC API provides several options that can be used to configure the behavior
of dialing and connecting to a gRPC server. Some of these options, such as
`FailOnNonTempDialError`, `WithBlock`, and `WithReturnConnectionError`, rely on
failures at dial time. However, we strongly discourage developers from using
these options, as they can introduce race conditions and result in unreliable
and difficult-to-debug code.

One of the most important reasons for avoiding these options, which is often
overlooked, is that connections can fail at any point in time. This means that
you need to handle RPC failures caused by connection issues, regardless of
whether a connection was never established in the first place, or if it was
created and then immediately lost.  Implementing proper error handling for RPCs
is crucial for maintaining the reliability and stability of your gRPC
communication.

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
- When retrying failed RPCs, consider using the built-in retry mechanism
  provided by gRPC-Go, if available, instead of manually implementing retries.
  Refer to the [gRPC-Go retry example
  documentation](https://github.com/grpc/grpc-go/blob/master/examples/features/retry/README.md)
  for more information.
- Avoid using `FailOnNonTempDialError`, `WithBlock`, and
  `WithReturnConnectionError`, as these options can introduce race conditions and
  result in unreliable and difficult-to-debug code.
- If making the outgoing RPC in order to handle an incoming RPC, be sure to
  translate the status code before returning the error from your method handler.
  For example, if the error is an `INVALID_ARGUMENT` error, that probably means
  your service has a bug (otherwise it shouldn't have triggered this error), in
  which case `INTERNAL` is more appropriate to return back to your users.

### Example: Handling errors from an RPC

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

### Example: Using a backoff strategy


When retrying failed RPCs, use a backoff strategy to avoid overwhelming the
server or exacerbating network issues:


```go 
var res *MyResponse
var err error

// If the user doesn't have a context with a deadline, create one
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()

// Retry the RPC call a maximum number of times
for i := 0; i < maxRetries; i++ {
    
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

The
[`FailOnNonTempDialError`](https://pkg.go.dev/google.golang.org/grpc#FailOnNonTempDialError),
[`WithBlock`](https://pkg.go.dev/google.golang.org/grpc#WithBlock), and
[`WithReturnConnectionError`](https://pkg.go.dev/google.golang.org/grpc#WithReturnConnectionError)
options are designed to handle errors at dial time, but they can introduce race
conditions and result in unreliable and difficult-to-debug code. Instead of
relying on these options, we strongly encourage developers to rely on errors
from RPCs for error handling. By following best practices for error handling in
gRPC, developers can write more reliable and robust gRPC applications.
