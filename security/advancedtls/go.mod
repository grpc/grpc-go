module google.golang.org/grpc/security/advancedtls

go 1.24.0

require (
	github.com/google/go-cmp v0.7.0
	golang.org/x/crypto v0.42.0
	google.golang.org/grpc v1.75.1
	google.golang.org/grpc/examples v0.0.0-20250407062114-b368379ef8f6
)

require (
	github.com/go-jose/go-jose/v4 v4.1.2 // indirect
	github.com/spiffe/go-spiffe/v2 v2.6.0 // indirect
	golang.org/x/net v0.44.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250908214217-97024824d090 // indirect
	google.golang.org/protobuf v1.36.9 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
