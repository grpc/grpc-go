/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.netty;

import io.grpc.netty.ProtocolNegotiators.GrpcNegotiationHandler;
import io.grpc.netty.ProtocolNegotiators.WaitUntilActiveHandler;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.handler.ssl.SslContext;
import io.netty.util.AsciiString;

/**
 * Internal accessor for {@link ProtocolNegotiators}.
 */
public final class InternalProtocolNegotiators {

  private InternalProtocolNegotiators() {}

  /**
   * Buffers all writes until either {@link #writeBufferedAndRemove(ChannelHandlerContext)} or
   * {@link #fail(ChannelHandlerContext, Throwable)} is called. This handler allows us to
   * write to a {@link io.netty.channel.Channel} before we are allowed to write to it officially
   * i.e.  before it's active or the TLS Handshake is complete.
   */
  public abstract static class AbstractBufferingHandler
      extends ProtocolNegotiators.AbstractBufferingHandler {

    protected AbstractBufferingHandler(ChannelHandler... handlers) {
      super(handlers);
    }
  }

  /**
   * Returns a {@link ProtocolNegotiator} that ensures the pipeline is set up so that TLS will
   * be negotiated, the {@code handler} is added and writes to the {@link io.netty.channel.Channel}
   * may happen immediately, even before the TLS Handshake is complete.
   */
  public static InternalProtocolNegotiator.ProtocolNegotiator tls(SslContext sslContext) {
    final io.grpc.netty.ProtocolNegotiator negotiator = ProtocolNegotiators.tls(sslContext);
    final class TlsNegotiator implements InternalProtocolNegotiator.ProtocolNegotiator {

      @Override
      public AsciiString scheme() {
        return negotiator.scheme();
      }

      @Override
      public ChannelHandler newHandler(GrpcHttp2ConnectionHandler grpcHandler) {
        return negotiator.newHandler(grpcHandler);
      }

      @Override
      public void close() {
        negotiator.close();
      }
    }
    
    return new TlsNegotiator();
  }

  /**
   * Internal version of {@link WaitUntilActiveHandler}.
   */
  public static ChannelHandler waitUntilActiveHandler(ChannelHandler next) {
    return new WaitUntilActiveHandler(next);
  }

  /**
   * Internal version of {@link GrpcNegotiationHandler}.
   */
  public static ChannelHandler grpcNegotiationHandler(GrpcHttp2ConnectionHandler next) {
    return new GrpcNegotiationHandler(next);
  }
}
