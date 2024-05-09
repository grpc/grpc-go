module google.golang.org/grpc/security/advancedtls/examples

go 1.19

require (
	google.golang.org/grpc v1.63.2
	google.golang.org/grpc/examples v0.0.0-20240509214311-59954c801658
	google.golang.org/grpc/security/advancedtls v0.0.0-20240509214311-59954c801658
)

require (
	golang.org/x/crypto v0.23.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240509183442-62759503f434 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
