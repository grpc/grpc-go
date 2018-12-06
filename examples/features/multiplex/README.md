# Multiplex

A `grpc.ClientConn` can be shared by two stubs and two servicers can share a
`grpc.Server`. This example illustrates how to perform both types of sharing.

```
go run server/main.go
```

```
go run client/main.go
```
