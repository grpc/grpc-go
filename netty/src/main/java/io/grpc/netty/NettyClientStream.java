/*
 * Copyright 2015, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.netty;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.netty.buffer.Unpooled.EMPTY_BUFFER;

import com.google.common.io.BaseEncoding;
import io.grpc.Attributes;
import io.grpc.InternalKnownTransport;
import io.grpc.InternalMethodDescriptor;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.AbstractClientStream2;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.Http2ClientStreamTransportState;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.WritableBuffer;
import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Stream;
import io.netty.util.AsciiString;
import javax.annotation.Nullable;

/**
 * Client stream for a Netty transport. Must only be called from the sending application
 * thread.
 */
class NettyClientStream extends AbstractClientStream2 {
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
      TransportState state, MethodDescriptor<?, ?> method, Metadata headers,
      Channel channel, AsciiString authority, AsciiString scheme, AsciiString userAgent,
      StatsTraceContext statsTraceCtx) {
    super(new NettyWritableBufferAllocator(channel.alloc()),
        statsTraceCtx,
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

  private class Sink implements AbstractClientStream2.Sink {
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
      headers.discardAll(GrpcUtil.USER_AGENT_KEY);
      Http2Headers http2Headers = Utils.convertClientHeaders(headers, scheme, defaultPath,
          authority, httpMethod, userAgent);

      ChannelFutureListener failureListener = new ChannelFutureListener() {
        @Override
        public void operationComplete(ChannelFuture future) throws Exception {
          if (!future.isSuccess()) {
            // Stream creation failed. Close the stream if not already closed.
            Status s = transportState().statusFromFailedFuture(future);
            transportState().transportReportStatus(s, true, new Metadata());
          }
        }
      };

      // Write the command requesting the creation of the stream.
      writeQueue.enqueue(new CreateStreamCommand(http2Headers, transportState(), get),
          !method.getType().clientSendsOneMessage() || get).addListener(failureListener);
    }

    @Override
    public void writeFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
      ByteBuf bytebuf = frame == null ? EMPTY_BUFFER : ((NettyWritableBuffer) frame).bytebuf();
      final int numBytes = bytebuf.readableBytes();
      if (numBytes > 0) {
        // Add the bytes to outbound flow control.
        onSendingBytes(numBytes);
        writeQueue.enqueue(
            new SendGrpcFrameCommand(transportState(), bytebuf, endOfStream),
            channel.newPromise().addListener(new ChannelFutureListener() {
              @Override
              public void operationComplete(ChannelFuture future) throws Exception {
                if (future.isSuccess()) {
                  // Remove the bytes from outbound flow control, optionally notifying
                  // the client that they can send more bytes.
                  transportState().onSentBytes(numBytes);
                }
              }
            }), flush);
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
    private int id;
    private Http2Stream http2Stream;

    public TransportState(NettyClientHandler handler, int maxMessageSize,
        StatsTraceContext statsTraceCtx) {
      super(maxMessageSize, statsTraceCtx);
      this.handler = checkNotNull(handler, "handler");
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
    }

    /**
     * Gets the underlying Netty {@link Http2Stream} for this stream.
     */
    @Nullable
    public Http2Stream http2Stream() {
      return http2Stream;
    }

    /**
     * Intended to be overriden by NettyClientTransport, which has more information about failures.
     * May only be called from event loop.
     */
    protected abstract Status statusFromFailedFuture(ChannelFuture f);

    @Override
    protected void http2ProcessingFailed(Status status, Metadata trailers) {
      transportReportStatus(status, false, trailers);
      handler.getWriteQueue().enqueue(new CancelClientStreamCommand(this, status), true);
    }

    @Override
    public void bytesRead(int processedBytes) {
      handler.returnProcessedBytes(http2Stream, processedBytes);
      handler.getWriteQueue().scheduleFlush();
    }

    @Override
    protected void deframeFailed(Throwable cause) {
      http2ProcessingFailed(Status.fromThrowable(cause), new Metadata());
    }

    void transportHeadersReceived(Http2Headers headers, boolean endOfStream) {
      if (endOfStream) {
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
