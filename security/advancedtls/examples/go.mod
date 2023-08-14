module google.golang.org/grpc/security/advancedtls/examples

go 1.19

require (
	google.golang.org/grpc v1.56.2
	google.golang.org/grpc/examples v0.0.0-20230714201633-919fe359160c
	google.golang.org/grpc/security/advancedtls v0.0.0-20230714201633-919fe359160c
)

require (
	github.com/golang/protobuf v1.5.3 // indirect
	golang.org/x/crypto v0.11.0 // indirect
	golang.org/x/net v0.12.0 // indirect
	golang.org/x/sys v0.10.0 // indirect
	golang.org/x/text v0.11.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230711160842-782d3b101e98 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
