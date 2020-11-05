module google.golang.org/grpc/security/advancedtls/examples

go 1.15

require (
	google.golang.org/grpc v1.33.1
	google.golang.org/grpc/examples v0.0.0-20201020200225-9519efffeb5d
	google.golang.org/grpc/security/advancedtls v0.0.0-20201102215344-15ae9fc2b247
)

replace google.golang.org/grpc => ../../..

replace google.golang.org/grpc/examples => ../../../examples

replace google.golang.org/grpc/security/advancedtls => ../
