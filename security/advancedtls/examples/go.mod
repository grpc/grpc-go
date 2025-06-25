module google.golang.org/grpc/security/advancedtls/examples

go 1.24

toolchain go1.24.2

require (
	google.golang.org/grpc v1.73.0
	google.golang.org/grpc/examples v0.0.0-20250625071442-a2d6045916b3
	google.golang.org/grpc/security/advancedtls v1.0.0
)

require (
	github.com/go-jose/go-jose/v4 v4.1.0 // indirect
	github.com/spiffe/go-spiffe/v2 v2.5.0 // indirect
	github.com/zeebo/errs v1.4.0 // indirect
	golang.org/x/crypto v0.39.0 // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.26.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
