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

import io.grpc.ManagedChannel;
import io.grpc.internal.FixedObjectPool;
import io.grpc.internal.SharedResourceHolder.Resource;
import io.grpc.netty.NettyChannelBuilder;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.util.concurrent.DefaultThreadFactory;
import java.util.concurrent.ThreadFactory;

/**
 * Class for creating a single shared gRPC channel to the ALTS Handshaker Service using
 * SharedResourceHolder. The channel to the handshaker service is local and is over plaintext. Each
 * application will have at most one connection to the handshaker service.
 */
final class HandshakerServiceChannel {

  static final Resource<ManagedChannel> SHARED_HANDSHAKER_CHANNEL =
      new Resource<ManagedChannel>() {

        private EventLoopGroup eventGroup = null;

        @Override
        public ManagedChannel create() {
          /* Use its own event loop thread pool to avoid blocking. */
          if (eventGroup == null) {
            eventGroup =
                new NioEventLoopGroup(1, new DefaultThreadFactory("handshaker pool", true));
          }
          return NettyChannelBuilder.forTarget("metadata.google.internal:8080")
              .directExecutor()
              .eventLoopGroup(eventGroup)
              .usePlaintext()
              .build();
        }

        @Override
        @SuppressWarnings("FutureReturnValueIgnored") // netty ChannelFuture
        public void close(ManagedChannel instance) {
          instance.shutdownNow();
          if (eventGroup != null) {
            eventGroup.shutdownGracefully();
          }
        }

        @Override
        public String toString() {
          return "grpc-alts-handshaker-service-channel";
        }
      };

  /** Returns a fixed object pool of handshaker service channel for testing only. */
  static FixedObjectPool<ManagedChannel> getHandshakerChannelPoolForTesting(
      String handshakerAddress) {
    ThreadFactory clientThreadFactory = new DefaultThreadFactory("handshaker pool", true);
    ManagedChannel channel =
        NettyChannelBuilder.forTarget(handshakerAddress)
            .directExecutor()
            .eventLoopGroup(new NioEventLoopGroup(1, clientThreadFactory))
            .usePlaintext()
            .build();
    return new FixedObjectPool<ManagedChannel>(channel);
  }
}
