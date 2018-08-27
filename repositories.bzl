"""External dependencies for grpc-java."""

def grpc_java_repositories(
        omit_com_google_api_grpc_google_common_protos = False,
        omit_com_google_auth_google_auth_library_credentials = False,
        omit_com_google_auth_google_auth_library_oauth2_http = False,
        omit_com_google_code_findbugs_jsr305 = False,
        omit_com_google_code_gson = False,
        omit_com_google_errorprone_error_prone_annotations = False,
        omit_com_google_guava = False,
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
        artifact = "com.google.api.grpc:proto-google-common-protos:1.0.0",
        sha1 = "86f070507e28b930e50d218ee5b6788ef0dd05e6",
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
        artifact = "com.google.errorprone:error_prone_annotations:2.2.0",
        sha1 = "88e3c593e9b3586e1c6177f89267da6fc6986f0c",
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

def com_google_re2j():
    native.maven_jar(
        name = "com_google_re2j",
        artifact = "com.google.re2j:re2j:1.2",
        sha1 = "499d5e041f962fefd0f245a9325e8125608ebb54",
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

def io_netty_codec_http2():
    native.maven_jar(
        name = "io_netty_netty_codec_http2",
        artifact = "io.netty:netty-codec-http2:4.1.27.Final",
        sha1 = "3769790a2033667d663f9a526d5b63cfecdbdf4e",
    )

def io_netty_buffer():
    native.maven_jar(
        name = "io_netty_netty_buffer",
        artifact = "io.netty:netty-buffer:4.1.27.Final",
        sha1 = "aafe2b9fb0d8f3b200cf10b9fd6486c6a722d7a1",
    )

def io_netty_common():
    native.maven_jar(
        name = "io_netty_netty_common",
        artifact = "io.netty:netty-common:4.1.27.Final",
        sha1 = "6a12a969c27fb37b230c4bde5a67bd822fa6b7a4",
    )

def io_netty_transport():
    native.maven_jar(
        name = "io_netty_netty_transport",
        artifact = "io.netty:netty-transport:4.1.27.Final",
        sha1 = "b5c2da3ea89dd67320925f1504c9eb3615241b7c",
    )

def io_netty_codec():
    native.maven_jar(
        name = "io_netty_netty_codec",
        artifact = "io.netty:netty-codec:4.1.27.Final",
        sha1 = "d2653d78ebaa650064768fb26b10051f5c8efb2c",
    )

def io_netty_codec_socks():
    native.maven_jar(
        name = "io_netty_netty_codec_socks",
        artifact = "io.netty:netty-codec-socks:4.1.27.Final",
        sha1 = "285b09af98764cf02e4b77b3d95af188469a7133",
    )

def io_netty_codec_http():
    native.maven_jar(
        name = "io_netty_netty_codec_http",
        artifact = "io.netty:netty-codec-http:4.1.27.Final",
        sha1 = "a1722d6bcbbef1c4c7877e8bf38b07a3db5ed07f",
    )

def io_netty_handler():
    native.maven_jar(
        name = "io_netty_netty_handler",
        artifact = "io.netty:netty-handler:4.1.27.Final",
        sha1 = "21bd9cf565390a8d72579b8664303e3c175dfc6a",
    )

def io_netty_handler_proxy():
    native.maven_jar(
        name = "io_netty_netty_handler_proxy",
        artifact = "io.netty:netty-handler-proxy:4.1.27.Final",
        sha1 = "1a822ce7760bc6eb4937b7e448c9e081fedcc807",
    )

def io_netty_resolver():
    native.maven_jar(
        name = "io_netty_netty_resolver",
        artifact = "io.netty:netty-resolver:4.1.27.Final",
        sha1 = "2536447ef9605ccb2b5203aa22392c6514484ea9",
    )

def io_netty_tcnative_boringssl_static():
    native.maven_jar(
        name = "io_netty_netty_tcnative_boringssl_static",
        artifact = "io.netty:netty-tcnative-boringssl-static:2.0.12.Final",
        sha1 = "b884be1450a7fd0854b98743836b8ccb0dfd75a4",
    )

def io_opencensus_api():
    native.maven_jar(
        name = "io_opencensus_opencensus_api",
        artifact = "io.opencensus:opencensus-api:0.12.3",
        sha1 = "743f074095f29aa985517299545e72cc99c87de0",
    )

def io_opencensus_grpc_metrics():
    native.maven_jar(
        name = "io_opencensus_opencensus_contrib_grpc_metrics",
        artifact = "io.opencensus:opencensus-contrib-grpc-metrics:0.12.3",
        sha1 = "a4c7ff238a91b901c8b459889b6d0d7a9d889b4d",
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
