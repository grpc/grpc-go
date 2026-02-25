module google.golang.org/grpc/security/advancedtls

go 1.24.0

require (
	github.com/google/go-cmp v0.7.0
	golang.org/x/crypto v0.47.0
	google.golang.org/grpc v1.78.0
	google.golang.org/grpc/examples v0.0.0-20250407062114-b368379ef8f6
)

require (
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	github.com/spiffe/go-spiffe/v2 v2.6.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260120221211-b8f7ae30c516 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
