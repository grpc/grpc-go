`testv3.go` was generated with an older version of codegen, to test reflection
behavior with `grpc.SupportPackageIsVersion3`. DO NOT REGENERATE!

`testv3.go` was then manually edited to replace `"golang.org/x/net/context"`
with `"context"`.

`dynamic.go` was generated with a newer protoc and manually edited to remove
everything except the descriptor bytes var, which is renamed and exported.

`simple_message_v1.go` was generated using protoc-gen-go v1.3.5 which doesn't
support the MesssageV2 API. As a result the generated code implements only the
old MessageV1 API.
