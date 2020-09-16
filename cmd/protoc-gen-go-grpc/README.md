# protoc-gen-go-grpc

This tool generates Go language bindings of `service`s in protobuf definition
files for gRPC.  For usage information, please see our [quick start
guide](https://grpc.io/docs/languages/go/quickstart/).

## Service implementation and registration

**NOTE:** service registration has changed from the previous version of the
code generator.  Please read this section carefully if you are migrating.

To register your service handlers with a gRPC server, first implement the
methods as either functions or methods on a struct.  Examples:

```go
// As a function:

func unaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
     // Echo.UnaryEcho implementation
}

// As a struct + method:

type myEchoService struct {
     // ...fields used by this service...
}

func (s *myEchoService) UnaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
     // Echo.UnaryEcho implementation
}
```

Then create an instance of the generated `Service` struct type and initialize
the handlers which have been implemented:

```go
func main() {
     // ...

     // As a function:
     echoService := pb.EchoService{
          UnaryEcho: unaryEcho,
          // etc
     }

     // As a struct+method:
     mes := &myEchoService{...}
     echoService := pb.EchoService{
          UnaryEcho: mes.UnaryEcho,
          // etc
     }

     // ...
 }
```

Finally, pass this `Service` instance to the generated `Register` function:

```go
     pb.RegisterEchoService(grpcServer, echoService)
```

### Migration from legacy version

Older versions of `protoc-gen-go-grpc` and `protoc-gen-go` with the grpc plugin
used a different method to register services.  With that method, it was only
possible to register a service implementation that was a complete
implementation of the service.  It was also possible to embed an
`Unimplemented` implementation of the service, which was also generated and
returned an UNIMPLEMENTED status for all methods.

#### Generating the legacy API

To avoid the need to update existing code, an option has been added to the code
generator to produce the legacy API alongside the new API.  To use it:

```sh
# Example 1: with OPTS set to common options for protoc-gen-go and
# protoc-gen-go-grpc
protoc --go_out=${OPTS}:. --go-grpc_out=${OPTS},gen_unstable_server_interfaces=true:. *.proto

# Example 2: if no special options are needed
protoc --go_out=:. --go-grpc_out=gen_unstable_server_interfaces=true:. *.proto
```

**The use of this legacy API is NOT recommended.** It was discontinued as it
results in compilation breakages when new methods are added to services, which
is a backward-compatible change in all other languages supported by gRPC.  With
the newer API, newly-added methods will return an UNIMPLEMENTED status.

#### Updating existing code

To convert your existing code using the previous code generator, please refer
to the following example:

```go
type myEchoService{
     // ...fields used by this service...
}
// ... method handler implementation ...


func main() {
     // ...

     // OLD:
     pb.RegisterEchoServer(grpcServer, &myEchoService{})

     // NEW:
     es := &myEchoService{}
     pb.RegisterEchoService(grpcServer, pb.EchoService{
        UnaryEcho: es.UnaryEcho,
        // enumerate all methods in EchoService implemented by myEchoService...
     })

     // ...
}
```
