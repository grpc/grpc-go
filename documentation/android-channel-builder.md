# AndroidChannelBuilder

Since gRPC's 1.12 release, the `grpc-android` package provides access to the
`AndroidChannelBuilder` class. Given an Android Context, this builder will
register a network event listener upon channel construction.  The listener is
used to automatically respond to changes in the device's network state, avoiding
delays and interrupted RPCs that may otherwise occur.

By default, gRPC uses exponential backoff to recover from connection failures.
Depending on the scheduled backoff delay when the device regains connectivity,
this could result in a  one minute or longer delay before gRPC re-establishes
the connection. This delay is removed when `AndroidChannelBuilder` is provided
with the app's Android Context.  Notifications from the network listener will
cause the channel to immediately reconnect upon network recovery.

On Android API levels 24+, `AndroidChannelBuilder`'s network listener mechanism
allows graceful switching from cellular to wifi connections. When an Android
device on a cellular network connects to a wifi network, there is a brief
(typically 30 second) interval when both cellular and wifi networks remain
available, then any connections on the cellular network are terminated.  By
listening for changes in the device's default network, `AndroidChannelBuilder`
sends new RPCs via wifi rather than using an already-established cellular
connection. Without listening for pending network changes, new RPCs sent on an
already established cellular connection would fail when the device terminates
cellular connections.

***Note:*** *Currently, `AndroidChannelBuilder` is only compatible with gRPC
OkHttp. We plan to offer additional Android-specific features compatible with
both the OkHttp and Cronet transports in the future, but the network listener
mechanism is only necessary with OkHttp; the Cronet library internally handles
connection management on Android devices.*

## Example usage:

In your `build.gradle` file, include a dependency on both `grpc-android` and
`grpc-okhttp`:

```
compile 'io.grpc:grpc-android:1.16.0' // CURRENT_GRPC_VERSION
compile 'io.grpc:grpc-okhttp:1.16.0' // CURRENT_GRPC_VERSION
```

You will also need permission to access the device's network state in your
`AndroidManifest.xml`:

```
<uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />
```

When constructing your channel, use `AndroidChannelBuilder` and provide it with
your app's Context:

```
import io.grpc.android.AndroidChannelBuilder;
...
ManagedChannel channel = AndroidChannelBuilder.forAddress("localhost", 8080)
    .context(getApplicationContext())
    .build();
```

You continue to use the constructed channel exactly as you would any other
channel. gRPC will now monitor and respond to the device's network state
automatically. When you shutdown the managed channel, the network listener
registered by `AndroidChannelBuilder` will be unregistered.

