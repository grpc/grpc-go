module google.golang.org/grpc/security/advancedtls

go 1.25.8

require (
	github.com/google/go-cmp v0.7.0
	golang.org/x/crypto v0.54.0
	google.golang.org/grpc v1.82.0
	google.golang.org/grpc/examples v0.0.0-20250407062114-b368379ef8f6
)

require (
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/spiffe/go-spiffe/v2 v2.8.1 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260713224248-f5fc221cf8c4 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
