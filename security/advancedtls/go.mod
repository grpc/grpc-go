module google.golang.org/grpc/security/advancedtls

go 1.23

require (
	github.com/google/go-cmp v0.6.0
	golang.org/x/crypto v0.33.0
	google.golang.org/grpc v1.70.0
	google.golang.org/grpc/examples v0.0.0-00010101000000-000000000000
)

require (
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250218202821-56aae31c358a // indirect
	google.golang.org/protobuf v1.36.5 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
