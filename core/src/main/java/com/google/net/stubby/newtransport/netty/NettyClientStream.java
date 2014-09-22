package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.StreamState.CLOSED;
import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.AbstractClientStream;
import com.google.net.stubby.newtransport.GrpcDeframer;
import com.google.net.stubby.newtransport.HttpUtil;
import com.google.net.stubby.newtransport.MessageDeframer2;
import com.google.net.stubby.newtransport.StreamListener;
import com.google.net.stubby.transport.Transport;

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
  private Transport.Code responseCode = Transport.Code.UNKNOWN;
  private boolean isGrpcResponse;
  private StringBuilder nonGrpcErrorMessage = new StringBuilder();

  NettyClientStream(StreamListener listener, Channel channel,
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
    responseCode = responseCode(headers);
    isGrpcResponse = isGrpcResponse(headers, responseCode);
    if (endOfStream) {
      if (isGrpcResponse) {
        // TODO(user): call stashTrailers() as appropriate, then provide endOfStream to
        // deframer.
        setStatus(new Status(responseCode), new Metadata.Trailers());
      } else {
        setStatus(new Status(responseCode), new Metadata.Trailers());
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
        setStatus(new Status(responseCode, msg), new Metadata.Trailers());
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
  private static boolean isGrpcResponse(Http2Headers headers, Transport.Code code) {
    if (headers == null) {
      // No headers, not a GRPC response.
      return false;
    }

    // GRPC responses should always return OK. Updated this code once b/16290036 is fixed.
    if (code == Transport.Code.OK) {
      // ESF currently returns the wrong content-type for grpc.
      return true;
    }

    AsciiString contentType = headers.get(Utils.CONTENT_TYPE_HEADER);
    return Utils.CONTENT_TYPE_PROTORPC.equalsIgnoreCase(contentType);
  }

  /**
   * Parses the response status and converts it to a transport code.
   */
  private static Transport.Code responseCode(Http2Headers headers) {
    if (headers == null) {
      return Transport.Code.UNKNOWN;
    }

    AsciiString statusLine = headers.status();
    if (statusLine == null) {
      return Transport.Code.UNKNOWN;
    }

    HttpResponseStatus status = HttpResponseStatus.parseLine(statusLine);
    return HttpUtil.httpStatusToTransportCode(status.code());
  }
}
