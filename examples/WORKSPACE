workspace(name = "examples")

# For released versions, use the tagged git-repository:
# git_repository(
#     name = "grpc_java",
#     remote = "https://github.com/grpc/grpc-java.git",
#     tag = "<TAG>",
# )
local_repository(
    name = "grpc_java",
    path = "..",
)

load("@grpc_java//:repositories.bzl", "grpc_java_repositories")

grpc_java_repositories()

