module google.golang.org/grpc/security/advancedtls/examples

go 1.24.0

require (
	google.golang.org/grpc v1.77.0
	google.golang.org/grpc/examples v0.0.0-20251211082145-ae4bd1e5489b
	google.golang.org/grpc/security/advancedtls v1.0.0
)

require (
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	github.com/spiffe/go-spiffe/v2 v2.6.0 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
