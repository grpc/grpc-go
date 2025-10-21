module google.golang.org/grpc/security/advancedtls/examples

go 1.24.0

require (
	google.golang.org/grpc v1.76.0
	google.golang.org/grpc/examples v0.0.0-20251014174115-ae62635cb0e2
	google.golang.org/grpc/security/advancedtls v1.0.0
)

require (
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	github.com/spiffe/go-spiffe/v2 v2.6.0 // indirect
	golang.org/x/crypto v0.43.0 // indirect
	golang.org/x/net v0.46.1-0.20251013234738-63d1a5100f82 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251014184007-4626949a642f // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
