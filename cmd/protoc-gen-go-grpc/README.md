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

#### Updating existing code

The previous version of protoc-gen-go-grpc used a different method to register
services.  In that version, it was only possible to register a service
implementation that was a complete implementation of the service.  It was also
possible to embed an `Unimplemented` implementation of the service, which was
also generated and returned an UNIMPLEMENTED status for all methods.  To make
it easier to migrate from the previous version, two new symbols are generated:
`New<Service>Service` and `Unstable<Service>Service`.

`New<Service>Service` allows for easy wrapping of a service implementation into
an instance of the new generated `Service` struct type.  *This has drawbacks,
however: `New<Service>Service` accepts any parameter, and does not panic or
error if any methods are missing, are misspelled, or have the wrong signature.*
These methods will return an UNIMPLEMENTED status if called by a client.

`Unstable<Service>Service` allows for asserting that a type properly implements
all methods of a service.  It is generated with the name `Unstable` to denote
that it will be extended whenever new methods are added to the service
definition.  This kind of change to the service definition is considered
backward-compatible in protobuf, but is not backward-compatible in Go, because
adding methods to an interface invalidates any existing implementations.  *Use
of this symbol could result in future compilation errors.*

To convert your existing code using the previous code generator, please refer
to the following example:

```go
type myEchoService{
     // ...fields used by this service...
}
// ... method handler implementation ...


// Optional; not recommended: to guarantee myEchoService fully implements
// EchoService:
var _ pb.UnstableEchoService = &myEchoService{}

func main() {
     // ...

     // OLD:
     pb.RegisterEchoServer(grpcServer, &myEchoService{})

     // NEW:
     pb.RegisterEchoService(grpcServer, pb.NewEchoService(&myEchoService{}))


     // Optional: to gracefully detect missing methods:
     if _, ok := &myEchoService{}.(pb.UnstableEchoService); !ok {
        fmt.Println("myEchoService does not implement all methods of EchoService.")
     }


     // ...
}
```

#### Updating generated code to support both legacy and new code

`protoc-gen-go-grpc` supports a flag, `migration_mode`, which enables it to be
run in tandem with the previous tool (`protoc-gen-go` with the grpc plugin).
It can be used as follows:

```sh
go install github.com/golang/protobuf/protoc-gen-go

# Example 1: with OPTS set to common options for protoc-gen-go and
# protoc-gen-go-grpc
protoc --go_out=${OPTS},plugins=grpc:. --go-grpc_out=${OPTS},migration_mode=true:. *.proto

# Example 2: if no special options are needed
protoc --go_out=plugins=grpc:. --go-grpc_out=migration_mode=true:. *.proto
```

This is recommended for temporary use only to ease migration from the legacy
version.  The `Register<Service>Server` and `<Service>Server` symbols it
produced are not backward compatible in the presence of newly added methods to
a service.