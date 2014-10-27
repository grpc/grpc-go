package com.google.net.stubby.transport.netty;

import static com.google.net.stubby.transport.StreamState.CLOSED;
import static com.google.net.stubby.transport.netty.Utils.CONTENT_TYPE_GRPC;
import static com.google.net.stubby.transport.netty.Utils.CONTENT_TYPE_HEADER;
import static com.google.net.stubby.transport.netty.Utils.GRPC_STATUS_HEADER;
import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.AbstractClientStream;
import com.google.net.stubby.transport.Buffers;
import com.google.net.stubby.transport.ClientStreamListener;
import com.google.net.stubby.transport.GrpcDeframer;
import com.google.net.stubby.transport.HttpUtil;
import com.google.net.stubby.transport.MessageDeframer2;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.BinaryHeaders;
import io.netty.handler.codec.http.HttpResponseStatus;
import io.netty.handler.codec.http2.DefaultHttp2InboundFlowController;
import io.netty.handler.codec.http2.Http2Headers;

import java.nio.ByteBuffer;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;

/**
 * Client stream for a Netty transport.
 */
class NettyClientStream extends AbstractClientStream implements NettyStream {

  private static final Logger log = Logger.getLogger(NettyClientStream.class.getName());

  public static final int PENDING_STREAM_ID = -1;

  private volatile int id = PENDING_STREAM_ID;
  private final Channel channel;
  private final GrpcDeframer deframer;
  private final MessageDeframer2 deframer2;
  private final WindowUpdateManager windowUpdateManager;
  private Status responseStatus = Status.UNKNOWN;
  private boolean isGrpcResponse;
  // Accumulate payload bytes that we can dump to log when the response is not a valid GRPC
  // response.
  private StringBuilder nonGrpcPayload;

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
  public void inboundHeadersReceived(Http2Headers headers, boolean endOfStream) {
    if (state() == CLOSED) {
      log.log(Level.INFO, "Received headers on closed stream {0}",
          BinaryHeaders.Utils.toStringUtf8(headers));
      return;
    }
    responseStatus = responseStatus(headers, responseStatus);
    if (inboundPhase == Phase.HEADERS) {
      inboundPhase(Phase.MESSAGE);
      isGrpcResponse = isGrpcResponse(headers);
      if (!isGrpcResponse) {
        responseStatus = Status.INTERNAL.withDescription(
            "Stream " + id() +
            "Invalid GRPC response headers "
            + BinaryHeaders.Utils.toStringUtf8(headers));
        // Don't send the RST_STREAM immediately, wait for some payload data to come in so we can
        // log it to aid debugging. Proxies will often describe why there is a failure in the body
        // so let's attempt to capture it for logs. We call endOfStream here to signal Status to
        // the application layer immediately.
        endOfStream();
        return;
      }
      // If endOfStream, we have trailers and no "headers" were sent.
      if (!endOfStream) {
        if (GRPC_V2_PROTOCOL) {
          deframer2.delayProcessing(receiveHeaders(Utils.convertHeaders(headers)));
        } else {
          // This is a little broken as it doesn't strictly wait for the last payload handled
          // by the deframer to be processed by the application layer. Not worth fixing as will
          // be removed when the old deframer is removed.
          receiveHeaders(Utils.convertHeaders(headers));
        }
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
    if (!isGrpcResponse) {
      if (responseStatus == Status.UNKNOWN) {
        responseStatus = Status.INTERNAL.withDescription("Received paylaod with no headers");
      }
      nonGrpcPayload = nonGrpcPayload == null ?
          new StringBuilder(frame.toString(UTF_8)) :
          nonGrpcPayload.append(frame.toString(UTF_8));
      if (nonGrpcPayload.length() > 1000 || endOfStream) {
        log.log(Level.INFO, "Stream {0} - invalid GRPC response payload {1}",
            new Object[]{id(), nonGrpcPayload});
        if (!endOfStream) {
          // Read enough data to provide reasonable context in the logs. Now cancel the stream
          // so servers that are streaming can stop.
          cancel();
        }
      }
    }
    if (state() == CLOSED) {
      return;
    }
    inboundPhase(Phase.MESSAGE);
    // Retain the ByteBuf until it is released by the deframer.
    if (!GRPC_V2_PROTOCOL) {
      deframer.deframe(new NettyBuffer(frame.retain()), false);
    } else {
      deframer2.deframe(new NettyBuffer(frame.retain()), false);
    }
    if (endOfStream) {
      endOfStream();
    }
  }

  private void endOfStream() {
    inboundPhase(Phase.STATUS);
    if (isGrpcResponse) {
      if (!GRPC_V2_PROTOCOL) {
        deframer.deframe(Buffers.empty(), true);
      } else {
        deframer2.deframe(Buffers.empty(), true);
      }
    } else {
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
    if (CONTENT_TYPE_GRPC.equalsIgnoreCase(contentType)) {
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
