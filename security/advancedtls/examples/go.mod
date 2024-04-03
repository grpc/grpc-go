module google.golang.org/grpc/security/advancedtls/examples

go 1.19

require (
	google.golang.org/grpc v1.62.1
	google.golang.org/grpc/examples v0.0.0-20240320170240-cce163274b6c
	google.golang.org/grpc/security/advancedtls v0.0.0-20240320170240-cce163274b6c
)

require (
	golang.org/x/crypto v0.21.0 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
