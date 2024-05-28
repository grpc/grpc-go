module google.golang.org/grpc/security/advancedtls

go 1.21

toolchain go1.22.1

require (
	github.com/google/go-cmp v0.6.0
	github.com/hashicorp/golang-lru v0.5.4
	golang.org/x/crypto v0.23.0
	google.golang.org/grpc v1.64.0
	google.golang.org/grpc/examples v0.0.0-20201112215255-90f1b3ee835b
)

require (
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240528155852-a33235495d66 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
