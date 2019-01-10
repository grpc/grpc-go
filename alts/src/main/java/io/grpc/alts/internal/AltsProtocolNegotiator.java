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

package io.grpc.alts.internal;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.protobuf.Any;
import io.grpc.Attributes;
import io.grpc.Grpc;
import io.grpc.InternalChannelz.OtherSecurity;
import io.grpc.InternalChannelz.Security;
import io.grpc.SecurityLevel;
import io.grpc.Status;
import io.grpc.alts.internal.RpcProtocolVersionsUtil.RpcVersionsCheckResult;
import io.grpc.alts.internal.TsiHandshakeHandler.TsiHandshakeCompletionEvent;
import io.grpc.internal.GrpcAttributes;
import io.grpc.netty.GrpcHttp2ConnectionHandler;
import io.grpc.netty.ProtocolNegotiator;
import io.grpc.netty.ProtocolNegotiators.AbstractBufferingHandler;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.util.AsciiString;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A GRPC {@link ProtocolNegotiator} for ALTS. This class creates a Netty handler that provides ALTS
 * security on the wire, similar to Netty's {@code SslHandler}.
 */
public abstract class AltsProtocolNegotiator implements ProtocolNegotiator {

  private static final Logger logger = Logger.getLogger(AltsProtocolNegotiator.class.getName());

  @Grpc.TransportAttr
  public static final Attributes.Key<TsiPeer> TSI_PEER_KEY = Attributes.Key.create("TSI_PEER");
  @Grpc.TransportAttr
  public static final Attributes.Key<AltsAuthContext> ALTS_CONTEXT_KEY =
      Attributes.Key.create("ALTS_CONTEXT_KEY");
  private static final AsciiString scheme = AsciiString.of("https");

  /** Creates a negotiator used for ALTS client. */
  public static AltsProtocolNegotiator createClientNegotiator(
      final TsiHandshakerFactory handshakerFactory) {
    final class ClientAltsProtocolNegotiator extends AltsProtocolNegotiator {
      @Override
      public Handler newHandler(GrpcHttp2ConnectionHandler grpcHandler) {
        TsiHandshaker handshaker = handshakerFactory.newHandshaker(grpcHandler.getAuthority());
        return new BufferUntilAltsNegotiatedHandler(
            grpcHandler,
            new TsiHandshakeHandler(new NettyTsiHandshaker(handshaker)),
            new TsiFrameHandler());
      }

      @Override
      public void close() {
        logger.finest("ALTS Client ProtocolNegotiator Closed");
        // TODO(jiangtaoli2016): release resources
      }
    }
    
    return new ClientAltsProtocolNegotiator();
  }

  /** Creates a negotiator used for ALTS server. */
  public static AltsProtocolNegotiator createServerNegotiator(
      final TsiHandshakerFactory handshakerFactory) {
    final class ServerAltsProtocolNegotiator extends AltsProtocolNegotiator {
      @Override
      public Handler newHandler(GrpcHttp2ConnectionHandler grpcHandler) {
        TsiHandshaker handshaker = handshakerFactory.newHandshaker(/*authority=*/ null);
        return new BufferUntilAltsNegotiatedHandler(
            grpcHandler,
            new TsiHandshakeHandler(new NettyTsiHandshaker(handshaker)),
            new TsiFrameHandler());
      }

      @Override
      public void close() {
        logger.finest("ALTS Server ProtocolNegotiator Closed");
        // TODO(jiangtaoli2016): release resources
      }
    }

    return new ServerAltsProtocolNegotiator();
  }

  /** Buffers all writes until the ALTS handshake is complete. */
  @VisibleForTesting
  static final class BufferUntilAltsNegotiatedHandler extends AbstractBufferingHandler
      implements ProtocolNegotiator.Handler {

    private final GrpcHttp2ConnectionHandler grpcHandler;

    BufferUntilAltsNegotiatedHandler(
        GrpcHttp2ConnectionHandler grpcHandler, ChannelHandler... negotiationhandlers) {
      super(negotiationhandlers);
      // Save the gRPC handler. The ALTS handler doesn't support buffering before the handshake
      // completes, so we wait until the handshake was successful before adding the grpc handler.
      this.grpcHandler = grpcHandler;
    }

    // TODO: Remove this once https://github.com/grpc/grpc-java/pull/3715 is in.
    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) {
      logger.log(Level.FINEST, "Exception while buffering for ALTS Negotiation", cause);
      fail(ctx, cause);
      ctx.fireExceptionCaught(cause);
    }

    @Override
    public AsciiString scheme() {
      return scheme;
    }

    @Override
    public void userEventTriggered(ChannelHandlerContext ctx, Object evt) throws Exception {
      if (logger.isLoggable(Level.FINEST)) {
        logger.log(Level.FINEST, "User Event triggered while negotiating ALTS", new Object[]{evt});
      }
      if (evt instanceof TsiHandshakeCompletionEvent) {
        TsiHandshakeCompletionEvent altsEvt = (TsiHandshakeCompletionEvent) evt;
        if (altsEvt.isSuccess()) {
          // Add the gRPC handler just before this handler. We only allow the grpcHandler to be
          // null to support testing. In production, a grpc handler will always be provided.
          if (grpcHandler != null) {
            ctx.pipeline().addBefore(ctx.name(), null, grpcHandler);
            AltsAuthContext altsContext = (AltsAuthContext) altsEvt.context();
            Preconditions.checkNotNull(altsContext);
            // Checks peer Rpc Protocol Versions in the ALTS auth context. Fails the connection if
            // Rpc Protocol Versions mismatch.
            RpcVersionsCheckResult checkResult =
                RpcProtocolVersionsUtil.checkRpcProtocolVersions(
                    RpcProtocolVersionsUtil.getRpcProtocolVersions(),
                    altsContext.getPeerRpcVersions());
            if (!checkResult.getResult()) {
              String errorMessage =
                  "Local Rpc Protocol Versions "
                      + RpcProtocolVersionsUtil.getRpcProtocolVersions().toString()
                      + "are not compatible with peer Rpc Protocol Versions "
                      + altsContext.getPeerRpcVersions().toString();
              logger.finest(errorMessage);
              fail(ctx, Status.UNAVAILABLE.withDescription(errorMessage).asRuntimeException());
            }
            grpcHandler.handleProtocolNegotiationCompleted(
                Attributes.newBuilder()
                    .set(TSI_PEER_KEY, altsEvt.peer())
                    .set(ALTS_CONTEXT_KEY, altsContext)
                    .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, ctx.channel().remoteAddress())
                    .set(Grpc.TRANSPORT_ATTR_LOCAL_ADDR, ctx.channel().localAddress())
                    .set(GrpcAttributes.ATTR_SECURITY_LEVEL, SecurityLevel.PRIVACY_AND_INTEGRITY)
                    .build(),
                new Security(new OtherSecurity("alts", Any.pack(altsContext.context))));
          }
          logger.finest("Flushing ALTS buffered data");
          // Now write any buffered data and remove this handler.
          writeBufferedAndRemove(ctx);
        } else {
          logger.log(Level.FINEST, "ALTS handshake failed", altsEvt.cause());
          fail(ctx, unavailableException("ALTS handshake failed", altsEvt.cause()));
        }
      }
      super.userEventTriggered(ctx, evt);
    }

    private static RuntimeException unavailableException(String msg, Throwable cause) {
      return Status.UNAVAILABLE.withCause(cause).withDescription(msg).asRuntimeException();
    }
  }
}
