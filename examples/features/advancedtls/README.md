# gRPC Advanced Security Examples
This repo contains example code for different security configurations for grpc-go using `advancedtls`.

The servers run a basic echo server with the following setups:
* Port 8885: A server with a good certificate using certificate providers and crl providers.
* Port 8884: A server with a revoked certificate using certificate providers and crl providers.
* Port 8883: A server running using InsecureCredentials.

The clients are designed to call these servers with varying configurations of credentials and revocation configurations.
* mTLS with certificate providers and CRLs
* mTLS with custom verification
* mTLS with credentials from credentials.NewTLS (directly using the tls.Config)
* Insecure Credentials

## Generate the credentials used in the examples
Run `./generate.sh` from `/path/to/grpc-go/examples/features/advancedtls` to generate the `creds` directory containing the certificates and CRLs needed for these examples.

## Building and Running
```
# Run the server
$ go run server/main.go -credentials_directory $(pwd)/creds
# Run the clients from the `grpc-go/examples/features/advancedtls` directory 
$ go run client/main.go -credentials_directory $(pwd)/creds
```

Stop the servers with ctrl-c or by killing the process.