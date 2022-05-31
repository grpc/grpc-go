testv3.pb.go is generated with an older version of codegen, to test reflection behavior with `grpc.SupportPackageIsVersion3`. DO NOT REGENERATE!

testv3.pb.go is manually edited to replace `"golang.org/x/net/context"` with `"context"`.

dynamic.pb.go is generated with the latest protoc and manually edited to remove everything except the descriptor bytes var, which is renamed and exported.