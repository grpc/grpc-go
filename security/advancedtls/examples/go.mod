module google.golang.org/grpc/security/advancedtls/examples

go 1.19

require (
	google.golang.org/grpc v1.58.2
	google.golang.org/grpc/examples v0.0.0-20231005234043-61ee14c70538
	google.golang.org/grpc/security/advancedtls v0.0.0-20231005234043-61ee14c70538
)

require (
	github.com/golang/protobuf v1.5.3 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/net v0.16.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231002182017-d307bd883b97 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
