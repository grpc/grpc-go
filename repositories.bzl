"""External dependencies for grpc-java."""

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
load("@bazel_tools//tools/build_defs/repo:jvm.bzl", "jvm_maven_import_external")

def grpc_java_repositories(
        omit_bazel_skylib = False,
        omit_com_google_android_annotations = False,
        omit_com_google_api_grpc_google_common_protos = False,
        omit_com_google_auth_google_auth_library_credentials = False,
        omit_com_google_auth_google_auth_library_oauth2_http = False,
        omit_com_google_code_findbugs_jsr305 = False,
        omit_com_google_code_gson = False,
        omit_com_google_errorprone_error_prone_annotations = False,
        omit_com_google_guava = False,
        omit_com_google_guava_failureaccess = False,
        omit_com_google_j2objc_j2objc_annotations = False,
        omit_com_google_protobuf = False,
        omit_com_google_protobuf_java = False,
        omit_com_google_protobuf_javalite = False,
        omit_com_google_protobuf_nano_protobuf_javanano = False,
        omit_com_google_re2j = False,
        omit_com_google_truth_truth = False,
        omit_com_squareup_okhttp = False,
        omit_com_squareup_okio = False,
        omit_io_grpc_grpc_proto = False,
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
        omit_net_zlib = False,
        omit_org_apache_commons_lang3 = False,
        omit_org_codehaus_mojo_animal_sniffer_annotations = False):
    """Imports dependencies for grpc-java."""
    if not omit_bazel_skylib:
        bazel_skylib()
    if not omit_com_google_android_annotations:
        com_google_android_annotations()
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
    if not omit_com_google_guava_failureaccess:
        com_google_guava_failureaccess()
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
    if not omit_io_grpc_grpc_proto:
        io_grpc_grpc_proto()
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
    if not omit_net_zlib:
        net_zlib()
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
    native.bind(
        name = "zlib",
        actual = "@net_zlib//:zlib",
    )
    native.bind(
        name = "error_prone_annotations",
        actual = "@com_google_errorprone_error_prone_annotations//jar",
    )

def bazel_skylib():
    http_archive(
        name = "bazel_skylib",
        sha256 = "bce240a0749dfc52fab20dce400b4d5cf7c28b239d64f8fd1762b3c9470121d8",
        strip_prefix = "bazel-skylib-0.7.0",
        urls = ["https://github.com/bazelbuild/bazel-skylib/archive/0.7.0.zip"],
    )

def com_google_android_annotations():
    jvm_maven_import_external(
        name = "com_google_android_annotations",
        artifact = "com.google.android:annotations:4.1.1.4",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "ba734e1e84c09d615af6a09d33034b4f0442f8772dec120efb376d86a565ae15",
        licenses = ["notice"],  # Apache 2.0
    )

def com_google_api_grpc_google_common_protos():
    jvm_maven_import_external(
        name = "com_google_api_grpc_proto_google_common_protos",
        artifact = "com.google.api.grpc:proto-google-common-protos:1.12.0",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "bd60cd7a423b00fb824c27bdd0293aaf4781be1daba6ed256311103fb4b84108",
        licenses = ["notice"],  # Apache 2.0
    )

def com_google_auth_google_auth_library_credentials():
    jvm_maven_import_external(
        name = "com_google_auth_google_auth_library_credentials",
        artifact = "com.google.auth:google-auth-library-credentials:0.9.0",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "ac9efdd6a930e4df906fa278576fa825d979f74315f2faf5c91fe7e6aabb2788",
        licenses = ["notice"],  # BSD 3-clause
    )

def com_google_auth_google_auth_library_oauth2_http():
    jvm_maven_import_external(
        name = "com_google_auth_google_auth_library_oauth2_http",
        artifact = "com.google.auth:google-auth-library-oauth2-http:0.9.0",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "e55d9722102cc1245c8c43d69acd49d3c9bbfcc1bcf722e971425506b970097e",
        licenses = ["notice"],  # BSD 3-clause
    )

def com_google_code_findbugs_jsr305():
    jvm_maven_import_external(
        name = "com_google_code_findbugs_jsr305",
        artifact = "com.google.code.findbugs:jsr305:3.0.2",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "766ad2a0783f2687962c8ad74ceecc38a28b9f72a2d085ee438b7813e928d0c7",
        licenses = ["notice"],  # Apache 2.0
    )

def com_google_code_gson():
    jvm_maven_import_external(
        name = "com_google_code_gson_gson",
        artifact = "com.google.code.gson:gson:jar:2.7",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "2d43eb5ea9e133d2ee2405cc14f5ee08951b8361302fdd93494a3a997b508d32",
        licenses = ["notice"],  # Apache 2.0
    )

def com_google_errorprone_error_prone_annotations():
    jvm_maven_import_external(
        name = "com_google_errorprone_error_prone_annotations",
        artifact = "com.google.errorprone:error_prone_annotations:2.3.2",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "357cd6cfb067c969226c442451502aee13800a24e950fdfde77bcdb4565a668d",
        licenses = ["notice"],  # Apache 2.0
    )

def com_google_guava():
    jvm_maven_import_external(
        name = "com_google_guava_guava",
        artifact = "com.google.guava:guava:26.0-android",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "1d044ebb866ef08b7d04e998b4260c9b52fab6e6d6b68d207859486bb3686cd5",
        licenses = ["notice"],  # Apache 2.0
    )

def com_google_guava_failureaccess():
    # Not needed until Guava 27.0, but including now to ease upgrading of users. See #5214
    jvm_maven_import_external(
        name = "com_google_guava_failureaccess",
        artifact = "com.google.guava:failureaccess:1.0.1",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "a171ee4c734dd2da837e4b16be9df4661afab72a41adaf31eb84dfdaf936ca26",
        licenses = ["notice"],  # Apache 2.0
    )

def com_google_j2objc_j2objc_annotations():
    jvm_maven_import_external(
        name = "com_google_j2objc_j2objc_annotations",
        artifact = "com.google.j2objc:j2objc-annotations:1.1",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "2994a7eb78f2710bd3d3bfb639b2c94e219cedac0d4d084d516e78c16dddecf6",
        licenses = ["notice"],  # Apache 2.0
    )

def com_google_protobuf():
    # proto_library rules implicitly depend on @com_google_protobuf//:protoc,
    # which is the proto-compiler.
    # This statement defines the @com_google_protobuf repo.
    http_archive(
        name = "com_google_protobuf",
        sha256 = "f976a4cd3f1699b6d20c1e944ca1de6754777918320c719742e1674fcf247b7e",
        strip_prefix = "protobuf-3.7.1",
        urls = ["https://github.com/protocolbuffers/protobuf/archive/v3.7.1.zip"],
    )

def com_google_protobuf_javalite():
    # java_lite_proto_library rules implicitly depend on @com_google_protobuf_javalite
    http_archive(
        name = "com_google_protobuf_javalite",
        sha256 = "79d102c61e2a479a0b7e5fc167bcfaa4832a0c6aad4a75fa7da0480564931bcc",
        strip_prefix = "protobuf-384989534b2246d413dbcd750744faab2607b516",
        urls = ["https://github.com/google/protobuf/archive/384989534b2246d413dbcd750744faab2607b516.zip"],
    )

def com_google_protobuf_nano_protobuf_javanano():
    jvm_maven_import_external(
        name = "com_google_protobuf_nano_protobuf_javanano",
        artifact = "com.google.protobuf.nano:protobuf-javanano:3.0.0-alpha-5",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "6d30f1e667a8952e1c90a0a125f0ce0edf84d6b1d51c91d8555c4fb549e3d7a1",
        licenses = ["notice"],  # BSD 2-clause
    )

def com_google_re2j():
    jvm_maven_import_external(
        name = "com_google_re2j",
        artifact = "com.google.re2j:re2j:1.2",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "e9dc705fd4c570344b54a7146b2e3a819cdc271a29793f4acc1a93b56a388e59",
        licenses = ["notice"],  # Go License
    )

def com_google_truth_truth():
    jvm_maven_import_external(
        name = "com_google_truth_truth",
        artifact = "com.google.truth:truth:0.42",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "dd652bdf0c4427c59848ac0340fd6b6d20c2cbfaa3c569a8366604dbcda5214c",
        licenses = ["notice"],  # Apache 2.0
    )

def com_squareup_okhttp():
    jvm_maven_import_external(
        name = "com_squareup_okhttp_okhttp",
        artifact = "com.squareup.okhttp:okhttp:2.5.0",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "1cc716e29539adcda677949508162796daffedb4794cbf947a6f65e696f0381c",
        licenses = ["notice"],  # Apache 2.0
    )

def com_squareup_okio():
    jvm_maven_import_external(
        name = "com_squareup_okio_okio",
        artifact = "com.squareup.okio:okio:1.13.0",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "734269c3ebc5090e3b23566db558f421f0b4027277c79ad5d176b8ec168bb850",
        licenses = ["notice"],  # Apache 2.0
    )

def io_grpc_grpc_proto():
    http_archive(
        name = "io_grpc_grpc_proto",
        sha256 = "873f3fdec7ed052f899aef83fc897926729713d96d7ccdb2df22843dc702ef3a",
        strip_prefix = "grpc-proto-96ecba6941c67b1da2af598330c60cf9b0336051",
        urls = ["https://github.com/grpc/grpc-proto/archive/96ecba6941c67b1da2af598330c60cf9b0336051.zip"],
    )

def io_netty_buffer():
    jvm_maven_import_external(
        name = "io_netty_netty_buffer",
        artifact = "io.netty:netty-buffer:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "39dfe88df8505fd01fbf9c1dbb6b6fa9b0297e453c3dc4ce039ea578aea2eaa3",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_codec():
    jvm_maven_import_external(
        name = "io_netty_netty_codec",
        artifact = "io.netty:netty-codec:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "52e9eeb3638a8ed0911c72a508c05fa4f9d3391125eae46f287d3a8a0776211d",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_codec_http():
    jvm_maven_import_external(
        name = "io_netty_netty_codec_http",
        artifact = "io.netty:netty-codec-http:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "5df5556ef6b0e7ce7c72a359e4ca774fcdf8d8fe12f0b6332715eaa44cfe41f8",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_codec_http2():
    jvm_maven_import_external(
        name = "io_netty_netty_codec_http2",
        artifact = "io.netty:netty-codec-http2:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "319f66f3ab0d3aac3477febf19c259990ee8c639fc7da8822dfa58e7dab1bdcf",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_codec_socks():
    jvm_maven_import_external(
        name = "io_netty_netty_codec_socks",
        artifact = "io.netty:netty-codec-socks:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "9c4ff58b648193942654db20f172d017441625754b902394f620f04074830346",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_common():
    jvm_maven_import_external(
        name = "io_netty_netty_common",
        artifact = "io.netty:netty-common:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "122931117eacf370b054d0e8a2411efa81de4956a6c3f938b0f0eb915969a425",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_handler():
    jvm_maven_import_external(
        name = "io_netty_netty_handler",
        artifact = "io.netty:netty-handler:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "035616801fe9894ca2490832cf9976536dac740f41e90de1cdd4ba46f04263d1",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_handler_proxy():
    jvm_maven_import_external(
        name = "io_netty_netty_handler_proxy",
        artifact = "io.netty:netty-handler-proxy:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "f506c6acb97b3e0b0795cf9f0971d80bbab7c17086312fa225b98ccc94be6dff",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_resolver():
    jvm_maven_import_external(
        name = "io_netty_netty_resolver",
        artifact = "io.netty:netty-resolver:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "774221ed4c130b532865770b10630bc12d0d400127da617ee0ac8de2a7ac2097",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_tcnative_boringssl_static():
    jvm_maven_import_external(
        name = "io_netty_netty_tcnative_boringssl_static",
        artifact = "io.netty:netty-tcnative-boringssl-static:2.0.22.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "382fef183d2dbb991e2c4ac8c9749673aa90ca1ce3cebf3301533beb664bf92f",
        licenses = ["notice"],  # Apache 2.0
    )

def io_netty_transport():
    jvm_maven_import_external(
        name = "io_netty_netty_transport",
        artifact = "io.netty:netty-transport:4.1.34.Final",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "2b3f7d3a595101def7d411793a675bf2a325964475fd7bdbbe448e908de09445",
        licenses = ["notice"],  # Apache 2.0
    )

def io_opencensus_api():
    jvm_maven_import_external(
        name = "io_opencensus_opencensus_api",
        artifact = "io.opencensus:opencensus-api:0.19.2",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "0e2e5d3f4f6fd296017a00b1cd8fb8e4261331cc0c3b6818c0533b01bf7945dc",
        licenses = ["notice"],  # Apache 2.0
    )

def io_opencensus_grpc_metrics():
    jvm_maven_import_external(
        name = "io_opencensus_opencensus_contrib_grpc_metrics",
        artifact = "io.opencensus:opencensus-contrib-grpc-metrics:0.19.2",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "0e23c03414612c7fbef1fdb347076eb69368e596de768cd4b98e081d92206f15",
        licenses = ["notice"],  # Apache 2.0
    )

def javax_annotation():
    # Use //stub:javax_annotation for neverlink=1 support.
    jvm_maven_import_external(
        name = "javax_annotation_javax_annotation_api",
        artifact = "javax.annotation:javax.annotation-api:1.2",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "5909b396ca3a2be10d0eea32c74ef78d816e1b4ead21de1d78de1f890d033e04",
        licenses = ["reciprocal"],  # CDDL License
    )

def junit_junit():
    jvm_maven_import_external(
        name = "junit_junit",
        artifact = "junit:junit:4.12",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "59721f0805e223d84b90677887d9ff567dc534d7c502ca903c0c2b17f05c116a",
        licenses = ["notice"],  # EPL 1.0
    )

def net_zlib():
    http_archive(
        name = "net_zlib",
        build_file = "@com_google_protobuf//:third_party/zlib.BUILD",
        sha256 = "c3e5e9fdd5004dcb542feda5ee4f0ff0744628baf8ed2dd5d66f8ca1197cb1a1",
        strip_prefix = "zlib-1.2.11",
        urls = ["https://zlib.net/zlib-1.2.11.tar.gz"],
    )

def org_apache_commons_lang3():
    jvm_maven_import_external(
        name = "org_apache_commons_commons_lang3",
        artifact = "org.apache.commons:commons-lang3:3.5",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "8ac96fc686512d777fca85e144f196cd7cfe0c0aec23127229497d1a38ff651c",
        licenses = ["notice"],  # Apache 2.0
    )

def org_codehaus_mojo_animal_sniffer_annotations():
    jvm_maven_import_external(
        name = "org_codehaus_mojo_animal_sniffer_annotations",
        artifact = "org.codehaus.mojo:animal-sniffer-annotations:1.17",
        server_urls = ["http://central.maven.org/maven2"],
        artifact_sha256 = "92654f493ecfec52082e76354f0ebf87648dc3d5cec2e3c3cdb947c016747a53",
        licenses = ["notice"],  # MIT
    )
