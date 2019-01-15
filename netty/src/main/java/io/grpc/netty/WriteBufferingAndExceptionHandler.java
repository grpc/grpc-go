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

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.Status;
import io.netty.channel.ChannelDuplexHandler;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.util.ReferenceCountUtil;
import java.net.SocketAddress;
import java.util.ArrayDeque;
import java.util.Queue;

/**
 * Buffers all writes until either {@link #writeBufferedAndRemove(ChannelHandlerContext)} or
 * {@link #fail(ChannelHandlerContext, Throwable)} is called. This handler allows us to
 * write to a {@link io.netty.channel.Channel} before we are allowed to write to it officially
 * i.e.  before it's active or the TLS Handshake is complete.
 */
final class WriteBufferingAndExceptionHandler extends ChannelDuplexHandler {

  private final Queue<ChannelWrite> bufferedWrites = new ArrayDeque<>();
  private final ChannelHandler next;
  private boolean writing;
  private boolean flushRequested;
  private Throwable failCause;

  WriteBufferingAndExceptionHandler(ChannelHandler next) {
    this.next = checkNotNull(next, "next");
  }

  @Override
  public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
    ctx.pipeline().addBefore(ctx.name(), null, next);
    super.handlerAdded(ctx);
  }

  @Override
  public void handlerRemoved(ChannelHandlerContext ctx) throws Exception {
    if (!bufferedWrites.isEmpty()) {
      Status status = Status.INTERNAL.withDescription("Buffer removed before draining writes");
      failWrites(status.asRuntimeException());
    }
    super.handlerRemoved(ctx);
  }

  /**
   * If this channel becomes inactive, then notify all buffered writes that we failed.
   */
  @Override
  public void channelInactive(ChannelHandlerContext ctx) {
    Status status = Status.UNAVAILABLE.withDescription(
        "Connection closed while performing protocol negotiation for " + ctx.pipeline().names());
    failWrites(status.asRuntimeException());
  }

  @Override
  public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) {
    Status status = Utils.statusFromThrowable(cause);
    failWrites(status.asRuntimeException());
    if (ctx.channel().isActive()) {
      ctx.close();
    }
  }

  /**
   * Buffers the write until either {@link #writeBufferedAndRemove(ChannelHandlerContext)} is
   * called, or we have somehow failed. If we have already failed in the past, then the write
   * will fail immediately.
   */
  @Override
  @SuppressWarnings("FutureReturnValueIgnored")
  public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise) {
    if (failCause != null) {
      promise.setFailure(failCause);
      ReferenceCountUtil.release(msg);
    } else {
      bufferedWrites.add(new ChannelWrite(msg, promise));
    }
  }

  /**
   * Connect failures do not show up as {@link #channelInactive} or {@link #exceptionCaught}, so
   * it needs to be watched.
   */
  @Override
  public void connect(
      ChannelHandlerContext ctx,
      SocketAddress remoteAddress,
      SocketAddress localAddress,
      ChannelPromise promise) throws Exception {
    final class ConnectListener implements ChannelFutureListener {
      @Override
      public void operationComplete(ChannelFuture future) {
        if (!future.isSuccess()) {
          failWrites(future.cause());
        }
      }
    }

    super.connect(ctx, remoteAddress, localAddress, promise);
    promise.addListener(new ConnectListener());
  }

  /**
   * Calls to this method will not trigger an immediate flush. The flush will be deferred until
   * {@link #writeBufferedAndRemove(ChannelHandlerContext)}.
   */
  @Override
  public void flush(ChannelHandlerContext ctx) {
    /**
     * Swallowing any flushes is not only an optimization but also required
     * for the SslHandler to work correctly. If the SslHandler receives multiple
     * flushes while the handshake is still ongoing, then the handshake "randomly"
     * times out. Not sure at this point why this is happening. Doing a single flush
     * seems to work but multiple flushes don't ...
     */
    flushRequested = true;
  }

  /**
   * If we are still performing protocol negotiation, then this will propagate failures to all
   * buffered writes.
   */
  @Override
  public void close(ChannelHandlerContext ctx, ChannelPromise future) throws Exception {
    Status status = Status.UNAVAILABLE.withDescription(
        "Connection closing while performing protocol negotiation for " + ctx.pipeline().names());
    failWrites(status.asRuntimeException());
    super.close(ctx, future);
  }

  @SuppressWarnings("FutureReturnValueIgnored")
  final void writeBufferedAndRemove(ChannelHandlerContext ctx) {
    // TODO(carl-mastrangelo): remove the isActive check and just fail if not yet ready.
    if (!ctx.channel().isActive() || writing) {
      return;
    }
    // Make sure that method can't be reentered, so that the ordering
    // in the queue can't be messed up.
    writing = true;
    while (!bufferedWrites.isEmpty()) {
      ChannelWrite write = bufferedWrites.poll();
      ctx.write(write.msg, write.promise);
    }
    if (flushRequested) {
      ctx.flush();
    }
    // Removal has to happen last as the above writes will likely trigger
    // new writes that have to be added to the end of queue in order to not
    // mess up the ordering.
    ctx.pipeline().remove(this);
  }

  /**
   * Propagate failures to all buffered writes.
   */
  @SuppressWarnings("FutureReturnValueIgnored")
  private void failWrites(Throwable cause) {
    if (failCause == null) {
      failCause = cause;
    }
    while (!bufferedWrites.isEmpty()) {
      ChannelWrite write = bufferedWrites.poll();
      write.promise.setFailure(cause);
      ReferenceCountUtil.release(write.msg);
    }
  }

  private static final class ChannelWrite {
    final Object msg;
    final ChannelPromise promise;

    ChannelWrite(Object msg, ChannelPromise promise) {
      this.msg = msg;
      this.promise = promise;
    }
  }
}
