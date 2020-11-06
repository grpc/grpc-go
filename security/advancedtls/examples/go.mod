module google.golang.org/grpc/security/advancedtls/examples

go 1.15

require (
	google.golang.org/grpc v1.33.1
	google.golang.org/grpc/examples v0.0.0-20201106192519-9c2f82d9a79c
	google.golang.org/grpc/security/advancedtls v0.0.0-20201106192519-9c2f82d9a79c
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
