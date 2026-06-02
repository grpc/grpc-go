load("//tools/build_defs/go:go_library.bzl", "go_library")
load("//tools/build_defs/go:go_test.bzl", "go_test")

package(
    default_applicable_licenses = ["//third_party/golang/grpc:license"],
)

licenses(["notice"])

go_library(
    name = "session_client",
    srcs = ["session_client.go"],
    visibility = [
        "//net/grpc/testing/interop:__subpackages__",
        "//third_party/golang/grpc:__subpackages__",
    ],
    deps = [
        "//third_party/golang/grpc",
        "//third_party/golang/grpc/credentials/insecure",
    ],
)

go_test(
    name = "session_test",
    srcs = ["session_test.go"],
    library = ":session_client",
    deps = [
        "//third_party/golang/grpc",
        "//third_party/golang/grpc/codes",
        "//third_party/golang/grpc/credentials/insecure",
        "//third_party/golang/grpc/encoding",
        "//third_party/golang/grpc/status",
        "//third_party/golang/grpc/test/bufconn",
    ],
)

