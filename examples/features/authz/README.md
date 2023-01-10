# RBAC authorization

This example uses the `StaticInterceptor` from the `google.golang.org/grpc/authz` package.
It uses a header based RBAC policy to match each gRPC method to a required role. For simplicity,
the context is injected with mock metadata which includes the required roles, but this should
be fetched from an appropriate service based on the authenticated context.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```
