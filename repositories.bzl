"""External dependencies for grpc-java."""

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

def grpc_java_repositories(
        omit_com_google_api_grpc_google_common_protos = False,
        omit_com_google_auth_google_auth_library_credentials = False,
        omit_com_google_auth_google_auth_library_oauth2_http = False,
        omit_com_google_code_findbugs_jsr305 = False,
        omit_com_google_code_gson = False,
        omit_com_google_errorprone_error_prone_annotations = False,
        omit_com_google_guava = False,
        omit_com_google_j2objc_j2objc_annotations = False,
        omit_com_google_protobuf = False,
        omit_com_google_protobuf_java = False,
        omit_com_google_protobuf_javalite = False,
        omit_com_google_protobuf_nano_protobuf_javanano = False,
        omit_com_google_re2j = False,
        omit_com_google_truth_truth = False,
        omit_com_squareup_okhttp = False,
        omit_com_squareup_okio = False,
        omit_io_netty_buffer = False,
        omit_io_netty_common = False,
        omit_io_netty_transport = False,
        omit_io_netty_codec = False,
        omit_io_netty_codec_socks = False,
        omit_io_netty_codec_http = False,
        omit_io_netty_codec_http2 = False,
        omit_io_netty_handler = False,
        omit_io_netty_handler_proxy = False,
        omit_io_netty_resolver = False,
        omit_io_netty_tcnative_boringssl_static = False,
        omit_io_opencensus_api = False,
        omit_io_opencensus_grpc_metrics = False,
        omit_javax_annotation = False,
        omit_junit_junit = False,
        omit_org_apache_commons_lang3 = False,
        omit_org_codehaus_mojo_animal_sniffer_annotations = False):
    """Imports dependencies for grpc-java."""
    if not omit_com_google_api_grpc_google_common_protos:
        com_google_api_grpc_google_common_protos()
    if not omit_com_google_auth_google_auth_library_credentials:
        com_google_auth_google_auth_library_credentials()
    if not omit_com_google_auth_google_auth_library_oauth2_http:
        com_google_auth_google_auth_library_oauth2_http()
    if not omit_com_google_code_findbugs_jsr305:
        com_google_code_findbugs_jsr305()
    if not omit_com_google_code_gson:
        com_google_code_gson()
    if not omit_com_google_errorprone_error_prone_annotations:
        com_google_errorprone_error_prone_annotations()
    if not omit_com_google_guava:
        com_google_guava()
    if not omit_com_google_j2objc_j2objc_annotations:
        com_google_j2objc_j2objc_annotations()
    if not omit_com_google_protobuf:
        com_google_protobuf()
    if omit_com_google_protobuf_java:
        fail("omit_com_google_protobuf_java is no longer supported and must be not be passed to grpc_java_repositories()")
    if not omit_com_google_protobuf_javalite:
        com_google_protobuf_javalite()
    if not omit_com_google_protobuf_nano_protobuf_javanano:
        com_google_protobuf_nano_protobuf_javanano()
    if not omit_com_google_re2j:
        com_google_re2j()
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
    if not omit_org_codehaus_mojo_animal_sniffer_annotations:
        org_codehaus_mojo_animal_sniffer_annotations()

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
        artifact = "com.google.api.grpc:proto-google-common-protos:1.12.0",
        sha1 = "1140cc74df039deb044ed0e320035e674dc13062",
    )

def com_google_auth_google_auth_library_credentials():
    native.maven_jar(
        name = "com_google_auth_google_auth_library_credentials",
        artifact = "com.google.auth:google-auth-library-credentials:0.9.0",
        sha1 = "8e2b181feff6005c9cbc6f5c1c1e2d3ec9138d46",
    )

def com_google_auth_google_auth_library_oauth2_http():
    native.maven_jar(
        name = "com_google_auth_google_auth_library_oauth2_http",
        artifact = "com.google.auth:google-auth-library-oauth2-http:0.9.0",
        sha1 = "04e6152c3aead24148627e84f5651e79698c00d9",
    )

def com_google_code_findbugs_jsr305():
    native.maven_jar(
        name = "com_google_code_findbugs_jsr305",
        artifact = "com.google.code.findbugs:jsr305:3.0.2",
        sha1 = "25ea2e8b0c338a877313bd4672d3fe056ea78f0d",
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
        artifact = "com.google.errorprone:error_prone_annotations:2.2.0",
        sha1 = "88e3c593e9b3586e1c6177f89267da6fc6986f0c",
    )

def com_google_guava():
    native.maven_jar(
        name = "com_google_guava_guava",
        artifact = "com.google.guava:guava:26.0-android",
        sha1 = "ef69663836b339db335fde0df06fb3cd84e3742b",
    )

def com_google_j2objc_j2objc_annotations():
    native.maven_jar(
        name = "com_google_j2objc_j2objc_annotations",
        artifact = "com.google.j2objc:j2objc-annotations:1.1",
        sha1 = "ed28ded51a8b1c6b112568def5f4b455e6809019",
    )

def com_google_protobuf():
    # proto_library rules implicitly depend on @com_google_protobuf//:protoc,
    # which is the proto-compiler.
    # This statement defines the @com_google_protobuf repo.
    http_archive(
        name = "com_google_protobuf",
        sha256 = "1f8b9b202e9a4e467ff0b0f25facb1642727cdf5e69092038f15b37c75b99e45",
        strip_prefix = "protobuf-3.5.1",
        urls = ["https://github.com/google/protobuf/archive/v3.5.1.zip"],
    )

def com_google_protobuf_javalite():
    # java_lite_proto_library rules implicitly depend on @com_google_protobuf_javalite
    http_archive(
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

def com_google_re2j():
    native.maven_jar(
        name = "com_google_re2j",
        artifact = "com.google.re2j:re2j:1.2",
        sha1 = "4361eed4abe6f84d982cbb26749825f285996dd2",
    )

def com_google_truth_truth():
    native.maven_jar(
        name = "com_google_truth_truth",
        artifact = "com.google.truth:truth:0.42",
        sha1 = "b5768f644b114e6cf5c3962c2ebcb072f788dcbb",
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

def io_netty_buffer():
    native.maven_jar(
        name = "io_netty_netty_buffer",
        artifact = "io.netty:netty-buffer:4.1.32.Final",
        sha1 = "046ede57693788181b2cafddc3a5967ed2f621c8",
    )

def io_netty_codec():
    native.maven_jar(
        name = "io_netty_netty_codec",
        artifact = "io.netty:netty-codec:4.1.32.Final",
        sha1 = "8f32bd79c5a16f014a4372ed979dc62b39ede33a",
    )

def io_netty_codec_http():
    native.maven_jar(
        name = "io_netty_netty_codec_http",
        artifact = "io.netty:netty-codec-http:4.1.32.Final",
        sha1 = "0b9218adba7353ad5a75fcb639e4755d64bd6ddf",
    )

def io_netty_codec_http2():
    native.maven_jar(
        name = "io_netty_netty_codec_http2",
        artifact = "io.netty:netty-codec-http2:4.1.32.Final",
        sha1 = "d14eb053a1f96d3330ec48e77d489118d547557a",
    )

def io_netty_codec_socks():
    native.maven_jar(
        name = "io_netty_netty_codec_socks",
        artifact = "io.netty:netty-codec-socks:4.1.32.Final",
        sha1 = "b1e83cb772f842839dbeebd9a1f053da98bf56d2",
    )

def io_netty_common():
    native.maven_jar(
        name = "io_netty_netty_common",
        artifact = "io.netty:netty-common:4.1.32.Final",
        sha1 = "e95de4f762606f492328e180c8ad5438565a5e3b",
    )

def io_netty_handler():
    native.maven_jar(
        name = "io_netty_netty_handler",
        artifact = "io.netty:netty-handler:4.1.32.Final",
        sha1 = "b4e3fa13f219df14a9455cc2111f133374428be0",
    )

def io_netty_handler_proxy():
    native.maven_jar(
        name = "io_netty_netty_handler_proxy",
        artifact = "io.netty:netty-handler-proxy:4.1.32.Final",
        sha1 = "58b621246262127b97a871b88c09374c8c324cb7",
    )

def io_netty_resolver():
    native.maven_jar(
        name = "io_netty_netty_resolver",
        artifact = "io.netty:netty-resolver:4.1.32.Final",
        sha1 = "3e0114715cb125a12db8d982b2208e552a91256d",
    )

def io_netty_tcnative_boringssl_static():
    native.maven_jar(
        name = "io_netty_netty_tcnative_boringssl_static",
        artifact = "io.netty:netty-tcnative-boringssl-static:2.0.20.Final",
        sha1 = "071141fca3e805d9d248cb43e1909cf6a50ad92c",
    )

def io_netty_transport():
    native.maven_jar(
        name = "io_netty_netty_transport",
        artifact = "io.netty:netty-transport:4.1.32.Final",
        sha1 = "d5e5a8ff9c2bc7d91ddccc536a5aca1a4355bd8b",
    )

def io_opencensus_api():
    native.maven_jar(
        name = "io_opencensus_opencensus_api",
        artifact = "io.opencensus:opencensus-api:0.18.0",
        sha1 = "b89a8f8dfd1e1e0d68d83c82a855624814b19a6e",
    )

def io_opencensus_grpc_metrics():
    native.maven_jar(
        name = "io_opencensus_opencensus_contrib_grpc_metrics",
        artifact = "io.opencensus:opencensus-contrib-grpc-metrics:0.18.0",
        sha1 = "8e90fab2930b6a0e67dab48911b9c936470d43dd",
    )

def javax_annotation():
    # Use //stub:javax_annotation for neverlink=1 support.
    native.maven_jar(
        name = "javax_annotation_javax_annotation_api",
        artifact = "javax.annotation:javax.annotation-api:1.2",
        sha1 = "479c1e06db31c432330183f5cae684163f186146",
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
        sha1 = "6c6c702c89bfff3cd9e80b04d668c5e190d588c6",
    )

def org_codehaus_mojo_animal_sniffer_annotations():
    native.maven_jar(
        name = "org_codehaus_mojo_animal_sniffer_annotations",
        artifact = "org.codehaus.mojo:animal-sniffer-annotations:1.17",
        sha1 = "f97ce6decaea32b36101e37979f8b647f00681fb",
    )
