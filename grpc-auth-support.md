# Authentication

As outlined <a href="https://github.com/grpc/grpc-common/blob/master/grpc-auth-support.md">here</a> gRPC supports a number of different mechanisms for asserting identity between an client and server. We'll present some code-samples here demonstrating how to provide TLS support encryption and identity assertions as well as passing OAuth2 tokens to services that support it.

# Enabling TLS on a gRPC client

```Go
conn, err := grpc.Dial(serverAddr, grpc.WithClientTLS(credentials.NewClientTLSFromCert(nil, ""))
```

# Enableing TLS on a gRPC server

```Go
creds, err := credentials.NewServerTLSFromFile(*certFile, *keyFile)
if err != nil {
  log.Fatalf("Failed to generate credentials %v", err)
}
server.Serve(creds.NewListener(lis))
```

# Using OAuth2


