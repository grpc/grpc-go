/*
 * Copyright 2014 The gRPC Authors
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

import com.google.common.base.Preconditions;
import io.grpc.Attributes;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.AbstractServerStream;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.TransportTracer;
import io.grpc.internal.WritableBuffer;
import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.EventLoop;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Stream;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Server stream for a Netty HTTP2 transport. Must only be called from the sending application
 * thread.
 */
class NettyServerStream extends AbstractServerStream {
  private static final Logger log = Logger.getLogger(NettyServerStream.class.getName());

  private final Sink sink = new Sink();
  private final TransportState state;
  private final Channel channel;
  private final WriteQueue writeQueue;
  private final Attributes attributes;
  private final String authority;
  private final TransportTracer transportTracer;

  public NettyServerStream(
      Channel channel,
      TransportState state,
      Attributes transportAttrs,
      String authority,
      StatsTraceContext statsTraceCtx,
      TransportTracer transportTracer) {
    super(new NettyWritableBufferAllocator(channel.alloc()), statsTraceCtx);
    this.state = checkNotNull(state, "transportState");
    this.channel = checkNotNull(channel, "channel");
    this.writeQueue = state.handler.getWriteQueue();
    this.attributes = checkNotNull(transportAttrs);
    this.authority = authority;
    this.transportTracer = checkNotNull(transportTracer, "transportTracer");
  }

  @Override
  protected TransportState transportState() {
    return state;
  }

  @Override
  protected Sink abstractServerStreamSink() {
    return sink;
  }

  @Override
  public Attributes getAttributes() {
    return attributes;
  }

  @Override
  public String getAuthority() {
    return authority;
  }

  private class Sink implements AbstractServerStream.Sink {
    @Override
    public void request(final int numMessages) {
      if (channel.eventLoop().inEventLoop()) {
        // Processing data read in the event loop so can call into the deframer immediately
        transportState().requestMessagesFromDeframer(numMessages);
      } else {
        channel.eventLoop().execute(new Runnable() {
          @Override
          public void run() {
            transportState().requestMessagesFromDeframer(numMessages);
          }
        });
      }
    }

    @Override
    public void writeHeaders(Metadata headers) {
      writeQueue.enqueue(
          SendResponseHeadersCommand.createHeaders(
              transportState(),
              Utils.convertServerHeaders(headers)),
          true);
    }

    @Override
    public void writeFrame(WritableBuffer frame, boolean flush, final int numMessages) {
      Preconditions.checkArgument(numMessages >= 0);
      if (frame == null) {
        writeQueue.scheduleFlush();
        return;
      }
      ByteBuf bytebuf = ((NettyWritableBuffer) frame).bytebuf();
      final int numBytes = bytebuf.readableBytes();
      // Add the bytes to outbound flow control.
      onSendingBytes(numBytes);
      writeQueue.enqueue(new SendGrpcFrameCommand(transportState(), bytebuf, false), flush)
          .addListener(new ChannelFutureListener() {
            @Override
            public void operationComplete(ChannelFuture future) throws Exception {
              // Remove the bytes from outbound flow control, optionally notifying
              // the client that they can send more bytes.
              transportState().onSentBytes(numBytes);
              if (future.isSuccess()) {
                transportTracer.reportMessageSent(numMessages);
              }
            }
          });
    }

    @Override
    public void writeTrailers(Metadata trailers, boolean headersSent, Status status) {
      Http2Headers http2Trailers = Utils.convertTrailers(trailers, headersSent);
      writeQueue.enqueue(
          SendResponseHeadersCommand.createTrailers(transportState(), http2Trailers, status),
          true);
    }

    @Override
    public void cancel(Status status) {
      writeQueue.enqueue(new CancelServerStreamCommand(transportState(), status), true);
    }
  }

  /** This should only called from the transport thread. */
  public static class TransportState extends AbstractServerStream.TransportState
      implements StreamIdHolder {
    private final Http2Stream http2Stream;
    private final NettyServerHandler handler;
    private final EventLoop eventLoop;

    public TransportState(
        NettyServerHandler handler,
        EventLoop eventLoop,
        Http2Stream http2Stream,
        int maxMessageSize,
        StatsTraceContext statsTraceCtx,
        TransportTracer transportTracer) {
      super(maxMessageSize, statsTraceCtx, transportTracer);
      this.http2Stream = checkNotNull(http2Stream, "http2Stream");
      this.handler = checkNotNull(handler, "handler");
      this.eventLoop = eventLoop;
    }

    @Override
    public void runOnTransportThread(final Runnable r) {
      if (eventLoop.inEventLoop()) {
        r.run();
      } else {
        eventLoop.execute(r);
      }
    }

    @Override
    public void bytesRead(int processedBytes) {
      handler.returnProcessedBytes(http2Stream, processedBytes);
      handler.getWriteQueue().scheduleFlush();
    }

    @Override
    public void deframeFailed(Throwable cause) {
      log.log(Level.WARNING, "Exception processing message", cause);
      Status status = Status.fromThrowable(cause);
      transportReportStatus(status);
      handler.getWriteQueue().enqueue(new CancelServerStreamCommand(this, status), true);
    }

    void inboundDataReceived(ByteBuf frame, boolean endOfStream) {
      super.inboundDataReceived(new NettyReadableBuffer(frame.retain()), endOfStream);
    }

    @Override
    public int id() {
      return http2Stream.id();
    }
  }
}
