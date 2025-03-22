module google.golang.org/grpc/security/advancedtls

go 1.23.0

toolchain go1.24.1

require (
	github.com/google/go-cmp v0.7.0
	golang.org/x/crypto v0.36.0
	google.golang.org/grpc v1.70.0
	google.golang.org/grpc/examples v0.0.0-20230224211313-3775f633ce20
)

require (
	golang.org/x/net v0.37.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250313205543-e70fdf4c4cb4 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
