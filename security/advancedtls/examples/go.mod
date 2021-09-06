module github.com/arshanvit/grpc/security/advancedtls/examples

go 1.15

require (
	github.com/arshanvit/grpc v1.38.0
	github.com/arshanvit/grpc/examples v0.0.0-20201112215255-90f1b3ee835b
	github.com/arshanvit/grpc/security/advancedtls v0.0.0-20201112215255-90f1b3ee835b
)

replace github.com/arshanvit/grpc => ../../..

replace github.com/arshanvit/grpc/examples => ../../../examples

replace github.com/arshanvit/grpc/security/advancedtls => ../
