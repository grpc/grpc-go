# Setting and Reading Error Details

gRPC servers return a single error value in response to client requests.
The error value contains an error code and a description. In some cases more
details regarding the error are required in order to take action. The recommended
way to specify error details is by attaching metadata in the HTTP/2 trailers
on the server and reading that metadata from the client.

The examples in this directory demonstrate how to set and read error details using
metadata attached in HTTP/2 trailers.

## Background

For this sample, we've already generated the server and client stubs from [helloworld.proto](helloworld/helloworld.proto).

## Install

```
$ go get -u google.golang.org/grpc/examples/error_metadata/greeter_client
$ go get -u google.golang.org/grpc/examples/error_metadata/greeter_server
```

## Try It!

Run the server

```
$ greeter_server &
```

Run the client

```
$ greeter_client
```

```
Greeting: Hello world
```

Run the client again

```
$ greeter_client
```

```
Could not greet: rpc error: code = 8 desc = "Request limit exceeded."
Error details:
  count: 2
  message: Limit one greeting per person.
```
