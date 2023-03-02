# RBAC authorization

This example uses the `StaticInterceptor` from the `google.golang.org/grpc/authz`
package. It uses a header based RBAC policy to match each gRPC method to a
required role. For simplicity, the context is injected with mock metadata which
includes the required roles, but this should be fetched from an appropriate
service based on the authenticated context.

## Try it

Server requires the following roles on an authenticated user to authorize usage
of these methods:

- `UnaryEcho` requires the role `UNARY_ECHO:W`
- `BidirectionalStreamingEcho` requires the role `STREAM_ECHO:RW`

Upon receiving a request, the server first checks that a token was supplied,
decodes it and checks that a secret is correctly set (hardcoded to `super-secret`
for simplicity, this should use a proper ID provider in production).

If the above is successful, it uses the username in the token to set appropriate
roles (hardcoded to the 2 required roles above if the username matches `super-user`
for simplicity, these roles should be supplied externally as well).

Start the server with:

```
go run server/main.go
```

The client implementation shows how using a valid token (setting username and
secret) with each of the endpoints will return successfully. It also exemplifies
how using a bad token will result in `codes.PermissionDenied` being returned
from the service.

Start the client with:

```
go run client/main.go
```
