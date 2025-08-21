module google.golang.org/grpc/security/advancedtls

go 1.24.0

require (
	github.com/google/go-cmp v0.7.0
	golang.org/x/crypto v0.40.0
	google.golang.org/grpc v1.74.2
	google.golang.org/grpc/examples v0.0.0-20250403095317-51d6a43ec597
)

require (
	github.com/go-jose/go-jose/v4 v4.1.2 // indirect
	github.com/spiffe/go-spiffe/v2 v2.5.0 // indirect
	github.com/zeebo/errs v1.4.0 // indirect
	golang.org/x/net v0.42.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.27.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250804133106-a7a43d27e69b // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
