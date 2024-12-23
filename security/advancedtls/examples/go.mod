module google.golang.org/grpc/security/advancedtls/examples

go 1.22

require (
	google.golang.org/grpc v1.68.1
	google.golang.org/grpc/examples v0.0.0-20241205092301-d7286fbc3f8f
	google.golang.org/grpc/security/advancedtls v1.0.0
)

require (
	golang.org/x/crypto v0.30.0 // indirect
	golang.org/x/net v0.32.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241202173237-19429a94021a // indirect
	google.golang.org/protobuf v1.35.2 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
