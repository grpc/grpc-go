/*
 * Copyright 2015 The gRPC Authors
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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.netty.buffer.Unpooled.EMPTY_BUFFER;

import com.google.common.base.Preconditions;
import com.google.common.io.BaseEncoding;
import io.grpc.Attributes;
import io.grpc.InternalKnownTransport;
import io.grpc.InternalMethodDescriptor;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.AbstractClientStream;
import io.grpc.internal.Http2ClientStreamTransportState;
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
import io.netty.util.AsciiString;
import javax.annotation.Nullable;

/**
 * Client stream for a Netty transport. Must only be called from the sending application
 * thread.
 */
class NettyClientStream extends AbstractClientStream {
  private static final InternalMethodDescriptor methodDescriptorAccessor =
      new InternalMethodDescriptor(InternalKnownTransport.NETTY);

  private final Sink sink = new Sink();
  private final TransportState state;
  private final WriteQueue writeQueue;
  private final MethodDescriptor<?, ?> method;
  private final Channel channel;
  private AsciiString authority;
  private final AsciiString scheme;
  private final AsciiString userAgent;

  NettyClientStream(
      TransportState state,
      MethodDescriptor<?, ?> method,
      Metadata headers,
      Channel channel,
      AsciiString authority,
      AsciiString scheme,
      AsciiString userAgent,
      StatsTraceContext statsTraceCtx,
      TransportTracer transportTracer) {
    super(
        new NettyWritableBufferAllocator(channel.alloc()),
        statsTraceCtx,
        transportTracer,
        headers,
        useGet(method));
    this.state = checkNotNull(state, "transportState");
    this.writeQueue = state.handler.getWriteQueue();
    this.method = checkNotNull(method, "method");
    this.channel = checkNotNull(channel, "channel");
    this.authority = checkNotNull(authority, "authority");
    this.scheme = checkNotNull(scheme, "scheme");
    this.userAgent = userAgent;
  }

  @Override
  protected TransportState transportState() {
    return state;
  }

  @Override
  protected Sink abstractClientStreamSink() {
    return sink;
  }

  @Override
  public void setAuthority(String authority) {
    this.authority = AsciiString.of(checkNotNull(authority, "authority"));
  }

  @Override
  public Attributes getAttributes() {
    return state.handler.getAttributes();
  }

  private static boolean useGet(MethodDescriptor<?, ?> method) {
    return method.isSafe();
  }

  private class Sink implements AbstractClientStream.Sink {
    @SuppressWarnings("BetaApi") // BaseEncoding is stable in Guava 20.0
    @Override
    public void writeHeaders(Metadata headers, byte[] requestPayload) {
      // Convert the headers into Netty HTTP/2 headers.
      AsciiString defaultPath = (AsciiString) methodDescriptorAccessor.geRawMethodName(method);
      if (defaultPath == null) {
        defaultPath = new AsciiString("/" + method.getFullMethodName());
        methodDescriptorAccessor.setRawMethodName(method, defaultPath);
      }
      boolean get = (requestPayload != null);
      AsciiString httpMethod;
      if (get) {
        // Forge the query string
        // TODO(ericgribkoff) Add the key back to the query string
        defaultPath =
            new AsciiString(defaultPath + "?" + BaseEncoding.base64().encode(requestPayload));
        httpMethod = Utils.HTTP_GET_METHOD;
      } else {
        httpMethod = Utils.HTTP_METHOD;
      }
      Http2Headers http2Headers = Utils.convertClientHeaders(headers, scheme, defaultPath,
          authority, httpMethod, userAgent);

      ChannelFutureListener failureListener = new ChannelFutureListener() {
        @Override
        public void operationComplete(ChannelFuture future) throws Exception {
          if (!future.isSuccess()) {
            // Stream creation failed. Close the stream if not already closed.
            // When the channel is shutdown, the lifecycle manager has a better view of the failure,
            // especially before negotiation completes (because the negotiator commonly doesn't
            // receive the execeptionCaught because NettyClientHandler does not propagate it).
            Status s = transportState().handler.getLifecycleManager().getShutdownStatus();
            if (s == null) {
              s = transportState().statusFromFailedFuture(future);
            }
            transportState().transportReportStatus(s, true, new Metadata());
          }
        }
      };

      // Write the command requesting the creation of the stream.
      writeQueue.enqueue(new CreateStreamCommand(http2Headers, transportState(), get),
          !method.getType().clientSendsOneMessage() || get).addListener(failureListener);
    }

    @Override
    public void writeFrame(
        WritableBuffer frame, boolean endOfStream, boolean flush, final int numMessages) {
      Preconditions.checkArgument(numMessages >= 0);
      ByteBuf bytebuf = frame == null ? EMPTY_BUFFER : ((NettyWritableBuffer) frame).bytebuf();
      final int numBytes = bytebuf.readableBytes();
      if (numBytes > 0) {
        // Add the bytes to outbound flow control.
        onSendingBytes(numBytes);
        writeQueue.enqueue(new SendGrpcFrameCommand(transportState(), bytebuf, endOfStream), flush)
            .addListener(new ChannelFutureListener() {
              @Override
              public void operationComplete(ChannelFuture future) throws Exception {
                // If the future succeeds when http2stream is null, the stream has been cancelled
                // before it began and Netty is purging pending writes from the flow-controller.
                if (future.isSuccess() && transportState().http2Stream() != null) {
                  // Remove the bytes from outbound flow control, optionally notifying
                  // the client that they can send more bytes.
                  transportState().onSentBytes(numBytes);
                  NettyClientStream.this.getTransportTracer().reportMessageSent(numMessages);
                }
              }
            });
      } else {
        // The frame is empty and will not impact outbound flow control. Just send it.
        writeQueue.enqueue(new SendGrpcFrameCommand(transportState(), bytebuf, endOfStream), flush);
      }
    }

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
    public void cancel(Status status) {
      writeQueue.enqueue(new CancelClientStreamCommand(transportState(), status), true);
    }
  }

  /** This should only called from the transport thread. */
  public abstract static class TransportState extends Http2ClientStreamTransportState
      implements StreamIdHolder {
    private final NettyClientHandler handler;
    private final EventLoop eventLoop;
    private int id;
    private Http2Stream http2Stream;

    public TransportState(
        NettyClientHandler handler,
        EventLoop eventLoop,
        int maxMessageSize,
        StatsTraceContext statsTraceCtx,
        TransportTracer transportTracer) {
      super(maxMessageSize, statsTraceCtx, transportTracer);
      this.handler = checkNotNull(handler, "handler");
      this.eventLoop = checkNotNull(eventLoop, "eventLoop");
    }

    @Override
    public int id() {
      return id;
    }

    public void setId(int id) {
      checkArgument(id > 0, "id must be positive");
      this.id = id;
    }

    /**
     * Sets the underlying Netty {@link Http2Stream} for this stream. This must be called in the
     * context of the transport thread.
     */
    public void setHttp2Stream(Http2Stream http2Stream) {
      checkNotNull(http2Stream, "http2Stream");
      checkState(this.http2Stream == null, "Can only set http2Stream once");
      this.http2Stream = http2Stream;

      // Now that the stream has actually been initialized, call the listener's onReady callback if
      // appropriate.
      onStreamAllocated();
      getTransportTracer().reportLocalStreamStarted();
    }

    /**
     * Gets the underlying Netty {@link Http2Stream} for this stream.
     */
    @Nullable
    public Http2Stream http2Stream() {
      return http2Stream;
    }

    /**
     * Intended to be overridden by NettyClientTransport, which has more information about failures.
     * May only be called from event loop.
     */
    protected abstract Status statusFromFailedFuture(ChannelFuture f);

    @Override
    protected void http2ProcessingFailed(Status status, boolean stopDelivery, Metadata trailers) {
      transportReportStatus(status, stopDelivery, trailers);
      handler.getWriteQueue().enqueue(new CancelClientStreamCommand(this, status), true);
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
      http2ProcessingFailed(Status.fromThrowable(cause), true, new Metadata());
    }

    void transportHeadersReceived(Http2Headers headers, boolean endOfStream) {
      if (endOfStream) {
        if (!isOutboundClosed()) {
          handler.getWriteQueue().enqueue(new CancelClientStreamCommand(this, null), true);
        }
        transportTrailersReceived(Utils.convertTrailers(headers));
      } else {
        transportHeadersReceived(Utils.convertHeaders(headers));
      }
    }

    void transportDataReceived(ByteBuf frame, boolean endOfStream) {
      transportDataReceived(new NettyReadableBuffer(frame.retain()), endOfStream);
    }
  }
}
