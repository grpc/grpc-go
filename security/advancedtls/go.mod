module google.golang.org/grpc/security/advancedtls

go 1.23.8

toolchain go1.24.2

require (
	github.com/google/go-cmp v0.7.0
	golang.org/x/crypto v0.39.0
	google.golang.org/grpc v1.73.0
	google.golang.org/grpc/examples v0.0.0-20250403095317-51d6a43ec597
)

require (
	github.com/go-jose/go-jose/v4 v4.1.1 // indirect
	github.com/spiffe/go-spiffe/v2 v2.5.0 // indirect
	github.com/zeebo/errs v1.4.0 // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.26.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
