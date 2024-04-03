`testv3.go` was generated with an older version of codegen, to test reflection
behavior with `grpc.SupportPackageIsVersion3`. DO NOT REGENERATE!

`testv3.go` was then manually edited to replace `"golang.org/x/net/context"`
with `"context"`.

`dynamic.go` was generated with a newer protoc and manually edited to remove
everything except the descriptor bytes var, which is renamed and exported.
