# RBAC authorization

This example uses the `StaticInterceptor` and `FileWatcherInterceptor` from the
`google.golang.org/grpc/authz` package. It uses a header based RBAC policy to
match each gRPC method to a required role. For simplicity, the context is
injected with mock metadata which includes the required roles, but this should
be fetched from an appropriate service based on the authenticated context.

## Try it

Server is expected to require the following roles on an authenticated user to
authorize usage of these methods:

- `UnaryEcho` requires the role `UNARY_ECHO:W`
- `BidirectionalStreamingEcho` requires the role `STREAM_ECHO:RW`

Upon receiving a request, the server first checks that a token was supplied,
decodes it and checks that a secret is correctly set (hardcoded to
`super-secret` for simplicity, this should use a proper ID provider in
production).

If the above is successful, it uses the username in the token to set appropriate
roles (hardcoded to the 2 required roles above if the username matches
`super-user` for simplicity, these roles should be supplied externally as well).

### Authorization with static policy

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

### Authorization by watching a policy file

The server accepts an optional `--authz-option filewatcher` flag to set up
authorization policy by reading a [policy
file](/examples/data/rbac/policy.json), and to look for update on the policy
file every 100 millisecond. Having `GRPC_GO_LOG_SEVERITY_LEVEL` environment
variable set to `info` will log out the reload activity of the policy every time
a file update is detected.

Start the server with:

```
GRPC_GO_LOG_SEVERITY_LEVEL=info go run server/main.go --authz-option filewatcher
```

Start the client with:

```
go run client/main.go
```

The client will first hit `codes.PermissionDenied` error when invoking
`UnaryEcho` although a legitimate username (`super-user`) is associated with the
RPC. This is because the policy file has an intentional glitch (falsely asks for
role `UNARY_ECHO:RW`).

While the server is still running, edit and save the policy file to replace
`UNARY_ECHO:RW` with the correct role `UNARY_ECHO:W` (policy reload activity
should now be found in server logs). This time when the client is started again
with the command above, it will be able to get responses just as in the
static-policy example.
