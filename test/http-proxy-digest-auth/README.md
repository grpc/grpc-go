# HTTP Proxy Digest Auth

This directory contains scripts to help testing manually digest authentication
against a http proxy.

## Start the HTTP proxy

```
./build.sh
./run.sh
```

## Run the test

Server:
```
export IP=$(hostname --ip-address)
go run examples/helloworld/greeter_server/main.go -address $IP:50052
```

Client:
```
export IP=$(hostname --ip-address)
export http_proxy='http://admin:secret@127.0.0.1:12345'
go run examples/helloworld/greeter_client/main.go -address $IP:50052
```
