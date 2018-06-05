gRPC Cronet Transport
========================

**EXPERIMENTAL:**  *gRPC's Cronet transport is an experimental API, and is not
yet integrated with our build system. Using Cronet with gRPC requires manually
integrating the gRPC code in this directory into your Android application.*

This code enables using the [Chromium networking stack
(Cronet)](https://chromium.googlesource.com/chromium/src/+/master/components/cronet)
as the transport layer for gRPC on Android. This lets your Android app make
RPCs using the same networking stack as used in the Chrome browser.

Some advantages of using Cronet with gRPC:
* Bundles an OpenSSL implementation, enabling TLS connections even on older
  versions of Android without additional configuration
* Robust to Android network connectivity changes
* Support for [QUIC](https://www.chromium.org/quic)

Cronet jars are available on Google's Maven repository. See the example app at
https://github.com/GoogleChrome/cronet-sample/blob/master/README.md. To use
Cronet with gRPC, you will need to copy the gRPC source files contained in this
directory into your application's code, as we do not currently provide a
`grpc-cronet` dependency.

To use Cronet, you must have the `ACCESS_NETWORK_STATE` permission set in
`AndroidManifest.xml`:

```
<uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />
```

Once the above steps are completed, you can create a gRPC Cronet channel as
follows:

```
import io.grpc.cronet.CronetChannelBuilder;
import org.chromium.net.ExperimentalCronetEngine;

...

ExperimentalCronetEngine engine =
    new ExperimentalCronetEngine.Builder(context /* Android Context */).build();
ManagedChannel channel = CronetChannelBuilder.forAddress("localhost", 8080, engine).build();
```

