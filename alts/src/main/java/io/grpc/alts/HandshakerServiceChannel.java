/*
 * Copyright 2018 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.alts;

import com.google.common.base.Preconditions;
import io.grpc.ManagedChannel;
import io.grpc.netty.NettyChannelBuilder;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.util.concurrent.DefaultThreadFactory;
import java.util.concurrent.ThreadFactory;

/**
 * Class for creating a single shared grpc channel to the ALTS Handshaker Service. The channel to
 * the handshaker service is local and is over plaintext. Each application will have at most one
 * connection to the handshaker service.
 *
 * <p>TODO: Release the channel if it is not used. https://github.com/grpc/grpc-java/issues/4755.
 */
final class HandshakerServiceChannel {
  // Default handshaker service address.
  private static String handshakerAddress = "metadata.google.internal:8080";
  // Shared channel to ALTS handshaker service.
  private static ManagedChannel channel = null;

  // Construct me not!
  private HandshakerServiceChannel() {}

  // Sets handshaker service address for testing and creates the channel to the handshaker service.
  public static synchronized void setHandshakerAddressForTesting(String handshakerAddress) {
    Preconditions.checkState(
        channel == null || HandshakerServiceChannel.handshakerAddress.equals(handshakerAddress),
        "HandshakerServiceChannel already created with a different handshakerAddress");
    HandshakerServiceChannel.handshakerAddress = handshakerAddress;
    if (channel == null) {
      channel = createChannel();
    }
  }

  /** Create a new channel to ALTS handshaker service, if it has not been created yet. */
  private static ManagedChannel createChannel() {
    /* Use its own event loop thread pool to avoid blocking. */
    ThreadFactory clientThreadFactory = new DefaultThreadFactory("handshaker pool", true);
    ManagedChannel channel =
        NettyChannelBuilder.forTarget(handshakerAddress)
            .directExecutor()
            .eventLoopGroup(new NioEventLoopGroup(1, clientThreadFactory))
            .usePlaintext()
            .build();
    return channel;
  }

  public static synchronized ManagedChannel get() {
    if (channel == null) {
      channel = createChannel();
    }
    return channel;
  }
}
