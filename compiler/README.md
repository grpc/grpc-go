gRPC Java Codegen Plugin for Protobuf Compiler
==============================================

This generates the Java interfaces out of the service definition from a
`.proto` file. It works with the Protobuf Compiler (``protoc``).

Normally you don't need to compile the codegen by yourself, since pre-compiled
binaries for common platforms are available on Maven Central. However, if the
pre-compiled binaries are not compatible with your system, you may want to
build your own codegen.

## System requirement

* Linux, Mac OS X with Clang, or Windows with MSYS2
* Java 7 or up
* [Protobuf](https://github.com/google/protobuf) 3.0.0-alpha-2 or up

## Compiling and testing the codegen
Change to the `compiler` directory:
```
$ cd $GRPC_JAVA_ROOT/compiler
```

To compile the plugin:
```
$ ../gradlew local_archJava_pluginExecutable
```

To test the plugin with the compiler:
```
$ ../gradlew test
```
You will see a `PASS` if the test succeeds.

To compile a proto file and generate Java interfaces out of the service definitions:
```
$ protoc --plugin=protoc-gen-java_rpc=build/binaries/java_pluginExecutable/local_arch/protoc-gen-grpc-java \
  --java_rpc_out="$OUTPUT_FILE" --proto_path="$DIR_OF_PROTO_FILE" "$PROTO_FILE"
```
To generate Java interfaces with protobuf nano:
```
$ protoc --plugin=protoc-gen-java_rpc=build/binaries/java_pluginExecutable/local_arch/protoc-gen-grpc-java \
  --java_rpc_out=nano=true:"$OUTPUT_FILE" --proto_path="$DIR_OF_PROTO_FILE" "$PROTO_FILE"
```

## Installing the codegen to Maven local repository
This will compile a codegen and put it under your ``~/.m2/repository``. This
will make it available to any build tool that pulls codegens from Maven
repostiories.
```
$ ../gradlew install
```

## Pushing the codegen to Maven Central
This will compile both the 32-bit and 64-bit versions of the codegen and upload
them to Maven Central.

You need to have both the 32-bit and 64-bit versions of Protobuf installed.
It's recommended that you install Protobuf to custom locations (e.g.,
``~/protobuf-3.0.0-32`` and ``~/protobuf-3.0.0-64``) rather than the system
default location, so that different version of Protobuf won't mess up. It
should also be compiled statically linked.

Generally speaking, to compile and install both 32-bit and 64-bit artifacts
locally. You may want to use ``--info`` so that you know what executables are
produced.
```
$ TARGET_ARCHS="<list-of-archs>" ../gradlew install --info
```

To compile and upload the artifacts to Maven Central:
```
$ TARGET_ARCHS="<list-of-archs>" ../gradlew uploadArchives
```

It's recommended that you run ``install --info`` prior to ``uploadArchives``
and examine the produced artifacts before running ``uploadArchives``.

Additional environments need to be set so that the compiler and linker can find
the protobuf headers and libraries. Follow the platform-specific instructions
below.

### Note for using Sonatype OSSRH services

A full fledged release is done by running ``uploadArchives`` on multiple
platforms. After you have successfully run ``uploadArchives`` on the first
platfrom, you should go to OSSRH UI and find the staging repository that has
been automatically created. At the subsequent runs of ``uploadArchives``, you
need to append ``-DrepositoryId=<repository-id-you-just-found>`` to your
command line, so that the artifacts can be merged in a single release.

### Linux
You must have multilib GCC installed on your system.

Compile and install 32-bit protobuf:
```
protobuf$ CXXFLAGS="-m32" ./configure --disable-shared --prefix=$HOME/protobuf-32
protobuf$ make clean && make && make install
```

Compile and install 64-bit protobuf:
```
protobuf$ CXXFLAGS="-m64" ./configure --disable-shared --prefix=$HOME/protobuf-64
protobuf$ make clean && make && make install
```

Compile and deploy GRPC codegen:
```
$ CXXFLAGS="-I$HOME/protobuf-32/include" \
  LDFLAGS="-L$HOME/protobuf-32/lib -L$HOME/protobuf-64/lib" \
  TARGET_ARCHS="x86_32 x86_64" ../gradlew uploadArchives
```

### Windows 64-bit with MSYS2 (Recommended for Windows)
Because the gcc shipped with MSYS2 doesn't support multilib, you have to
compile and deploy 32-bit and 64-bit binaries in separate steps.

#### Under MinGW-w64 Win32 Shell
Compile and install 32-bit protobuf:
```
protobuf$ ./configure --disable-shared --prefix=$HOME/protobuf-32
protobuf$ make clean && make && make install
```

Compile and deploy GRPC 32-bit codegen
```
$ CXXFLAGS="-I$HOME/protobuf-32/include" \
  LDFLAGS="-L$HOME/protobuf-32/lib" \
  TARGET_ARCHS="x86_32" ../gradlew uploadArchives
```

#### Under MinGW-w64 Win64 Shell
Compile and install 64-bit protobuf:
```
protobuf$ ./configure --disable-shared --prefix=$HOME/protobuf-64
protobuf$ make clean && make && make install
```

Compile and deploy GRPC 64-bit codegen:
```
$ CXXFLAGS="-I$HOME/protobuf-64/include" \
  LDFLAGS="-L$HOME/protobuf-64/lib" \
  TARGET_ARCHS="x86_64" ../gradlew uploadArchives
```

### Windows 64-bit with Cygwin64 (TODO: incomplete)
Because the MinGW gcc shipped with Cygwin64 doesn't support multilib, you have
to compile and deploy 32-bit and 64-bit binaries in separate steps.

Compile and install 32-bit protobuf. ``-static-libgcc -static-libstdc++`` are
needed for ``protoc`` to be successfully run in the unit test.
```
protobuf$ LDFLAGS="-static-libgcc -static-libstdc++" ./configure --host=i686-w64-mingw32 --disable-shared --prefix=$HOME/protobuf-32
protobuf$ make clean && make && make install
```

Compile and install 64-bit protobuf:
```
protobuf$ ./configure --host=x86_64-w64-mingw32 --disable-shared --prefix=$HOME/protobuf-64
protobuf$ make clean && make && make install
```

### Mac
Please refer to [Protobuf
README](https://github.com/google/protobuf/blob/master/README.md) for how to
set up GCC and Unix tools on Mac.

Mac OS X has been 64-bit-only since 10.7 and we are compiling for 10.7 and up.
We only build 64-bit artifact for Mac.

Compile and install protobuf:
```
protobuf$ CXXFLAGS="-m64" ./configure --disable-shared --prefix=$HOME/protobuf
protobuf$ make clean && make && make install
```

Compile and deploy GRPC codegen:
```
$ CXXFLAGS="-I$HOME/protobuf/include" \
  LDFLAGS="$HOME/protobuf/lib/libprotobuf.a $HOME/protobuf/lib/libprotoc.a" \
  TARGET_ARCHS="x86_64" ../gradlew uploadArchives
```
