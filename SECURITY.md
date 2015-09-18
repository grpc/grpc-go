# Authentication

As outlined in <a href="https://github.com/grpc/grpc/blob/master/doc/grpc-auth-support.md">gRPC Authentication Support</a>, gRPC supports a number of different mechanisms for asserting identity between an client and server. This document provides code samples demonstrating how to provide SSL/TLS encryption support and identity assertions in Java, as well as passing OAuth2 tokens to services that support it.

# Transport Security (TLS)

HTTP/2 over TLS mandates the use of [ALPN](https://tools.ietf.org/html/draft-ietf-tls-applayerprotoneg-05) to negotiate the use of the h2 protocol. ALPN is a fairly new standard and (where possible) gRPC also supports protocol negotiation via [NPN](https://tools.ietf.org/html/draft-agl-tls-nextprotoneg-04) for systems that do not yet support ALPN.

## TLS on Android

On Android we recommend the use of the [Play Services Dynamic Security Provider](http://appfoundry.be/blog/2014/11/18/Google-Play-Services-Dynamic-Security-Provider) to ensure your application has an up-to-date OpenSSL library with the necessary ciper-suites and a reliable ALPN implementation.

You may need to [update the security provider](https://developer.android.com/training/articles/security-gms-provider.html) to enable ALPN support, especially for Android versions < 5.0. If the provider fails to update, ALPN may not work.

## TLS with OpenSSL

This is currently the recommended approach for using gRPC over TLS (on non-Android systems). 

### Benefits of using OpenSSL

1. **Speed**: In local testing, we've seen performance improvements of 3x over the JDK.
2. **Ciphers**: OpenSSL has its own ciphers and is not dependent on the limitations of the JDK (see section on TLS with JDK). Also, the GCM codec (the main codec recommended by the HTTP/2 spec) in OpenSSL does not suffer from the performance problems in Java 8.
3. **ALPN to NPN Fallback**: if the remote endpoint doesn't support ALPN.

### Requirements for using OpenSSL

1. Currently only supported by the Netty transport (via netty-tcnative).
2. [OpenSSL](https://www.openssl.org/) version >= 1.0.2 for ALPN support, or version >= 1.0.1 for NPN.
3. [netty-tcnative](https://github.com/netty/netty-tcnative) version >= 1.1.33.Fork7 must be on classpath.
4. Supported platforms (for netty-tcnative): `linux-x86_64`, `mac-x86_64`, `windows-x86_64`. Supporting other platforms will require manually building netty-tcnative.

If the above requirements met, the Netty transport will automatically select OpenSSL as the default TLS provider.

### Configuring netty-tcnative

[Netty-tcnative](https://github.com/netty/netty-tcnative) is a fork of [Apache Tomcat's tcnative](http://tomcat.apache.org/native-doc/), a JNI wrapper around OpenSSL.

Netty uses classifiers when deploying to [Maven Central](http://repo1.maven.org/maven2/io/netty/netty-tcnative/) to provide distributions for the various platforms. On Linux it should be noted that OpenSSL uses a different soname for Fedora derivatives than other Linux releases. To work around this limitation, netty-tcnative deploys two separate versions for linux.

Classifier | Description
---------------- | -----------
windows-x86_64 | Windows distribution
osx-x86_64 | Mac distribution
linux-x86_64 | Used for non-Fedora derivatives of Linux
linux-x86_64-fedora | Used for Fedora derivatives

*NOTE: Make sure you use a version of netty-tcnative >= 1.1.33.Fork7.  Prior versions only supported NPN and only Fedora-derivatives were supported for Linux.*

#### Getting netty-tcnative from Maven

In Maven, you can use the [os-maven-plugin](https://github.com/trustin/os-maven-plugin) to help simplify the dependency.

```xml
<project>
  <dependencies>
    <dependency>
      <groupId>io.netty</groupId>
      <artifactId>netty-tcnative</artifactId>
      <version>1.1.33.Fork7</version>
      <classifier>${tcnative.classifier}</classifier>
    </dependency>
  </dependencies>

  <build>
    <extensions>
      <!-- Use os-maven-plugin to initialize the "os.detected" properties -->
      <extension>
        <groupId>kr.motd.maven</groupId>
        <artifactId>os-maven-plugin</artifactId>
        <version>1.4.0.Final</version>
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

#### Getting netty-tcnative from Gradle

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
    compile 'io.netty:netty-tcnative:1.1.33.Fork7:' + tcnative_classifier
}
```

## TLS with JDK (Jetty ALPN/NPN)

If not using the Netty transport (or you are unable to use OpenSSL for some reason) another alternative is to use the JDK for TLS.

No standard Java release has built-in support for ALPN today ([there is a tracking issue](https://bugs.openjdk.java.net/browse/JDK-8051498) so go upvote it!) so we need to use the [Jetty-ALPN](https://github.com/jetty-project/jetty-alpn") (or [Jetty-NPN](https://github.com/jetty-project/jetty-npn) if on Java < 8) bootclasspath extension for OpenJDK. To do this, add a `Xbootclasspath` JVM option referencing the path to the Jetty `alpn-boot` jar.

```sh
java -Xbootclasspath/p:/path/to/jetty/alpn/extension.jar ...
```

Note that you must use the [release of the Jetty-ALPN jar](http://www.eclipse.org/jetty/documentation/current/alpn-chapter.html#alpn-versions) specific to the version of Java you are using.

### JDK Ciphers

Java 7 does not support [the cipher suites recommended](https://tools.ietf.org/html/draft-ietf-httpbis-http2-17#section-9.2.2) by the HTTP2 specification. To address this we suggest servers use Java 8 where possible or use an alternative JCE implementation such as [Bouncy Castle](https://www.bouncycastle.org/java.html). If this is not practical it is possible to use other ciphers but you need to ensure that the services you intend to call have [allowed out-of-spec ciphers](https://github.com/grpc/grpc/issues/681) and have evaluated the security risks of doing so.

Users should be aware that GCM is [_very_ slow (1 MB/s)](https://bugzilla.redhat.com/show_bug.cgi?id=1135504) in Java 8. GCM cipher suites are the only suites available that comply with HTTP2's cipher requirements.

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

In this example the service owner provides a certificate chain and private key to create an SslContext. This is then bound to the server which is started on a specific port, in this case 443 which is the standard SSL port. Note that the service implementation is also bound while creating the server.

```java
// Load certificate chain and key for SSL server into a Netty SslContext
SslContext sslContext = GrpcSslContexts.forServer(certChainFile, privateKeyFile);
// Create a server, bound to port 443 and exposing a service implementation
ServerImpl server = NettyServerBuilder.forPort(443)
    .sslContext(sslContext)
    .addService(TestServiceGrpc.bindService(serviceImplementation))
    .build();
server.start();
```

If the issuing certificate authority for a server is not known to the client then a similar process should be followed on the client to load it so that it may validate the certificate issued to the server. If <a href="http://en.wikipedia.org/wiki/Transport_Layer_Security#Client-authenticated_TLS_handshake">mutual authentication</a> is desired this can also be supported by creating the appropriate SslContext.

# gRPC over plaintext

An option is provided to use gRPC over plaintext without TLS. While this is convenient for testing environments, users must be aware of the security risks of doing so for real production systems.

# Using OAuth2

The following code snippet shows how you can call the Google Cloud PubSub API using gRPC with a service account. The credentials are loaded from a key stored in a well-known location or by detecting that the application is running in an environment that can provide one automatically, e.g. Google Compute Engine. While this example is specific to Google and it's services, similar patterns can be followed for other service providers.

```java
// Create a channel to the test service.
ChannelImpl channelImpl = NettyChannelBuilder.forAddress("pubsub.googleapis.com")
    .negotiationType(NegotiationType.TLS)
    .build();
// Get the default credentials from the environment
GoogleCredentials creds = GoogleCredentials.getApplicationDefault();
// Down-scope the credential to just the scopes required by the service
creds = creds.createScoped(Arrays.asList("https://www.googleapis.com/auth/pubsub"));
// Intercept the channel to bind the credential
ClientAuthInterceptor interceptor = new ClientAuthInterceptor(creds, someExecutor);
Channel channel = ClientInterceptors.intercept(channelImpl, interceptor);
// Create a stub using the channel that has the bound credential
PublisherGrpc.PublisherBlockingStub publisherStub = PublisherGrpc.newBlockingStub(channel);
publisherStub.publish(someMessage);
```
