# Mutual TLS

In mutual TLS, the client and the server authenticates each other. gRPC allows
users to configure mutual TLS at the connection level.

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

### Client

In normal TLS, the client is only concerned with authenticating the server by
using a trusted CA file. In mutual TLS, the client also presents the its client
certificate to the server for authentication. This is done via setting
`tls.Config.Certificates`.

### Server

In normal TLS, the server is only concerned with presenting the server
certificate for clients to verify. In mutual TLS, the server also load in a
list of trusted CA files for verifying client presented certificates with.
This is done via setting `tls.Config.RootCAs` to the list of trusted CA files,
and setting `tls.config.ClientAuth` to `tls.RequireAndVerifyClientCert`.
