module google.golang.org/grpc/security/advancedtls

go 1.24.0

require (
	github.com/google/go-cmp v0.7.0
	golang.org/x/crypto v0.43.0
	google.golang.org/grpc v1.76.0
	google.golang.org/grpc/examples v0.0.0-20250407062114-b368379ef8f6
)

require (
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	github.com/spiffe/go-spiffe/v2 v2.6.0 // indirect
	golang.org/x/net v0.46.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251022142026-3a174f9686a8 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
