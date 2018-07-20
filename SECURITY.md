# Authentication

gRPC supports a number of different mechanisms for asserting identity between an client and server. This document provides code samples demonstrating how to provide SSL/TLS encryption support and identity assertions in Java, as well as passing OAuth2 tokens to services that support it.

# Transport Security (TLS)

HTTP/2 over TLS mandates the use of [ALPN](https://tools.ietf.org/html/draft-ietf-tls-applayerprotoneg-05) to negotiate the use of the h2 protocol. ALPN is a fairly new standard and (where possible) gRPC also supports protocol negotiation via [NPN](https://tools.ietf.org/html/draft-agl-tls-nextprotoneg-04) for systems that do not yet support ALPN.

On Android, use the [Play Services Provider](#tls-on-android). For non-Android systems, use [OpenSSL](#tls-with-openssl).

## TLS on Android

On Android we recommend the use of the [Play Services Dynamic Security
Provider](https://www.appfoundry.be/blog/2014/11/18/Google-Play-Services-Dynamic-Security-Provider/)
to ensure your application has an up-to-date OpenSSL library with the necessary
ciper-suites and a reliable ALPN implementation. This requires [updating the
security provider at
runtime](https://developer.android.com/training/articles/security-gms-provider.html).

Although ALPN mostly works on newer Android releases (especially since 5.0),
there are bugs and discovered security vulnerabilities that are only fixed by
upgrading the security provider. Thus, we recommend using the Play Service
Dynamic Security Provider for all Android versions.

*Note: The Dynamic Security Provider must be installed **before** creating a gRPC OkHttp channel. gRPC's OkHttpProtocolNegotiator statically initializes the security protocol(s) available to gRPC, which means that changes to the security provider after the first channel is created will not be picked up by gRPC.*

### Bundling Conscrypt

If depending on Play Services is not an option for your app, then you may bundle
[Conscrypt](https://conscrypt.org) with your application. Binaries are available
on [Maven
Central](https://search.maven.org/#search%7Cga%7C1%7Cg%3Aorg.conscrypt%20a%3Aconscrypt-android).

Like the Play Services Dynamic Security Provider, you must still "install"
Conscrypt before use.

```java
import org.conscrypt.Conscrypt;
import java.security.Security;
...

Security.insertProviderAt(Conscrypt.newProvider(), 1);
```

## TLS with OpenSSL

This is currently the recommended approach for using gRPC over TLS (on non-Android systems).

The main benefits of using OpenSSL are:

1. **Speed**: In local testing, we've seen performance improvements of 3x over the JDK. GCM, which is used by the only cipher suite required by the HTTP/2 spec, is 10-500x faster.
2. **Ciphers**: OpenSSL has its own ciphers and is not dependent on the limitations of the JDK. This allows supporting GCM on Java 7.
3. **ALPN to NPN Fallback**: if the remote endpoint doesn't support ALPN.
4. **Version Independence**: does not require using a different library version depending on the JDK update.

Support for OpenSSL is only provided for the Netty transport via [netty-tcnative](https://github.com/netty/netty-tcnative), which is a fork of
[Apache Tomcat's tcnative](http://tomcat.apache.org/native-doc/), a JNI wrapper around OpenSSL.

### OpenSSL: Dynamic vs Static (which to use?)

As of version `1.1.33.Fork14`, netty-tcnative provides two options for usage: statically or dynamically linked. For simplification of initial setup,
we recommend that users first look at `netty-tcnative-boringssl-static`, which is statically linked against BoringSSL and Apache APR. Using this artifact requires no extra installation and guarantees that ALPN and the ciphers required for
HTTP/2 are available. In addition, starting with `1.1.33.Fork16` binaries for
all supported platforms can be included at compile time and the correct binary
for the platform can be selected at runtime.

Production systems, however, may require an easy upgrade path for OpenSSL security patches. In this case, relying on the statically linked artifact also implies waiting for the Netty team
to release the new artifact to Maven Central, which can take some time. A better solution in this case is to use the dynamically linked `netty-tcnative` artifact, which allows the site administrator
to easily upgrade OpenSSL in the standard way (e.g. apt-get) without relying on any new builds from Netty.

### OpenSSL: Statically Linked (netty-tcnative-boringssl-static)

This is the simplest way to configure the Netty transport for OpenSSL. You just need to add the appropriate `netty-tcnative-boringssl-static` artifact to your application's classpath.

Artifacts are available on [Maven Central](http://repo1.maven.org/maven2/io/netty/netty-tcnative-boringssl-static/) for the following platforms:

Maven Classifier | Description
---------------- | -----------
windows-x86_64 | Windows distribution
osx-x86_64 | Mac distribution
linux-x86_64 | Linux distribution

##### Getting netty-tcnative-boringssl-static from Maven

In Maven, you can use the [os-maven-plugin](https://github.com/trustin/os-maven-plugin) to help simplify the dependency.

```xml
<project>
  <dependencies>
    <dependency>
      <groupId>io.netty</groupId>
      <artifactId>netty-tcnative-boringssl-static</artifactId>
      <version>2.0.7.Final</version>
    </dependency>
  </dependencies>
</project>
```

##### Getting netty-tcnative-boringssl-static from Gradle

Gradle you can use the [osdetector-gradle-plugin](https://github.com/google/osdetector-gradle-plugin), which is a wrapper around the os-maven-plugin.

```gradle
buildscript {
  repositories {
    mavenCentral()
  }
}

dependencies {
    compile 'io.netty:netty-tcnative-boringssl-static:2.0.7.Final'
}
```

### OpenSSL: Dynamically Linked (netty-tcnative)

If for any reason you need to dynamically link against OpenSSL (e.g. you need control over the version of OpenSSL), you can instead use the `netty-tcnative` artifact.

Requirements:

1. [OpenSSL](https://www.openssl.org/) version >= 1.0.2 for ALPN support, or version >= 1.0.1 for NPN.
2. [Apache APR library (libapr-1)](https://apr.apache.org/) version >= 1.5.2.
3. [netty-tcnative](https://github.com/netty/netty-tcnative) version >= 1.1.33.Fork7 must be on classpath. Prior versions only supported NPN and only Fedora-derivatives were supported for Linux.

Artifacts are available on [Maven Central](http://repo1.maven.org/maven2/io/netty/netty-tcnative/) for the following platforms:

Classifier | Description
---------------- | -----------
windows-x86_64 | Windows distribution
osx-x86_64 | Mac distribution
linux-x86_64 | Used for non-Fedora derivatives of Linux
linux-x86_64-fedora | Used for Fedora derivatives

On Linux it should be noted that OpenSSL uses a different soname for Fedora derivatives than other Linux releases. To work around this limitation, netty-tcnative deploys two separate versions for linux.

##### Getting netty-tcnative from Maven

In Maven, you can use the [os-maven-plugin](https://github.com/trustin/os-maven-plugin) to help simplify the dependency.

```xml
<project>
  <dependencies>
    <dependency>
      <groupId>io.netty</groupId>
      <artifactId>netty-tcnative</artifactId>
      <version>2.0.7.Final</version>
      <classifier>${tcnative.classifier}</classifier>
    </dependency>
  </dependencies>

  <build>
    <extensions>
      <!-- Use os-maven-plugin to initialize the "os.detected" properties -->
      <extension>
        <groupId>kr.motd.maven</groupId>
        <artifactId>os-maven-plugin</artifactId>
        <version>1.5.0.Final</version>
      </extension>
    </extensions>
    <plugins>
      <!-- Use Ant to configure the appropriate "tcnative.classifier" property -->
      <plugin>
        <groupId>org.apache.maven.plugins</groupId>
        <artifactId>maven-antrun-plugin</artifactId>
        <executions>
          <execution>
            <phase>initialize</phase>
            <configuration>
              <exportAntProperties>true</exportAntProperties>
              <target>
                <condition property="tcnative.classifier"
                           value="${os.detected.classifier}-fedora"
                           else="${os.detected.classifier}">
                  <isset property="os.detected.release.fedora"/>
                </condition>
              </target>
            </configuration>
            <goals>
              <goal>run</goal>
            </goals>
          </execution>
        </executions>
      </plugin>
    </plugins>
  </build>
</project>
```

##### Getting netty-tcnative from Gradle

Gradle you can use the [osdetector-gradle-plugin](https://github.com/google/osdetector-gradle-plugin), which is a wrapper around the os-maven-plugin.

```gradle
buildscript {
  repositories {
    mavenCentral()
  }
  dependencies {
    classpath 'com.google.gradle:osdetector-gradle-plugin:1.4.0'
  }
}

// Use the osdetector-gradle-plugin
apply plugin: "com.google.osdetector"

def tcnative_classifier = osdetector.classifier;
// Fedora variants use a different soname for OpenSSL than other linux distributions
// (see http://netty.io/wiki/forked-tomcat-native.html).
if (osdetector.os == "linux" && osdetector.release.isLike("fedora")) {
  tcnative_classifier += "-fedora";
}

dependencies {
    compile 'io.netty:netty-tcnative:2.0.7.Final:' + tcnative_classifier
}
```

## TLS with JDK (Jetty ALPN/NPN)

**WARNING: DON'T DO THIS!!**

*For non-Android systems, the recommended approach is to use [OpenSSL](#tls-with-openssl). Using the JDK for ALPN is generally much slower and may not support the necessary ciphers for HTTP2.*

*Jetty ALPN brings its own baggage in that the Java bootclasspath needs to be modified, which may not be an option for some environments. In addition, a specific version of Jetty ALPN has to be used for a given version of the JRE. If the versions don't match the negotiation will fail, but you won't really know why. And since there is such a tight coupling between Jetty ALPN and the JRE, there are no guarantees that Jetty ALPN will support every JRE out in the wild.*

*The moral of the story is: Don't use the JDK for ALPN!  But if you absolutely have to, here's how you do it... :)*

---

If not using the Netty transport (or you are unable to use OpenSSL for some reason) another alternative is to use the JDK for TLS.

No standard Java release has built-in support for ALPN today ([there is a tracking issue](https://bugs.openjdk.java.net/browse/JDK-8051498) so go upvote it!) so we need to use the [Jetty-ALPN](https://github.com/jetty-project/jetty-alpn) (or [Jetty-NPN](https://github.com/jetty-project/jetty-npn) if on Java < 8) bootclasspath extension for OpenJDK. To do this, add an `Xbootclasspath` JVM option referencing the path to the Jetty `alpn-boot` jar.

```sh
java -Xbootclasspath/p:/path/to/jetty/alpn/extension.jar ...
```

Note that you must use the [release of the Jetty-ALPN jar](http://www.eclipse.org/jetty/documentation/current/alpn-chapter.html#alpn-versions) specific to the version of Java you are using. However, you can use the JVM agent [Jetty-ALPN-Agent](https://github.com/jetty-project/jetty-alpn-agent) to load the correct Jetty `alpn-boot` jar file for the current Java version. To do this, instead of adding an `Xbootclasspath` option, add a `javaagent` JVM option referencing the path to the Jetty `alpn-agent` jar.

```sh
java -javaagent:/path/to/jetty-alpn-agent.jar ...
```

### JDK Ciphers

Java 7 does not support [the cipher suites recommended](https://tools.ietf.org/html/draft-ietf-httpbis-http2-17#section-9.2.2) by the HTTP2 specification. To address this we suggest servers use Java 8 where possible or use an alternative JCE implementation such as [Bouncy Castle](https://www.bouncycastle.org/java.html). If this is not practical it is possible to use other ciphers but you need to ensure that the services you intend to call have [allowed out-of-spec ciphers](https://github.com/grpc/grpc/issues/681) and have evaluated the security risks of doing so.

Users should be aware that GCM is [_very_ slow (1 MB/s)](https://bugzilla.redhat.com/show_bug.cgi?id=1135504) before Java 8u60. With Java 8u60 GCM is 10x faster (10-20 MB/s), but that is still slow compared to OpenSSL (~200 MB/s), especially with AES-NI support (~1 GB/s). GCM cipher suites are the only suites available that comply with HTTP2's cipher requirements.

### Configuring Jetty ALPN in Web Containers

Some web containers, such as [Jetty](http://www.eclipse.org/jetty/documentation/current/jetty-classloading.html) restrict access to server classes for web applications. A gRPC client running within such a container must be properly configured to allow access to the ALPN classes. In Jetty, this is done by including a `WEB-INF/jetty-env.xml` file containing the following:

```xml
<?xml version="1.0"  encoding="ISO-8859-1"?>
<!DOCTYPE Configure PUBLIC "-//Mort Bay Consulting//DTD Configure//EN" "http://www.eclipse.org/jetty/configure.dtd">
<Configure class="org.eclipse.jetty.webapp.WebAppContext">
    <!-- Must be done in jetty-env.xml, since jetty-web.xml is loaded too late.   -->
    <!-- Removing ALPN from the blacklisted server classes (using "-" to remove). -->
    <!-- Must prepend to the blacklist since order matters.                       -->
    <Call name="prependServerClass">
        <Arg>-org.eclipse.jetty.alpn.</Arg>
    </Call>
</Configure>
```
## Enabling TLS on a server

To use TLS on the server, a certificate chain and private key need to be
specified in PEM format. The standard TLS port is 443, but we use 8443 below to
avoid needing extra permissions from the OS.

```java
Server server = ServerBuilder.forPort(8443)
    // Enable TLS
    .useTransportSecurity(certChainFile, privateKeyFile)
    .addService(serviceImplementation)
    .build();
server.start();
```

If the issuing certificate authority is not known to the client then a properly
configured SslContext or SSLSocketFactory should be provided to the
NettyChannelBuilder or OkHttpChannelBuilder, respectively.

## Mutual TLS

[Mutual authentication][] (or "client-side authentication") configuration is similar to the server by providing truststores, a client certificate and private key to the client channel.  The server must also be configured to request a certificate from clients, as well as truststores for which client certificates it should allow.

```java
Server server = NettyServerBuilder.forPort(8443)
    .sslContext(GrpcSslContexts.forServer(certChainFile, privateKeyFile)
        .trustManager(clientCAsFile)
        .clientAuth(ClientAuth.REQUIRE)
        .build());
```

Negotiated client certificates are available in the SSLSession, which is found in the `TRANSPORT_ATTR_SSL_SESSION` attribute of <a href="https://github.com/grpc/grpc-java/blob/master/core/src/main/java/io/grpc/Grpc.java">Grpc</a>.  A server interceptor can provide details in the current Context.

```java
public final static Context.Key<SSLSession> SSL_SESSION_CONTEXT = Context.key("SSLSession");

@Override
public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(ServerCall<RespT> call, 
    Metadata headers, ServerCallHandler<ReqT, RespT> next) {
    SSLSession sslSession = call.attributes().get(Grpc.TRANSPORT_ATTR_SSL_SESSION);
    if (sslSession == null) {
        return next.startCall(call, headers)
    }
    return Contexts.interceptCall(
        Context.current().withValue(SSL_SESSION_CONTEXT, clientContext), call, headers, next);
}
```

[Mutual authentication]: http://en.wikipedia.org/wiki/Transport_Layer_Security#Client-authenticated_TLS_handshake

## Troubleshooting

If you received an error message "ALPN is not configured properly" or "Jetty ALPN/NPN has not been properly configured", it most likely means that:
 - ALPN related dependencies are either not present in the classpath
 - or that there is a classpath conflict
 - or that a wrong version is used due to dependency management
 - or you are on an unsupported platform (e.g., 32-bit OS, Alpine with `musl` libc). See [Transport Security](#transport-security-tls) for supported platforms.

### Netty
If you aren't using gRPC on Android devices, you are most likely using `grpc-netty` transport.

If you are developing for Android and have a dependency on `grpc-netty`, you should remove it as `grpc-netty` is unsupported on Android. Use `grpc-okhttp` instead.

If you are on a 32-bit operating system, or not on a [Transport Security supported platform](#transport-security-tls), you should use Jetty ALPN (and beware of potential issues), or you'll need to build your own 32-bit version of `netty-tcnative`.

If you are using `musl` libc (e.g., with Alpine Linux), then
`netty-tcnative-boringssl-static` won't work. There are several alternatives:
 - Use [netty-tcnative-alpine](https://github.com/pires/netty-tcnative-alpine)
 - Use a distribution with `glibc`

If you are running inside of an embedded Tomcat runtime (e.g., Spring Boot),
then some versions of `netty-tcnative-boringssl-static` will have conflicts and
won't work. You must use gRPC 1.4.0 or later.

Most dependency versioning problems can be solved by using
`io.grpc:grpc-netty-shaded` instead of `io.grpc:grpc-netty`, although this also
limits your usage of the Netty-specific APIs. `io.grpc:grpc-netty-shaded`
includes the proper version of Netty and `netty-tcnative-boringssl-static` in a
way that won't conflict with other Netty usages.

Find the dependency tree (e.g., `mvn dependency:tree`), and look for versions of:
 - `io.grpc:grpc-netty`
 - `io.netty:netty-handler` (really, make sure all of io.netty except for
   netty-tcnative has the same version)
 - `io.netty:netty-tcnative-boringssl-static:jar` 

If `netty-tcnative-boringssl-static` is missing, then you either need to add it as a dependency, or use alternative methods of providing ALPN capability by reading the *Transport Security (TLS)* section carefully.

If you have both `netty-handler` and `netty-tcnative-boringssl-static` dependencies, then check the versions carefully. These versions could've been overridden by dependency management from another BOM. You would receive the "ALPN is not configured properly" exception if you are using incompatible versions.

If you have other `netty` dependencies, such as `netty-all`, that are pulled in from other libraries, then ultimately you should make sure only one `netty` dependency is used to avoid classpath conflict. The easiest way is to exclude transitive Netty dependencies from all the immediate dependencies, e.g., in Maven use `<exclusions>`, and then add an explict Netty dependency in your project along with the corresponding `tcnative` versions. See the versions table below.

If you are running in a runtime environment that also uses Netty (e.g., Hadoop, Spark, Spring Boot 2) and you have no control over the Netty version at all, then you should use a shaded gRPC Netty dependency to avoid classpath conflicts with other Netty versions in runtime the classpath:
 - Remove `io.grpc:grpc-netty` dependency
 - Add `io.grpc:grpc-netty-shaded` dependency

Below are known to work version combinations:

grpc-netty version | netty-handler version | netty-tcnative-boringssl-static version
------------------ | --------------------- | ---------------------------------------
1.0.0-1.0.1        | 4.1.3.Final           | 1.1.33.Fork19
1.0.2-1.0.3        | 4.1.6.Final           | 1.1.33.Fork23
1.1.x-1.3.x        | 4.1.8.Final           | 1.1.33.Fork26
1.4.x              | 4.1.11.Final          | 2.0.1.Final
1.5.x              | 4.1.12.Final          | 2.0.5.Final
1.6.x              | 4.1.14.Final          | 2.0.5.Final
1.7.x-1.8.x        | 4.1.16.Final          | 2.0.6.Final
1.9.x-1.10.x       | 4.1.17.Final          | 2.0.7.Final
1.11.x-1.12.x      | 4.1.22.Final          | 2.0.7.Final
1.13.x             | 4.1.25.Final          | 2.0.8.Final
1.14.x-            | 4.1.27.Final          | 2.0.12.Final

_(grpc-netty-shaded avoids issues with keeping these versions in sync.)_

### OkHttp
If you are using gRPC on Android devices, you are most likely using `grpc-okhttp` transport.

Find the dependency tree (e.g., `mvn dependency:tree`), and look for versions of:
 - `io.grpc:grpc-okhttp`
 - `com.squareup.okhttp:okhttp`

If you don't have `grpc-okhttp`, you should add it as a dependency.

If you have both `io.grpc:grpc-netty` and `io.grpc:grpc-okhttp`, you may also have issues. Remove `grpc-netty` if you are on Android.

If you have `okhttp` version below 2.5.0, then it may not work with gRPC.

It is OK to have both `okhttp` 2.x and 3.x since they have different group name and under different packages.

# gRPC over plaintext

An option is provided to use gRPC over plaintext without TLS. While this is convenient for testing environments, users must be aware of the security risks of doing so for real production systems.

# Using OAuth2

The following code snippet shows how you can call the Google Cloud PubSub API using gRPC with a service account. The credentials are loaded from a key stored in a well-known location or by detecting that the application is running in an environment that can provide one automatically, e.g. Google Compute Engine. While this example is specific to Google and it's services, similar patterns can be followed for other service providers.

```java
// Create a channel to the test service.
ManagedChannel channel = ManagedChannelBuilder.forTarget("pubsub.googleapis.com")
    .build();
// Get the default credentials from the environment
GoogleCredentials creds = GoogleCredentials.getApplicationDefault();
// Down-scope the credential to just the scopes required by the service
creds = creds.createScoped(Arrays.asList("https://www.googleapis.com/auth/pubsub"));
// Create an instance of {@link io.grpc.CallCredentials}
CallCredentials callCreds = MoreCallCredentials.from(creds);
// Create a stub with credential
PublisherGrpc.PublisherBlockingStub publisherStub =
    PublisherGrpc.newBlockingStub(channel).withCallCredentials(callCreds);
publisherStub.publish(someMessage);
```
