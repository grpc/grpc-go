module google.golang.org/grpc/security/advancedtls/examples

go 1.21

require (
	google.golang.org/grpc v1.64.0
	google.golang.org/grpc/examples v0.0.0-20240606220939-dfcabe08c639
	google.golang.org/grpc/security/advancedtls v1.0.0
)

require (
	golang.org/x/crypto v0.24.0 // indirect
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240604185151-ef581f913117 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
