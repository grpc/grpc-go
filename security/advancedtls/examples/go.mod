module google.golang.org/grpc/security/advancedtls/examples

go 1.17

require (
	google.golang.org/grpc v1.54.0
	google.golang.org/grpc/examples v0.0.0-20230418213844-0ed709c4a71d
	google.golang.org/grpc/security/advancedtls v0.0.0-20230418213844-0ed709c4a71d
)

require (
	github.com/golang/protobuf v1.5.3 // indirect
	golang.org/x/crypto v0.8.0 // indirect
	golang.org/x/net v0.9.0 // indirect
	golang.org/x/sys v0.7.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230525234030-28d5490b6b19 // indirect
	google.golang.org/protobuf v1.30.0 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
