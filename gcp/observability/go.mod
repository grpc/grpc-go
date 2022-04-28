module google.golang.org/grpc/gcp/observability

go 1.14

require (
	cloud.google.com/go/logging v1.4.2
	github.com/google/uuid v1.3.0
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8
	google.golang.org/grpc v1.46.0
	google.golang.org/protobuf v1.28.0
)

// TODO(lidiz) remove the following line when we have a release containing the
// necessary internal binary logging changes
replace google.golang.org/grpc => ../../
