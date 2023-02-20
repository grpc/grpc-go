module google.golang.org/grpc/security/advancedtls/examples

go 1.17

require (
	google.golang.org/grpc v1.52.0
	google.golang.org/grpc/examples v0.0.0-20230111003119-9b73c42daa31
	google.golang.org/grpc/security/advancedtls v0.0.0-20230111003119-9b73c42daa31
)

require (
	github.com/golang/protobuf v1.5.2 // indirect
	golang.org/x/crypto v0.5.0 // indirect
	golang.org/x/net v0.7.0 // indirect
	golang.org/x/sys v0.5.0 // indirect
	golang.org/x/text v0.7.0 // indirect
	google.golang.org/genproto v0.0.0-20230110181048-76db0878b65f // indirect
	google.golang.org/protobuf v1.28.1 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
