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

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.channel.ChannelHandlerContext;
import io.netty.handler.codec.ByteToMessageDecoder;
import io.netty.util.ReferenceCountUtil;
import java.security.GeneralSecurityException;
import java.util.List;
import java.util.concurrent.Future;
import javax.annotation.Nullable;

/**
 * Performs The TSI Handshake. When the handshake is complete, it fires a user event with a {@link
 * TsiHandshakeCompletionEvent} indicating the result of the handshake.
 */
public final class TsiHandshakeHandler extends ByteToMessageDecoder {
  private static final int HANDSHAKE_FRAME_SIZE = 1024;

  private final NettyTsiHandshaker handshaker;
  private boolean started;

  /**
   * This buffer doesn't store any state. We just hold onto it in case we end up allocating a buffer
   * that ends up being unused.
   */
  private ByteBuf buffer;

  public TsiHandshakeHandler(NettyTsiHandshaker handshaker) {
    this.handshaker = checkNotNull(handshaker);
  }

  /**
   * Event that is fired once the TSI handshake is complete, which may be because it was successful
   * or there was an error.
   */
  public static final class TsiHandshakeCompletionEvent {

    private final Throwable cause;
    private final TsiPeer peer;
    private final Object context;
    private final TsiFrameProtector protector;

    /** Creates a new event that indicates a successful handshake. */
    @VisibleForTesting
    TsiHandshakeCompletionEvent(
        TsiFrameProtector protector, TsiPeer peer, @Nullable Object peerObject) {
      this.cause = null;
      this.peer = checkNotNull(peer);
      this.protector = checkNotNull(protector);
      this.context = peerObject;
    }

    /** Creates a new event that indicates an unsuccessful handshake/. */
    TsiHandshakeCompletionEvent(Throwable cause) {
      this.cause = checkNotNull(cause);
      this.peer = null;
      this.protector = null;
      this.context = null;
    }

    /** Return {@code true} if the handshake was successful. */
    public boolean isSuccess() {
      return cause == null;
    }

    /**
     * Return the {@link Throwable} if {@link #isSuccess()} returns {@code false} and so the
     * handshake failed.
     */
    @Nullable
    public Throwable cause() {
      return cause;
    }

    @Nullable
    public TsiPeer peer() {
      return peer;
    }

    @Nullable
    public Object context() {
      return context;
    }

    @Nullable
    TsiFrameProtector protector() {
      return protector;
    }
  }

  @Override
  public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
    maybeStart(ctx);
    super.handlerAdded(ctx);
  }

  @Override
  public void channelActive(ChannelHandlerContext ctx) throws Exception {
    maybeStart(ctx);
    super.channelActive(ctx);
  }

  @Override
  public void handlerRemoved0(ChannelHandlerContext ctx) throws Exception {
    close();
    super.handlerRemoved0(ctx);
  }

  @Override
  public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
    ctx.fireUserEventTriggered(new TsiHandshakeCompletionEvent(cause));
    super.exceptionCaught(ctx, cause);
  }

  @Override
  protected void decodeLast(ChannelHandlerContext ctx, ByteBuf in, List<Object> out)
      throws Exception {
    // TODO: Not sure why override is needed. Investigate if it can be removed.
    decode(ctx, in, out);
  }

  @Override
  protected void decode(ChannelHandlerContext ctx, ByteBuf in, List<Object> out) throws Exception {
    // Process the data. If we need to send more data, do so now.
    if (handshaker.processBytesFromPeer(in) && handshaker.isInProgress()) {
      sendHandshake(ctx);
    }

    // If the handshake is complete, transition to the framing state.
    if (!handshaker.isInProgress()) {
      try {
        ctx.pipeline().remove(this);
        ctx.fireUserEventTriggered(
            new TsiHandshakeCompletionEvent(
                handshaker.createFrameProtector(ctx.alloc()),
                handshaker.extractPeer(),
                handshaker.extractPeerObject()));
        // No need to do anything with the in buffer, it will be re added to the pipeline when this
        // handler is removed.
      } finally {
        close();
      }
    }
  }

  private void maybeStart(ChannelHandlerContext ctx) {
    if (!started && ctx.channel().isActive()) {
      started = true;
      sendHandshake(ctx);
    }
  }

  /** Sends as many bytes as are available from the handshaker to the remote peer. */
  private void sendHandshake(ChannelHandlerContext ctx) {
    boolean needToFlush = false;

    // Iterate until there is nothing left to write.
    while (true) {
      buffer = getOrCreateBuffer(ctx.alloc());
      try {
        handshaker.getBytesToSendToPeer(buffer);
      } catch (GeneralSecurityException e) {
        throw new RuntimeException(e);
      }
      if (!buffer.isReadable()) {
        break;
      }

      needToFlush = true;
      @SuppressWarnings("unused") // go/futurereturn-lsc
      Future<?> possiblyIgnoredError = ctx.write(buffer);
      buffer = null;
    }

    // If something was written, flush.
    if (needToFlush) {
      ctx.flush();
    }
  }

  private ByteBuf getOrCreateBuffer(ByteBufAllocator alloc) {
    if (buffer == null) {
      buffer = alloc.buffer(HANDSHAKE_FRAME_SIZE);
    }
    return buffer;
  }

  private void close() {
    ReferenceCountUtil.safeRelease(buffer);
    buffer = null;
  }
}
