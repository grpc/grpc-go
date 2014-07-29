package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.StreamState.CLOSED;
import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.base.Preconditions;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.AbstractStream;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.Deframer;
import com.google.net.stubby.newtransport.HttpUtil;
import com.google.net.stubby.newtransport.StreamListener;
import com.google.net.stubby.transport.Transport;

import io.netty.handler.codec.http.HttpResponseStatus;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelPromise;

import java.nio.ByteBuffer;

/**
 * Client stream for a Netty transport.
 */
class NettyClientStream extends AbstractStream implements ClientStream {
  public static final int PENDING_STREAM_ID = -1;

  private volatile int id = PENDING_STREAM_ID;
  private final Channel channel;
  private final Deframer<ByteBuf> deframer;
  private Transport.Code responseCode = Transport.Code.UNKNOWN;
  private boolean isGrpcResponse;
  private StringBuilder nonGrpcErrorMessage = new StringBuilder();

  NettyClientStream(StreamListener listener, Channel channel) {
    super(listener);
    this.channel = Preconditions.checkNotNull(channel, "channel");
    this.deframer = new ByteBufDeframer(channel.alloc(), inboundMessageHandler());
  }

  /**
   * Returns the HTTP/2 ID for this stream.
   */
  public int id() {
    return id;
  }

  void id(int id) {
    this.id = id;
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
  public void inboundHeadersRecieved(Http2Headers headers) {
    responseCode = responseCode(headers);
    isGrpcResponse = isGrpcResponse(headers, responseCode);
  }

  /**
   * Called in the channel thread to process the content of an inbound DATA frame.
   *
   * @param frame the inbound HTTP/2 DATA frame. If this buffer is not used immediately, it must be
   *        retained.
   * @param promise the promise to be set after the application has finished processing the frame.
   */
  public void inboundDataReceived(ByteBuf frame, boolean endOfStream, ChannelPromise promise) {
    Preconditions.checkNotNull(frame, "frame");
    Preconditions.checkNotNull(promise, "promise");
    if (state() == CLOSED) {
      promise.setSuccess();
      return;
    }

    if (isGrpcResponse) {
      // Retain the ByteBuf until it is released by the deframer.
      deframer.deliverFrame(frame.retain(), endOfStream);

      // TODO(user): add flow control.
      promise.setSuccess();
    } else {
      // It's not a GRPC response, assume that the frame contains a text-based error message.

      // TODO(user): Should we send RST_STREAM as well?
      // TODO(user): is there a better way to handle large non-GRPC error messages?
      nonGrpcErrorMessage.append(frame.toString(UTF_8));

      if (endOfStream) {
        String msg = nonGrpcErrorMessage.toString();
        setStatus(new Status(responseCode, msg));
      }
    }
  }

  @Override
  protected void sendFrame(ByteBuffer frame, boolean endOfStream) {
    SendGrpcFrameCommand cmd =
        new SendGrpcFrameCommand(this, toByteBuf(frame), endOfStream, endOfStream);
    channel.writeAndFlush(cmd);
  }

  /**
   * Copies the content of the given {@link ByteBuffer} to a new {@link ByteBuf} instance.
   */
  private ByteBuf toByteBuf(ByteBuffer source) {
    ByteBuf buf = channel.alloc().buffer(source.remaining());
    buf.writeBytes(source);
    return buf;
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

    String contentType = headers.get(HttpUtil.CONTENT_TYPE_HEADER);
    return HttpUtil.CONTENT_TYPE_PROTORPC.equalsIgnoreCase(contentType);
  }

  /**
   * Parses the response status and converts it to a transport code.
   */
  private static Transport.Code responseCode(Http2Headers headers) {
    if (headers == null) {
      return Transport.Code.UNKNOWN;
    }

    String statusLine = headers.status();
    if (statusLine == null) {
      return Transport.Code.UNKNOWN;
    }

    HttpResponseStatus status = HttpResponseStatus.parseLine(statusLine);
    return HttpUtil.httpStatusToTransportCode(status.code());
  }
}
