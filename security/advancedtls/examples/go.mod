module google.golang.org/grpc/security/advancedtls/examples

go 1.22.0

require (
	google.golang.org/grpc v1.70.0
	google.golang.org/grpc/examples v0.0.0-20250110041721-2d4daf347590
	google.golang.org/grpc/security/advancedtls v1.0.0
)

require (
	golang.org/x/crypto v0.32.0 // indirect
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250115164207-1a7da9e5054f // indirect
	google.golang.org/protobuf v1.36.4 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
