workspace(name = "examples")

# For released versions, use the tagged git-repository:
# git_repository(
#     name = "io_grpc_grpc_java",
#     remote = "https://github.com/grpc/grpc-java.git",
#     tag = "<TAG>",
# )
local_repository(
    name = "io_grpc_grpc_java",
    path = "..",
)

load("@io_grpc_grpc_java//:repositories.bzl", "grpc_java_repositories")

maven_jar(
        name = "com_google_api_grpc_cloud_pubsub_v1",
        artifact = "com.google.api.grpc:grpc-google-cloud-pubsub-v1:0.1.24",
        sha1 = "601d8be0fd0cc0e050b1af3b88f191ada9a2f4e5",
)

maven_jar(
        name = "com_google_api_grpc_proto_cloud_pubsub_v1",
        artifact = "com.google.api.grpc:proto-google-cloud-pubsub-v1:0.1.24",
        sha1 = "e6dd66635f674b4e380dfd3de252ae019a51a67e",
)



grpc_java_repositories()
