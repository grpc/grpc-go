module google.golang.org/grpc/security/advancedtls/examples

go 1.25.0

require (
	google.golang.org/grpc v1.83.0
	google.golang.org/grpc/examples v0.0.0-20260818050025-b6685c50ff98
	google.golang.org/grpc/security/advancedtls v1.0.0
)

require (
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/spiffe/go-spiffe/v2 v2.8.1 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../

replace google.golang.org/grpc/interop => ../../../interop
