module google.golang.org/grpc/security/advancedtls/examples

go 1.17

require (
	google.golang.org/grpc v1.53.0
	google.golang.org/grpc/examples v0.0.0-20230318005552-70c52915099a
	google.golang.org/grpc/security/advancedtls v0.0.0-20230318005552-70c52915099a
)

require (
	github.com/golang/protobuf v1.5.3 // indirect
	golang.org/x/crypto v0.7.0 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sys v0.6.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	google.golang.org/genproto v0.0.0-20230306155012-7f2fa6fef1f4 // indirect
	google.golang.org/protobuf v1.30.0 // indirect
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
