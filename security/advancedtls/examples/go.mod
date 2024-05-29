module google.golang.org/grpc/security/advancedtls/examples

go 1.21

toolchain go1.22.1

require (
	google.golang.org/grpc v1.64.0
	google.golang.org/grpc/examples v0.0.0-20240528152018-6e59dd1d7f86
	google.golang.org/grpc/security/advancedtls v0.0.0-20240528152018-6e59dd1d7f86
)

require (
	golang.org/x/crypto v0.23.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240528155852-a33235495d66 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
