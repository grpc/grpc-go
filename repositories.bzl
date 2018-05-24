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
    omit_com_google_protobuf_javalite=False,
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
    omit_javax_annotation=False,
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
  if not omit_com_google_protobuf_javalite:
    com_google_protobuf_javalite()
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
  if not omit_javax_annotation:
    javax_annotation()
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

def com_google_protobuf_javalite():
  # java_lite_proto_library rules implicitly depend on @com_google_protobuf_javalite
  native.http_archive(
      name = "com_google_protobuf_javalite",
      sha256 = "d8a2fed3708781196f92e1e7e7e713cf66804bd2944894401057214aff4f468e",
      strip_prefix = "protobuf-5e8916e881c573c5d83980197a6f783c132d4276",
      urls = ["https://github.com/google/protobuf/archive/5e8916e881c573c5d83980197a6f783c132d4276.zip"],
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
      artifact = "io.netty:netty-codec-http2:4.1.25.Final",
      sha1 = "20ffe2d83900da019b69bc557bf211737b322f71",
  )

def io_netty_buffer():
  native.maven_jar(
      name = "io_netty_netty_buffer",
      artifact = "io.netty:netty-buffer:4.1.25.Final",
      sha1 = "f366d0cc87b158ca064d27507127e3cc4eb2f089",
  )

def io_netty_common():
  native.maven_jar(
      name = "io_netty_netty_common",
      artifact = "io.netty:netty-common:4.1.25.Final",
      sha1 = "e17d5c05c101fe14536ce3fb34b36c54e04791f6",
  )

def io_netty_transport():
  native.maven_jar(
      name = "io_netty_netty_transport",
      artifact = "io.netty:netty-transport:4.1.25.Final",
      sha1 = "19a6f1f649894b6705aa9d8cbcced188dff133b0",
  )

def io_netty_codec():
  native.maven_jar(
      name = "io_netty_netty_codec",
      artifact = "io.netty:netty-codec:4.1.25.Final",
      sha1 = "3e465c75bead40d06b5b9c0612b37cf77c548887",
  )

def io_netty_codec_socks():
  native.maven_jar(
      name = "io_netty_netty_codec_socks",
      artifact = "io.netty:netty-codec-socks:4.1.25.Final",
      sha1 = "81d672c2823d83825b4839673828bcf20fd53e2c",
  )

def io_netty_codec_http():
  native.maven_jar(
      name = "io_netty_netty_codec_http",
      artifact = "io.netty:netty-codec-http:4.1.25.Final",
      sha1 = "70888d3f2a829541378f68503ddd52c3193df35a",
  )

def io_netty_handler():
  native.maven_jar(
      name = "io_netty_netty_handler",
      artifact = "io.netty:netty-handler:4.1.25.Final",
      sha1 = "ecdfb8fe93a8b75db3ea8746d3437eed845c24bd",
  )

def io_netty_handler_proxy():
  native.maven_jar(
      name = "io_netty_netty_handler_proxy",
      artifact = "io.netty:netty-handler-proxy:4.1.25.Final",
      sha1 = "be053c1e9f4ac5463883aa7b692798ac7841359a",
  )

def io_netty_resolver():
  native.maven_jar(
      name = "io_netty_netty_resolver",
      artifact = "io.netty:netty-resolver:4.1.25.Final",
      sha1 = "dc0965d00746b782b33f419b005cbc130973030d",
  )

def io_netty_tcnative_boringssl_static():
  native.maven_jar(
      name = "io_netty_netty_tcnative_boringssl_static",
      artifact = "io.netty:netty-tcnative-boringssl-static:2.0.8.Final",
      sha1 = "5c3483dfa33cd04f5469c95abf67e1b69a8f1221",
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

def javax_annotation():
  # TODO(davido): maven_jar does not support neverlink attribute.
  # To circumvent use http_file following by java_import.
  native.http_file(
      name = "javax_annotation_api",
      sha256 = "5909b396ca3a2be10d0eea32c74ef78d816e1b4ead21de1d78de1f890d033e04",
      urls = ["http://central.maven.org/maven2/javax/annotation/javax.annotation-api/1.2/javax.annotation-api-1.2.jar"],
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
