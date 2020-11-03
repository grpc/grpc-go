module google.golang.org/grpc/security/advancedtls

go 1.14

require (
	github.com/google/go-cmp v0.5.1
	google.golang.org/grpc v1.31.0
	google.golang.org/grpc/examples v0.0.0-20201020200225-9519efffeb5d
)

// TODO(Zhenlian): remove these lines when advancedtls becomes stable.
replace google.golang.org/grpc => ../../

replace google.golang.org/grpc/examples => ../../examples
