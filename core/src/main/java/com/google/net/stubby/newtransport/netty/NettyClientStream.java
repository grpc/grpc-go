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
import com.google.net.stubby.newtransport.Buffers;
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
  private boolean seenHeaders;
  private StringBuilder nonGrpcErrorMessage;

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
    if (!seenHeaders) {
      seenHeaders = true;
      isGrpcResponse = isGrpcResponse(headers);
      // If endOfStream, we have trailers and no "headers" were sent.
      if (!endOfStream && GRPC_V2_PROTOCOL) {
        deframer2.delayProcessing(receiveHeaders(Utils.convertHeaders(headers)));
      }
    }
    if (endOfStream) {
      if (GRPC_V2_PROTOCOL) {
        stashTrailers(Utils.convertTrailers(headers));
      }
      endOfStream();
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
        deframer.deframe(new NettyBuffer(frame.retain()), false);
      } else {
        deframer2.deframe(new NettyBuffer(frame.retain()), false);
      }
    } else {
      // It's not a GRPC response, assume that the frame contains a text-based error message.

      // TODO(user): Should we send RST_STREAM as well?
      // TODO(user): is there a better way to handle large non-GRPC error messages?
      if (nonGrpcErrorMessage == null) {
        nonGrpcErrorMessage = new StringBuilder();
      }
      nonGrpcErrorMessage.append(frame.toString(UTF_8));
    }

    if (endOfStream) {
      endOfStream();
    }
  }

  private void endOfStream() {
    if (isGrpcResponse) {
      if (!GRPC_V2_PROTOCOL) {
        deframer.deframe(Buffers.empty(), true);
      } else {
        deframer2.deframe(Buffers.empty(), true);
      }
    } else {
      if (nonGrpcErrorMessage != null && nonGrpcErrorMessage.length() > 0) {
        responseStatus = responseStatus.withDescription(nonGrpcErrorMessage.toString());
      }
      setStatus(responseStatus, new Metadata.Trailers());
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
  private boolean isGrpcResponse(Http2Headers headers) {
    if (isGrpcResponse) {
      // Already verified that it's a gRPC response.
      return true;
    }

    if (headers == null) {
      // No headers, not a GRPC response.
      return false;
    }

    AsciiString contentType = headers.get(CONTENT_TYPE_HEADER);
    if (CONTENT_TYPE_PROTORPC.equalsIgnoreCase(contentType)) {
      return true;
    }

    // Since ESF returns the wrong content-type, assume that any 200 response is gRPC, until
    // b/16290036 is fixed.
    AsciiString statusLine = headers.status();
    if (statusLine != null) {
      HttpResponseStatus httpStatus = HttpResponseStatus.parseLine(statusLine);
      if (HttpResponseStatus.OK.equals(httpStatus)) {
        return true;
      }
    }

    return false;
  }

  /**
   * Parses the response status and converts it to a transport code.
   */
  private static Status responseStatus(Http2Headers headers, Status defaultValue) {
    // First, check to see if we found a v2 protocol grpc-status header.
    AsciiString grpcStatus = headers.get(GRPC_STATUS_HEADER);
    if (grpcStatus != null) {
      return Status.fromCodeValue(grpcStatus.parseInt());
    }

    // Next, check the HTTP/2 status.
    AsciiString statusLine = headers.status();
    if (statusLine != null) {
      HttpResponseStatus httpStatus = HttpResponseStatus.parseLine(statusLine);
      Status status = HttpUtil.httpStatusToGrpcStatus(httpStatus.code());
      // Only use OK when provided via the GRPC status header.
      if (!status.isOk()) {
        return status;
      }
    }

    return defaultValue;
  }
}
