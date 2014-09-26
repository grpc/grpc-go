package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.StreamState.CLOSED;
import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_HEADER;
import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_PROTORPC;
import static com.google.net.stubby.newtransport.netty.Utils.GRPC_STATUS_HEADER;
import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.AbstractClientStream;
import com.google.net.stubby.newtransport.ClientStreamListener;
import com.google.net.stubby.newtransport.GrpcDeframer;
import com.google.net.stubby.newtransport.HttpUtil;
import com.google.net.stubby.newtransport.MessageDeframer2;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.http.HttpResponseStatus;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.Http2Headers;

import java.nio.ByteBuffer;

import javax.annotation.Nullable;

/**
 * Client stream for a Netty transport.
 */
class NettyClientStream extends AbstractClientStream implements NettyStream {
  public static final int PENDING_STREAM_ID = -1;

  private volatile int id = PENDING_STREAM_ID;
  private final Channel channel;
  private final GrpcDeframer deframer;
  private final MessageDeframer2 deframer2;
  private final WindowUpdateManager windowUpdateManager;
  private Status responseStatus = Status.UNKNOWN;
  private boolean isGrpcResponse;
  private StringBuilder nonGrpcErrorMessage = new StringBuilder();

  NettyClientStream(ClientStreamListener listener, Channel channel,
      DefaultHttp2InboundFlowController inboundFlow) {
    super(listener);
    this.channel = Preconditions.checkNotNull(channel, "channel");
    if (!GRPC_V2_PROTOCOL) {
      this.deframer = new GrpcDeframer(new NettyDecompressor(channel.alloc()),
          inboundMessageHandler(), channel.eventLoop());
      this.deframer2 = null;
    } else {
      this.deframer = null;
      this.deframer2 = new MessageDeframer2(inboundMessageHandler(), channel.eventLoop());
    }
    windowUpdateManager = new WindowUpdateManager(channel, inboundFlow);
  }

  /**
   * Returns the HTTP/2 ID for this stream.
   */
  @Override
  public int id() {
    return id;
  }

  void id(int id) {
    this.id = id;
    windowUpdateManager.streamId(id);
  }

  @Override
  public void cancel() {
    outboundPhase = Phase.STATUS;

    // Send the cancel command to the handler.
    channel.writeAndFlush(new CancelStreamCommand(this));
  }

  /**
   * Called in the channel thread to process headers received from the server.
   */
  public void inboundHeadersRecieved(Http2Headers headers, boolean endOfStream) {
    responseStatus = responseStatus(headers, responseStatus);
    isGrpcResponse = isGrpcResponse(headers, responseStatus);
    if (endOfStream) {
      if (isGrpcResponse) {
        // TODO(user): call stashTrailers() as appropriate, then provide endOfStream to
        // deframer.
        setStatus(responseStatus, new Metadata.Trailers());
      } else {
        setStatus(responseStatus, new Metadata.Trailers());
      }
    }
  }

  /**
   * Called in the channel thread to process the content of an inbound DATA frame.
   *
   * @param frame the inbound HTTP/2 DATA frame. If this buffer is not used immediately, it must be
   *        retained.
   */
  @Override
  public void inboundDataReceived(ByteBuf frame, boolean endOfStream) {
    Preconditions.checkNotNull(frame, "frame");
    if (state() == CLOSED) {
      return;
    }

    if (isGrpcResponse) {
      // Retain the ByteBuf until it is released by the deframer.
      if (!GRPC_V2_PROTOCOL) {
        deframer.deframe(new NettyBuffer(frame.retain()), endOfStream);
      } else {
        deframer2.deframe(new NettyBuffer(frame.retain()), endOfStream);
      }
    } else {
      // It's not a GRPC response, assume that the frame contains a text-based error message.

      // TODO(user): Should we send RST_STREAM as well?
      // TODO(user): is there a better way to handle large non-GRPC error messages?
      nonGrpcErrorMessage.append(frame.toString(UTF_8));

      if (endOfStream) {
        String msg = nonGrpcErrorMessage.toString();
        setStatus(responseStatus.withDescription(msg), new Metadata.Trailers());
      }
    }
  }

  @Override
  protected void sendFrame(ByteBuffer frame, boolean endOfStream) {
    SendGrpcFrameCommand cmd = new SendGrpcFrameCommand(id(),
        Utils.toByteBuf(channel.alloc(), frame), endOfStream);
    channel.writeAndFlush(cmd);
  }

  @Override
  protected void disableWindowUpdate(@Nullable ListenableFuture<Void> processingFuture) {
    windowUpdateManager.disableWindowUpdate(processingFuture);
  }

  /**
   * Determines whether or not the response from the server is a GRPC response.
   */
  private boolean isGrpcResponse(Http2Headers headers, Status status) {
    if (isGrpcResponse) {
      // Already verified that it's a gRPC response.
      return true;
    }

    if (headers == null) {
      // No headers, not a GRPC response.
      return false;
    }

    // GRPC responses should always return OK. Updated this code once b/16290036 is fixed.
    if (status.isOk()) {
      // ESF currently returns the wrong content-type for grpc.
      return true;
    }

    AsciiString contentType = headers.get(CONTENT_TYPE_HEADER);
    return CONTENT_TYPE_PROTORPC.equalsIgnoreCase(contentType);
  }

  /**
   * Parses the response status and converts it to a transport code.
   */
  private static Status responseStatus(Http2Headers headers, Status defaultValue) {
    if (headers == null) {
      return defaultValue;
    }

    // First, check to see if we found a v2 protocol grpc-status header.
    AsciiString grpcStatus = headers.get(GRPC_STATUS_HEADER);
    if (grpcStatus != null) {
      return Status.fromCodeValue(grpcStatus.parseInt());
    }

    // Next, check the HTTP/2 status.
    AsciiString statusLine = headers.status();
    if (statusLine == null) {
      return defaultValue;
    }
    HttpResponseStatus status = HttpResponseStatus.parseLine(statusLine);
    return HttpUtil.httpStatusToGrpcStatus(status.code());
  }
}
