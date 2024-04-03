module google.golang.org/grpc/security/advancedtls/examples

go 1.19

require (
	google.golang.org/grpc v1.62.0
	google.golang.org/grpc/examples v0.0.0-20240228175848-51f9cc0f35ae
	google.golang.org/grpc/security/advancedtls v0.0.0-20240228175848-51f9cc0f35ae
)

require (
	golang.org/x/crypto v0.21.0 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240227224415-6ceb2ff114de // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
