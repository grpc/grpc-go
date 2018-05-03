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
import static com.google.common.base.Preconditions.checkState;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.alts.internal.TsiFrameProtector.Consumer;
import io.grpc.alts.internal.TsiHandshakeHandler.TsiHandshakeCompletionEvent;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelException;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelOutboundHandler;
import io.netty.channel.ChannelPromise;
import io.netty.channel.PendingWriteQueue;
import io.netty.handler.codec.ByteToMessageDecoder;
import java.net.SocketAddress;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Future;

/**
 * Encrypts and decrypts TSI Frames. Writes are buffered here until {@link #flush} is called. Writes
 * must not be made before the TSI handshake is complete.
 */
public final class TsiFrameHandler extends ByteToMessageDecoder implements ChannelOutboundHandler {

  private TsiFrameProtector protector;
  private PendingWriteQueue pendingUnprotectedWrites;

  public TsiFrameHandler() {}

  @Override
  public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
    super.handlerAdded(ctx);
    assert pendingUnprotectedWrites == null;
    pendingUnprotectedWrites = new PendingWriteQueue(checkNotNull(ctx));
  }

  @Override
  public void userEventTriggered(ChannelHandlerContext ctx, Object event) throws Exception {
    if (event instanceof TsiHandshakeCompletionEvent) {
      TsiHandshakeCompletionEvent tsiEvent = (TsiHandshakeCompletionEvent) event;
      if (tsiEvent.isSuccess()) {
        setProtector(tsiEvent.protector());
      }
      // Ignore errors.  Another handler in the pipeline must handle TSI Errors.
    }
    // Keep propagating the message, as others may want to read it.
    super.userEventTriggered(ctx, event);
  }

  @VisibleForTesting
  void setProtector(TsiFrameProtector protector) {
    checkState(this.protector == null);
    this.protector = checkNotNull(protector);
  }

  @Override
  protected void decode(ChannelHandlerContext ctx, ByteBuf in, List<Object> out) throws Exception {
    checkState(protector != null, "Cannot read frames while the TSI handshake is in progress");
    protector.unprotect(in, out, ctx.alloc());
  }

  @Override
  public void write(ChannelHandlerContext ctx, Object message, ChannelPromise promise)
      throws Exception {
    checkState(protector != null, "Cannot write frames while the TSI handshake is in progress");
    ByteBuf msg = (ByteBuf) message;
    if (!msg.isReadable()) {
      // Nothing to encode.
      @SuppressWarnings("unused") // go/futurereturn-lsc
      Future<?> possiblyIgnoredError = promise.setSuccess();
      return;
    }

    // Just add the message to the pending queue. We'll write it on the next flush.
    pendingUnprotectedWrites.add(msg, promise);
  }

  @Override
  public void handlerRemoved0(ChannelHandlerContext ctx) throws Exception {
    if (!pendingUnprotectedWrites.isEmpty()) {
      pendingUnprotectedWrites.removeAndFailAll(
          new ChannelException("Pending write on removal of TSI handler"));
    }
  }

  @Override
  public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
    pendingUnprotectedWrites.removeAndFailAll(cause);
    super.exceptionCaught(ctx, cause);
  }

  @Override
  public void bind(ChannelHandlerContext ctx, SocketAddress localAddress, ChannelPromise promise) {
    ctx.bind(localAddress, promise);
  }

  @Override
  public void connect(
      ChannelHandlerContext ctx,
      SocketAddress remoteAddress,
      SocketAddress localAddress,
      ChannelPromise promise) {
    ctx.connect(remoteAddress, localAddress, promise);
  }

  @Override
  public void disconnect(ChannelHandlerContext ctx, ChannelPromise promise) {
    ctx.disconnect(promise);
  }

  @Override
  public void close(ChannelHandlerContext ctx, ChannelPromise promise) {
    ctx.close(promise);
  }

  @Override
  public void deregister(ChannelHandlerContext ctx, ChannelPromise promise) {
    ctx.deregister(promise);
  }

  @Override
  public void read(ChannelHandlerContext ctx) {
    ctx.read();
  }

  @Override
  public void flush(final ChannelHandlerContext ctx) throws GeneralSecurityException {
    checkState(protector != null, "Cannot write frames while the TSI handshake is in progress");
    final ProtectedPromise aggregatePromise =
        new ProtectedPromise(ctx.channel(), ctx.executor(), pendingUnprotectedWrites.size());

    List<ByteBuf> bufs = new ArrayList<>(pendingUnprotectedWrites.size());

    if (pendingUnprotectedWrites.isEmpty()) {
      // Return early if there's nothing to write. Otherwise protector.protectFlush() below may
      // not check for "no-data" and go on writing the 0-byte "data" to the socket with the
      // protection framing.
      return;
    }
    // Drain the unprotected writes.
    while (!pendingUnprotectedWrites.isEmpty()) {
      ByteBuf in = (ByteBuf) pendingUnprotectedWrites.current();
      bufs.add(in.retain());
      // Remove and release the buffer and add its promise to the aggregate.
      aggregatePromise.addUnprotectedPromise(pendingUnprotectedWrites.remove());
    }

    protector.protectFlush(
        bufs,
        new Consumer<ByteBuf>() {
          @Override
          public void accept(ByteBuf b) {
            ctx.writeAndFlush(b, aggregatePromise.newPromise());
          }
        },
        ctx.alloc());

    // We're done writing, start the flow of promise events.
    @SuppressWarnings("unused") // go/futurereturn-lsc
    Future<?> possiblyIgnoredError = aggregatePromise.doneAllocatingPromises();
  }
}
