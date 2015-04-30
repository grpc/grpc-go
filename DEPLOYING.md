How to Deploy GRPC Java to Maven Central (for Maintainers Only)
===============================================================

Build Environments
------------------
We have succesfully deployed GRPC to Maven Central under the following systems:
- Ubuntu 14.04 64-bit
- Windows 7 64-bit with MSYS2 with mingw32 and mingw64
- Mac OS X 10.9.5

Other systems may also work, but we haven't verified them.

Prerequisites
-------------

### Setup OSSRH and Signing

If you haven't deployed artifacts to Maven Central before, you need to setup
your OSSRH account and signing keys.
- Follow the instructions on [this
  page](http://central.sonatype.org/pages/ossrh-guide.html) to set up an
  account with OSSRH.
- (For release deployment only) Install GnuPG and [generate your key
  pair](https://www.gnupg.org/documentation/howtos.html)
- Put your GnuPG key password and OSSRH account information in
  ``<your-home-directory>/.gradle/gradle.properties``.

```
# You need the signing properties only if you are making release deployment
signing.keyId=<8-character-public-key-id>
signing.password=<key-password>
signing.secretKeyRingFile=<your-home-directory>/.gnupg/secring.gpg

ossrhUsername=<ossrh-username>
ossrhPassword=<ossrh-password>
checkstyle.ignoreFailures=false
```

### Build Protobuf
Protobuf libraries are needed for compiling the GRPC codegen. Despite that you
may have installed Protobuf on your system, you may want to build Protobuf
separately and install it under your personal directory, because

1. The Protobuf version installed on your system may be different from what
   GRPC requires. You may not want to pollute your system installation.
2. We will deploy both 32-bit and 64-bit versions of the codegen, thus require
   both variants of Protobuf libraries. You don't want to mix them in your
   system paths.


#### Linux
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


#### Windows 64-bit with MSYS2 (Recommended for Windows)
Because the gcc shipped with MSYS2 doesn't support multilib, you have to
compile and deploy 32-bit and 64-bit binaries in separate steps.

##### Under MinGW-w64 Win32 Shell
Compile and install 32-bit protobuf:
```
protobuf$ ./configure --disable-shared --prefix=$HOME/protobuf-32
protobuf$ make clean && make && make install
```

##### Under MinGW-w64 Win64 Shell
Compile and install 64-bit protobuf:
```
protobuf$ ./configure --disable-shared --prefix=$HOME/protobuf-64
protobuf$ make clean && make && make install
```


#### Windows 64-bit with Cygwin64 (TODO: incomplete)
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


#### Mac
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

Build and Deploy
----------------
You may want to open a shell dedicated for the following tasks, because you
will set a few environment variables which is only used in deployment.


### Setup Environment Variables

Compilation of the codegen requires additional environment variables to locate
the header files and libraries of Protobuf.

#### Linux
```
$ export CXXFLAGS="-I$HOME/protobuf-32/include" \
  LDFLAGS="-L$HOME/protobuf-32/lib -L$HOME/protobuf-64/lib"
```

#### Windows 64-bit with MSYS2

##### Under MinGW-w64 Win32 Shell

```
$ export CXXFLAGS="-I$HOME/protobuf-32/include" \
  LDFLAGS="-L$HOME/protobuf-32/lib"
```

##### Under MinGW-w64 Win64 Shell
```
$ export CXXFLAGS="-I$HOME/protobuf-64/include" \
  LDFLAGS="-L$HOME/protobuf-64/lib"
```


#### Mac
```
$ export CXXFLAGS="-I$HOME/protobuf/include" \
  LDFLAGS="$HOME/protobuf/lib/libprotobuf.a $HOME/protobuf/lib/libprotoc.a"
```



### Deploy the Entire Project

The following command will build the whole project and upload it to Maven
Central.
```
grpc-java$ ./gradlew clean uploadArchives
```

If the version has the ``SNAPSHOT`` suffix, the artifacts will automatically
go to the snapshot repository. Otherwise it's a release deployment and the
artifacts will go to a freshly created staging repository.


### Deploy GRPC Codegen for Additional Platforms
The previous step will only deploy the codegen artifacts for the OS you run on
it and the architecture of your JVM. For a fully fledged deployment, you will
need to deploy the codegen for all other supported OSes and architectures.

To deploy the codegen for an OS and architecture, you must run the following
commands on that OS and specify the architecture by the flag ``-Darch=<arch>``.

We currently distribute the following OSes and architectures:
- Linux: ``x86_32``, ``x86_64``
- Windows: ``x86_32``, ``x86_64``
- Mac: ``x86_64``

If you are doing a snapshot deployment:
```
grpc-java$ ./gradlew clean grpc-compiler:uploadArchives -Darch=<arch>
```

If you are doing a release deployment:
```
grpc-java$ ./gradlew clean grpc-compiler:uploadArchives -Darch=<arch> \
    -DrepositoryId=<repository-id>
```
where ``<repository-id>`` is the ID of the staging repository that you have
found from the OSSRH UI after the first deployment, usually in the form of
``iogrpc-*``. This makes sure the additional codegen artifacts go to the same
repository as the main project is in.

