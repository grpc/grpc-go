"""External dependencies for grpc-java."""

def grpc_java_repositories(
    omit_com_google_api_grpc_google_common_protos=False,
    omit_com_google_auth_google_auth_library_credentials=False,
    omit_com_google_code_findbugs_jsr305=False,
    omit_com_google_code_gson=False,
    omit_com_google_errorprone_error_prone_annotations=False,
    omit_com_google_guava=False,
    omit_com_google_protobuf=False,
    omit_com_google_protobuf_java=False,
    omit_com_google_protobuf_nano_protobuf_javanano=False,
    omit_com_google_truth_truth=False,
    omit_com_squareup_okhttp=False,
    omit_com_squareup_okio=False,
    omit_io_netty_buffer=False,
    omit_io_netty_common=False,
    omit_io_netty_transport=False,
    omit_io_netty_codec=False,
    omit_io_netty_codec_socks=False,
    omit_io_netty_codec_http=False,
    omit_io_netty_codec_http2=False,
    omit_io_netty_handler=False,
    omit_io_netty_handler_proxy=False,
    omit_io_netty_resolver=False,
    omit_io_netty_tcnative_boringssl_static=False,
    omit_io_opencensus_api=False,
    omit_io_opencensus_grpc_metrics=False,
    omit_junit_junit=False,
    omit_org_apache_commons_lang3=False):
  """Imports dependencies for grpc-java."""
  if not omit_com_google_api_grpc_google_common_protos:
    com_google_api_grpc_google_common_protos()
  if not omit_com_google_auth_google_auth_library_credentials:
    com_google_auth_google_auth_library_credentials()
  if not omit_com_google_code_findbugs_jsr305:
    com_google_code_findbugs_jsr305()
  if not omit_com_google_code_gson:
    com_google_code_gson()
  if not omit_com_google_errorprone_error_prone_annotations:
    com_google_errorprone_error_prone_annotations()
  if not omit_com_google_guava:
    com_google_guava()
  if not omit_com_google_protobuf:
    com_google_protobuf()
  if omit_com_google_protobuf_java:
    fail("omit_com_google_protobuf_java is no longer supported and must be not be passed to grpc_java_repositories()")
  if not omit_com_google_protobuf_nano_protobuf_javanano:
    com_google_protobuf_nano_protobuf_javanano()
  if not omit_com_google_truth_truth:
    com_google_truth_truth()
  if not omit_com_squareup_okhttp:
    com_squareup_okhttp()
  if not omit_com_squareup_okio:
    com_squareup_okio()
  if not omit_io_netty_buffer:
    io_netty_buffer()
  if not omit_io_netty_common:
    io_netty_common()
  if not omit_io_netty_transport:
    io_netty_transport()
  if not omit_io_netty_codec:
    io_netty_codec()
  if not omit_io_netty_codec_socks:
    io_netty_codec_socks()
  if not omit_io_netty_codec_http:
    io_netty_codec_http()
  if not omit_io_netty_codec_http2:
    io_netty_codec_http2()
  if not omit_io_netty_handler:
    io_netty_handler()
  if not omit_io_netty_handler_proxy:
    io_netty_handler_proxy()
  if not omit_io_netty_resolver:
    io_netty_resolver()
  if not omit_io_netty_tcnative_boringssl_static:
    io_netty_tcnative_boringssl_static()
  if not omit_io_opencensus_api:
    io_opencensus_api()
  if not omit_io_opencensus_grpc_metrics:
    io_opencensus_grpc_metrics()
  if not omit_junit_junit:
    junit_junit()
  if not omit_org_apache_commons_lang3:
    org_apache_commons_lang3()

  native.bind(
    name = "guava",
    actual = "@com_google_guava_guava//jar",
  )
  native.bind(
    name = "gson",
    actual = "@com_google_code_gson_gson//jar",
  )

def com_google_api_grpc_google_common_protos():
  native.maven_jar(
      name = "com_google_api_grpc_proto_google_common_protos",
      artifact = "com.google.api.grpc:proto-google-common-protos:1.0.0",
      sha1 = "86f070507e28b930e50d218ee5b6788ef0dd05e6",
  )

def com_google_auth_google_auth_library_credentials():
  native.maven_jar(
      name = "com_google_auth_google_auth_library_credentials",
      artifact = "com.google.auth:google-auth-library-credentials:0.9.0",
      sha1 = "8e2b181feff6005c9cbc6f5c1c1e2d3ec9138d46",
  )

def com_google_code_findbugs_jsr305():
  native.maven_jar(
      name = "com_google_code_findbugs_jsr305",
      artifact = "com.google.code.findbugs:jsr305:3.0.0",
      sha1 = "5871fb60dc68d67da54a663c3fd636a10a532948",
  )

def com_google_code_gson():
  native.maven_jar(
      name = "com_google_code_gson_gson",
      artifact = "com.google.code.gson:gson:jar:2.7",
      sha1 = "751f548c85fa49f330cecbb1875893f971b33c4e",
  )

def com_google_errorprone_error_prone_annotations():
  native.maven_jar(
      name = "com_google_errorprone_error_prone_annotations",
      artifact = "com.google.errorprone:error_prone_annotations:2.1.2",
      sha1 = "6dcc08f90f678ac33e5ef78c3c752b6f59e63e0c",
  )

def com_google_guava():
  native.maven_jar(
      name = "com_google_guava_guava",
      artifact = "com.google.guava:guava:20.0",
      sha1 = "89507701249388e1ed5ddcf8c41f4ce1be7831ef",
  )

def com_google_protobuf():
  # proto_library rules implicitly depend on @com_google_protobuf//:protoc,
  # which is the proto-compiler.
  # This statement defines the @com_google_protobuf repo.
  native.http_archive(
      name = "com_google_protobuf",
      sha256 = "1f8b9b202e9a4e467ff0b0f25facb1642727cdf5e69092038f15b37c75b99e45",
      strip_prefix = "protobuf-3.5.1",
      urls = ["https://github.com/google/protobuf/archive/v3.5.1.zip"],
  )

def com_google_protobuf_nano_protobuf_javanano():
  native.maven_jar(
      name = "com_google_protobuf_nano_protobuf_javanano",
      artifact = "com.google.protobuf.nano:protobuf-javanano:3.0.0-alpha-5",
      sha1 = "357e60f95cebb87c72151e49ba1f570d899734f8",
  )

def com_google_truth_truth():
  native.maven_jar(
      name = "com_google_truth_truth",
      artifact = "com.google.truth:truth:0.36",
      sha1 = "7485219d2c1d341097a19382c02bde07e69ff5d2",
  )

def com_squareup_okhttp():
  native.maven_jar(
      name = "com_squareup_okhttp_okhttp",
      artifact = "com.squareup.okhttp:okhttp:2.5.0",
      sha1 = "4de2b4ed3445c37ec1720a7d214712e845a24636",
  )

def com_squareup_okio():
  native.maven_jar(
      name = "com_squareup_okio_okio",
      artifact = "com.squareup.okio:okio:1.13.0",
      sha1 = "a9283170b7305c8d92d25aff02a6ab7e45d06cbe",
  )

def io_netty_codec_http2():
  native.maven_jar(
      name = "io_netty_netty_codec_http2",
      artifact = "io.netty:netty-codec-http2:4.1.22.Final",
      sha1 = "6d01daf652551a3219cc07122b765d4c4924dcf8",
  )

def io_netty_buffer():
  native.maven_jar(
      name = "io_netty_netty_buffer",
      artifact = "io.netty:netty-buffer:4.1.22.Final",
      sha1 = "15e964a2095031364f534a6e21977f5ee9ca32a9",
  )

def io_netty_common():
  native.maven_jar(
      name = "io_netty_netty_common",
      artifact = "io.netty:netty-common:4.1.22.Final",
      sha1 = "56ff4deca53fc791ed59ac2b72eb6718714a4de9",
  )

def io_netty_transport():
  native.maven_jar(
      name = "io_netty_netty_transport",
      artifact = "io.netty:netty-transport:4.1.22.Final",
      sha1 = "3bd455cd9e5e5fb2e08fd9cd0acfa54c079ca989",
  )

def io_netty_codec():
  native.maven_jar(
      name = "io_netty_netty_codec",
      artifact = "io.netty:netty-codec:4.1.22.Final",
      sha1 = "239c0af275952e70bb4adf7cf8c03d88ddc394c9",
  )

def io_netty_codec_socks():
  native.maven_jar(
      name = "io_netty_netty_codec_socks",
      artifact = "io.netty:netty-codec-socks:4.1.22.Final",
      sha1 = "d077b39da2dedc5dc5db50a44e5f4c30353e86f3",
  )

def io_netty_codec_http():
  native.maven_jar(
      name = "io_netty_netty_codec_http",
      artifact = "io.netty:netty-codec-http:4.1.22.Final",
      sha1 = "3805f3ca0d57630200defc7f9bb6ed3382dcb10b",
  )

def io_netty_handler():
  native.maven_jar(
      name = "io_netty_netty_handler",
      artifact = "io.netty:netty-handler:4.1.22.Final",
      sha1 = "a3a16b17d5a5ed6f784b0daba95e28d940356109",
  )

def io_netty_handler_proxy():
  native.maven_jar(
      name = "io_netty_netty_handler_proxy",
      artifact = "io.netty:netty-handler-proxy:4.1.22.Final",
      sha1 = "8eabe24f0b8e95d0873964666ad070179ca81e72",
  )

def io_netty_resolver():
  native.maven_jar(
      name = "io_netty_netty_resolver",
      artifact = "io.netty:netty-resolver:4.1.22.Final",
      sha1 = "b5484d17a97cb57b07d2a1ac092c249e47234c17",
  )

def io_netty_tcnative_boringssl_static():
  native.maven_jar(
      name = "io_netty_netty_tcnative_boringssl_static",
      artifact = "io.netty:netty-tcnative-boringssl-static:2.0.7.Final",
      sha1 = "a8ec0f0ee612fa89c709bdd3881c3f79fa00431d",
  )

def io_opencensus_api():
  native.maven_jar(
      name = "io_opencensus_opencensus_api",
      artifact = "io.opencensus:opencensus-api:0.11.0",
      sha1 = "c1ff1f0d737a689d900a3e2113ddc29847188c64",
  )

def io_opencensus_grpc_metrics():
  native.maven_jar(
      name = "io_opencensus_opencensus_contrib_grpc_metrics",
      artifact = "io.opencensus:opencensus-contrib-grpc-metrics:0.11.0",
      sha1 = "d57b877f1a28a613452d45e35c7faae5af585258",
  )

def junit_junit():
  native.maven_jar(
      name = "junit_junit",
      artifact = "junit:junit:4.12",
      sha1 = "2973d150c0dc1fefe998f834810d68f278ea58ec",
  )

def org_apache_commons_lang3():
  native.maven_jar(
    name = "org_apache_commons_commons_lang3",
    artifact = "org.apache.commons:commons-lang3:3.5",
    sha1 = "6c6c702c89bfff3cd9e80b04d668c5e190d588c6"
  )
