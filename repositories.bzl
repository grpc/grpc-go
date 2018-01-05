"""External dependencies for grpc-java."""

def grpc_java_repositories(
    omit_com_google_api_grpc_google_common_protos=False,
    omit_com_google_code_findbugs_jsr305=False,
    omit_com_google_code_gson=False,
    omit_com_google_errorprone_error_prone_annotations=False,
    omit_com_google_guava=False,
    omit_com_google_instrumentation_api=False,
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
    omit_junit_junit=False):
  """Imports dependencies for grpc-java."""
  if not omit_com_google_api_grpc_google_common_protos:
    com_google_api_grpc_google_common_protos()
  if not omit_com_google_code_findbugs_jsr305:
    com_google_code_findbugs_jsr305()
  if not omit_com_google_code_gson:
    com_google_code_gson()
  if not omit_com_google_errorprone_error_prone_annotations:
    com_google_errorprone_error_prone_annotations()
  if not omit_com_google_guava:
    com_google_guava()
  if not omit_com_google_instrumentation_api:
    com_google_instrumentation_api()
  if not omit_com_google_protobuf:
    com_google_protobuf()
  if not omit_com_google_protobuf_java:
    com_google_protobuf_java()
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
      artifact = "com.google.guava:guava:19.0",
      sha1 = "6ce200f6b23222af3d8abb6b6459e6c44f4bb0e9",
  )

def com_google_instrumentation_api():
  native.maven_jar(
      name = "com_google_instrumentation_instrumentation_api",
      artifact = "com.google.instrumentation:instrumentation-api:0.4.3",
      sha1 = "41614af3429573dc02645d541638929d877945a2",
  )

def com_google_protobuf():
  # proto_library rules implicitly depend on @com_google_protobuf//:protoc,
  # which is the proto-compiler.
  # This statement defines the @com_google_protobuf repo.
  native.http_archive(
      name = "com_google_protobuf",
      sha256 = "cef7f1b5a7c5fba672bec2a319246e8feba471f04dcebfe362d55930ee7c1c30",
      strip_prefix = "protobuf-3.5.0",
      urls = ["https://github.com/google/protobuf/archive/v3.5.0.zip"],
  )

def com_google_protobuf_java():
  # java_proto_library rules implicitly depend on @com_google_protobuf_java//:java_toolchain,
  # which is the Java proto runtime (base classes and common utilities).
  native.http_archive(
      name = "com_google_protobuf_java",
      sha256 = "cef7f1b5a7c5fba672bec2a319246e8feba471f04dcebfe362d55930ee7c1c30",
      strip_prefix = "protobuf-3.5.0",
      urls = ["https://github.com/google/protobuf/archive/v3.5.0.zip"],
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
      artifact = "io.netty:netty-codec-http2:4.1.17.Final",
      sha1 = "f9844005869c6d9049f4b677228a89fee4c6eab3",
  )

def io_netty_buffer():
  native.maven_jar(
      name = "io_netty_netty_buffer",
      artifact = "io.netty:netty-buffer:4.1.17.Final",
      sha1 = "fdd68fb3defd7059a7392b9395ee941ef9bacc25",
  )

def io_netty_common():
  native.maven_jar(
      name = "io_netty_netty_common",
      artifact = "io.netty:netty-common:4.1.17.Final",
      sha1 = "581c8ee239e4dc0976c2405d155f475538325098",
  )

def io_netty_transport():
  native.maven_jar(
      name = "io_netty_netty_transport",
      artifact = "io.netty:netty-transport:4.1.17.Final",
      sha1 = "9585776b0a8153182412b5d5366061ff486914c1",
  )

def io_netty_codec():
  native.maven_jar(
      name = "io_netty_netty_codec",
      artifact = "io.netty:netty-codec:4.1.17.Final",
      sha1 = "1d00f56dc9e55203a4bde5aae3d0828fdeb818e7",
  )

def io_netty_codec_socks():
  native.maven_jar(
      name = "io_netty_netty_codec_socks",
      artifact = "io.netty:netty-codec-socks:4.1.17.Final",
      sha1 = "a159bf1f3d5019e0d561c92fbbec8400967471fa",
  )

def io_netty_codec_http():
  native.maven_jar(
      name = "io_netty_netty_codec_http",
      artifact = "io.netty:netty-codec-http:4.1.17.Final",
      sha1 = "251d7edcb897122b9b23f24ff793cd0739056b9e",
  )

def io_netty_handler():
  native.maven_jar(
      name = "io_netty_netty_handler",
      artifact = "io.netty:netty-handler:4.1.17.Final",
      sha1 = "18c40ffb61a1d1979eca024087070762fdc4664a",
  )

def io_netty_handler_proxy():
  native.maven_jar(
      name = "io_netty_netty_handler_proxy",
      artifact = "io.netty:netty-handler-proxy:4.1.17.Final",
      sha1 = "9330ee60c4e48ca60aac89b7bc5ec2567e84f28e",
  )

def io_netty_resolver():
  native.maven_jar(
      name = "io_netty_netty_resolver",
      artifact = "io.netty:netty-resolver:4.1.17.Final",
      sha1 = "8f386c80821e200f542da282ae1d3cde5cad8368",
  )

def io_netty_tcnative_boringssl_static():
  native.maven_jar(
      name = "io_netty_netty_tcnative_boringssl_static",
      artifact = "io.netty:netty-tcnative-boringssl-static:2.0.5.Final",
      sha1 = "321c1239ceb3faec04531ffcdeb1bc8e85408b12",
  )

def io_opencensus_api():
  native.maven_jar(
      name = "io_opencensus_opencensus_api",
      artifact = "io.opencensus:opencensus-api:0.10.0",
      sha1 = "46bcf07e0bd835022ccd531d99c3eb813382d4d8",
  )

def io_opencensus_grpc_metrics():
  native.maven_jar(
      name = "io_opencensus_opencensus_contrib_grpc_metrics",
      artifact = "io.opencensus:opencensus-contrib-grpc-metrics:0.10.0",
      sha1 = "e47f918dc577b6316f57a884c500b13a98d3c11b",
  )

def junit_junit():
  native.maven_jar(
      name = "junit_junit",
      artifact = "junit:junit:4.12",
      sha1 = "2973d150c0dc1fefe998f834810d68f278ea58ec",
  )
