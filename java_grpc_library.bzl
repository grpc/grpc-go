# "repository" here is for Bazel builds that span multiple WORKSPACES.
def _path_ignoring_repository(f):
    if len(f.owner.workspace_root) == 0:
        return f.short_path
    return f.path[f.path.find(f.owner.workspace_root) + len(f.owner.workspace_root) + 1:]

def _create_include_path(include):
    return "-I{0}={1}".format(_path_ignoring_repository(include), include.path)

def _java_rpc_library_impl(ctx):
    if len(ctx.attr.srcs) != 1:
        fail("Exactly one src value supported", "srcs")
    if ctx.attr.srcs[0].label.package != ctx.label.package:
        print(("in srcs attribute of {0}: Proto source with label {1} should be in " +
               "same package as consuming rule").format(ctx.label, ctx.attr.srcs[0].label))

    srcs = ctx.attr.srcs[0][ProtoInfo].direct_sources
    includes = ctx.attr.srcs[0][ProtoInfo].transitive_imports
    flavor = ctx.attr.flavor
    if flavor == "normal":
        flavor = ""

    args = ctx.actions.args()
    args.add(ctx.executable._java_plugin.path, format = "--plugin=protoc-gen-grpc-java=%s")
    args.add("--grpc-java_out={0}:{1}".format(flavor, ctx.outputs.srcjar.path))
    args.add_all(includes, map_each = _create_include_path)
    args.add_all(srcs, map_each = _path_ignoring_repository)

    ctx.actions.run(
        inputs = depset(srcs, transitive = [includes]),
        outputs = [ctx.outputs.srcjar],
        tools = [ctx.executable._java_plugin],
        executable = ctx.executable._protoc,
        arguments = [args],
    )

    deps_java_info = java_common.merge([dep[JavaInfo] for dep in ctx.attr.deps])

    java_info = java_common.compile(
        ctx,
        java_toolchain = ctx.attr._java_toolchain,
        host_javabase = ctx.attr._host_javabase,
        source_jars = [ctx.outputs.srcjar],
        output = ctx.outputs.jar,
        deps = [
            java_common.make_non_strict(deps_java_info),
            ctx.attr.runtime[JavaInfo],
        ],
    )
    return [java_info]

_java_rpc_library = rule(
    attrs = {
        "srcs": attr.label_list(
            mandatory = True,
            allow_empty = False,
            providers = [ProtoInfo],
        ),
        "deps": attr.label_list(
            mandatory = True,
            allow_empty = False,
            providers = [JavaInfo],
        ),
        "flavor": attr.string(
            values = [
                "normal",
                "lite",
            ],
            default = "normal",
        ),
        "runtime": attr.label(
            mandatory = True,
        ),
        "_protoc": attr.label(
            default = Label("@com_google_protobuf//:protoc"),
            executable = True,
            cfg = "host",
        ),
        "_java_plugin": attr.label(
            default = Label("//compiler:grpc_java_plugin"),
            executable = True,
            cfg = "host",
        ),
        "_java_toolchain": attr.label(
            default = Label("@bazel_tools//tools/jdk:current_java_toolchain"),
        ),
        "_host_javabase": attr.label(
            cfg = "host",
            default = Label("@bazel_tools//tools/jdk:current_host_java_runtime"),
        ),
    },
    fragments = ["java"],
    outputs = {
        "jar": "lib%{name}.jar",
        "srcjar": "lib%{name}-src.jar",
    },
    provides = [JavaInfo],
    implementation = _java_rpc_library_impl,
)

def java_grpc_library(
        name,
        srcs,
        deps,
        flavor = None,
        tags = None,
        visibility = None,
        **kwargs):
    """Generates and compiles gRPC Java sources for services defined in a proto
    file. This rule is compatible with proto_library with java_api_version,
    java_proto_library, and java_lite_proto_library.

    Do note that this rule only scans through the proto file for RPC services. It
    does not generate Java classes for proto messages. You will need a separate
    proto_library with java_api_version, java_proto_library, or
    java_lite_proto_library rule.

    Args:
      name: (str) A unique name for this rule. Required.
      srcs: (list) a single proto_library target that contains the schema of the
          service. Required.
      deps: (list) a single java_proto_library target for the proto_library in
          srcs.  Required.
      flavor: (str) "normal" (default) for normal proto runtime. "lite"
          for the lite runtime.
      visibility: (list) the visibility list
      **kwargs: Passed through to generated targets
    """

    if len(deps) > 1:
        print("Multiple values in 'deps' is deprecated in " + name)

    if flavor == "lite":
        inner_name = name + "__do_not_reference"
        inner_visibility = ["//visibility:private"]
        inner_tags = ["avoid_dep"]
        runtime = "@io_grpc_grpc_java//:java_lite_grpc_library_deps__do_not_reference"
    else:
        inner_name = name
        inner_visibility = visibility
        inner_tags = tags
        runtime = "@io_grpc_grpc_java//:java_grpc_library_deps__do_not_reference"

    _java_rpc_library(
        name = inner_name,
        srcs = srcs,
        deps = deps,
        flavor = flavor,
        runtime = runtime,
        visibility = inner_visibility,
        tags = inner_tags,
        **kwargs
    )

    if flavor == "lite":
        # Use java_import to work around error with android_binary:
        # Dependencies on .jar artifacts are not allowed in Android binaries,
        # please use a java_import to depend on...
        native.java_import(
            name = name,
            deps = deps + [runtime],
            jars = [":lib{}.jar".format(inner_name)],
            srcjar = ":lib{}-src.jar".format(inner_name),
            visibility = visibility,
            tags = tags,
            **kwargs
        )
