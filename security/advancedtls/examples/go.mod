module google.golang.org/grpc/security/advancedtls/examples

go 1.23.0

require (
	google.golang.org/grpc v1.72.2
	google.golang.org/grpc/examples v0.0.0-20250529183914-4275c5bdd8e8
	google.golang.org/grpc/security/advancedtls v1.0.0
)

require (
	github.com/go-jose/go-jose/v4 v4.0.5 // indirect
	github.com/spiffe/go-spiffe/v2 v2.5.0 // indirect
	github.com/zeebo/errs v1.4.0 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250528174236-200df99c418a // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
