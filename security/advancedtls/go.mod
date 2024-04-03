module google.golang.org/grpc/security/advancedtls

go 1.19

require (
	github.com/google/go-cmp v0.6.0
	github.com/hashicorp/golang-lru v0.5.4
	golang.org/x/crypto v0.21.0
	google.golang.org/grpc v1.62.0
	google.golang.org/grpc/examples v0.0.0-20201112215255-90f1b3ee835b
)

require (
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240227224415-6ceb2ff114de // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)

replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
