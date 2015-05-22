# Authentication

As outlined in <a href="https://github.com/grpc/grpc-common/blob/master/grpc-auth-support.md">gRPC Authentication Support</a>, gRPC supports a number of different mechanisms for asserting identity between an client and server. This document provides code samples demonstrating how to provide SSL/TLS encryption support and identity assertions in Java, as well as passing OAuth2 tokens to services that support it.

# Java 7, HTTP2 & Crypto

## Cipher-Suites
Java 7 does not support the <a href="https://tools.ietf.org/html/draft-ietf-httpbis-http2-17#section-9.2.2">the cipher suites recommended</a> by the HTTP2 specification. To address this we suggest servers use Java 8 where possible or use an alternative JCE implementation such as <a href="https://www.bouncycastle.org/java.html">Bouncy Castle</a>. If this is not practical it is possible to use other ciphers but you need to ensure that the services you intend to call have <a href="https://github.com/grpc/grpc/issues/681">allowed out-of-spec ciphers</a> and have evaluated the security risks of doing so. On Android we recommend the use of the <a href="http://appfoundry.be/blog/2014/11/18/Google-Play-Services-Dynamic-Security-Provider/">Play Services Dynamic Security Provider</a> to ensure your application has an up-to-date OpenSSL library with the necessary ciper-suites and a reliable ALPN implementation.

Users should be aware that GCM is [_very_ slow (1 MB/s)](https://bugzilla.redhat.com/show_bug.cgi?id=1135504) in JDK 8. GCM cipher suites are the only suites available that comply with HTTP2's cipher requirements.

## Protocol Negotiation (TLS-ALPN)
HTTP2 mandates the use of <a href="https://tools.ietf.org/html/draft-ietf-tls-applayerprotoneg-05">ALPN</a> to negotiate the use of the protocol over SSL. No standard Java release has built-in support for ALPN today (<a href="https://bugs.openjdk.java.net/browse/JDK-8051498">there is a tracking issue</a> so go upvote it!) so we need to use the <a href="https://github.com/jetty-project/jetty-alpn">Jetty-ALPN</a> bootclasspath extension for OpenJDK to make it work.

```sh
java -Xbootclasspath/p:/path/to/jetty/alpn/extension.jar ...
```

Note that you must use the release of the Jetty-ALPN jar specific to the version of Java you are using.

An option is provided to use GRPC over plaintext without TLS. This is convenient for testing environments, however users must be aware of the secuirty risks of doing so for real production systems.

### TLS-ALPN on Android
On Android, it is needed to <a href="https://developer.android.com/training/articles/security-gms-provider.html">update your security provider</a> to enable ALPN support, especially for Android versions < 5.0. If the provider fails to update, ALPN may not work.

After the update is done, you'll need to pass an SSLSocketFactorty to OkHttpChannelBuilder, like the code snippet below shows.

```java
OkHttpChannelBuilder channelBuilder = OkHttpChannelBuilder.forAddress(host, port)
    .sslSocketFactory(SSLContext.getDefault().getSocketFactory());
```

### TLS-ALPN in Jetty
Some web containers, such as <a href="http://www.eclipse.org/jetty/documentation/current/jetty-classloading.html">Jetty</a> restrict access to server classes for web applications. A gRPC client running within such a container must be properly configured to allow access to the ALPN classes.

In Jetty, this is done by including a `WEB-INF/jetty-env.xml` file containing the following:

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

# Using OAuth2

The following code snippet shows how you can call the Google Cloud PubSub API using GRPC with a service account. The credentials are loaded from a key stored in a well-known location or by detecting that the application is running in an environment that can provide one automatically, e.g. Google Compute Engine. While this example is specific to Google and it's services, similar patterns can be followed for other service providers.

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


# Enabling TLS on a server

In this example the service owner provides a certificate chain and private key to create an SslContext. This is then bound to the server which is started on a specific port, in this case 443 which is the standard SSL port. Note that the service implementation is also bound while creating the server.


```java
// Load certificate chain and key for SSL server into a Netty SslContext
SslContext sslContext = SslContext.newServerContext(certChainFile, privateKeyFile);
// Create a server, bound to port 443 and exposing a service implementation
ServerImpl server = NettyServerBuilder.forPort(443)
    .sslContext(sslContext)
    .addService(TestServiceGrpc.bindService(serviceImplementation))
    .build();
server.start();
```

If the issuing certificate authority for a server is not known to the client then a similar process should be followed on the client to load it so that it may validate the certificate issued to the server. If <a href="http://en.wikipedia.org/wiki/Transport_Layer_Security#Client-authenticated_TLS_handshake">mutual authentication</a> is desired this can also be supported by creating the appropriate SslContext.
