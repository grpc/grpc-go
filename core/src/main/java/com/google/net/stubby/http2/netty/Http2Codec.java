package com.google.net.stubby.http2.netty;

import com.google.net.stubby.Operation;
import com.google.net.stubby.Operation.Phase;
import com.google.net.stubby.Request;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Session;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.MessageFramer;
import com.google.net.stubby.transport.Transport;
import com.google.net.stubby.transport.Transport.Code;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.channel.ChannelHandlerAdapter;
import io.netty.channel.ChannelHandlerContext;
import io.netty.handler.codec.http2.draft10.Http2Error;
import io.netty.handler.codec.http2.draft10.Http2Headers;
import io.netty.handler.codec.http2.draft10.frame.DefaultHttp2RstStreamFrame;
import io.netty.handler.codec.http2.draft10.frame.Http2DataFrame;
import io.netty.handler.codec.http2.draft10.frame.Http2HeadersFrame;
import io.netty.handler.codec.http2.draft10.frame.Http2RstStreamFrame;
import io.netty.handler.codec.http2.draft10.frame.Http2StreamFrame;

/**
 * Codec used by clients and servers to interpret HTTP2 frames in the context of an ongoing
 * request-response dialog
 */
public class Http2Codec extends ChannelHandlerAdapter {

  private final boolean client;
  private final RequestRegistry requestRegistry;
  private final Session session;
  private ByteBufAllocator alloc;

  /**
   * Constructor used by servers, takes a session which will receive operation events.
   */
  public Http2Codec(Session session, RequestRegistry requestRegistry) {
    this.client = false;
    this.session = session;
    this.requestRegistry = requestRegistry;
  }

  /**
   * Constructor used by clients to send operations to a remote server
   */
  public Http2Codec(RequestRegistry requestRegistry) {
    this.client = true;
    this.session = null;
    this.requestRegistry = requestRegistry;
  }

  @Override
  public void channelInactive(ChannelHandlerContext ctx) throws Exception {
    // Abort any active requests.
    requestRegistry.drainAllRequests(new Status(Transport.Code.ABORTED));

    super.channelInactive(ctx);
  }

  @Override
  public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
    if (!(msg instanceof Http2StreamFrame)) {
      return;
    }
    this.alloc = ctx.alloc();
    Http2StreamFrame frame = (Http2StreamFrame) msg;
    Request operation = requestRegistry.lookup(frame.getStreamId());
    try {
      if (operation == null) {
        if (client) {
          // For clients an operation must already exist in the registry
          throw new IllegalStateException("Response operation must already be bound");
        } else {
          operation = serverStart(ctx, frame);
          if (operation == null) {
            // Unknown operation, refuse the stream
            sendRstStream(ctx, frame.getStreamId(), Http2Error.REFUSED_STREAM);
          }
        }
      } else {
        // Consume the frame
        progress(client ? operation.getResponse() : operation, frame);
      }
    } catch (Throwable e) {
      closeWithInternalError(operation, e);
      sendRstStream(ctx, frame.getStreamId(), Http2Error.INTERNAL_ERROR);
      throw e;
    }
  }

  /**
   * Closes the request and its associate response with an internal error.
   */
  private void closeWithInternalError(Request request, Throwable e) {
    if (request != null) {
      Status status = new Status(Code.INTERNAL, e);
      request.close(status);
      request.getResponse().close(status);
      requestRegistry.remove(request.getId());
    }
  }

  /**
   * Writes the HTTP/2 RST Stream frame to the remote endpoint, indicating a stream failure.
   */
  private void sendRstStream(ChannelHandlerContext ctx, int streamId, Http2Error error) {
    DefaultHttp2RstStreamFrame frame = new DefaultHttp2RstStreamFrame.Builder()
        .setStreamId(streamId).setErrorCode(error.getCode()).build();
    ctx.writeAndFlush(frame);
  }

  /**
   * Start the Request operation on the server
   */
  private Request serverStart(ChannelHandlerContext ctx, Http2StreamFrame frame) {
    if (!(frame instanceof Http2HeadersFrame)) {
      // TODO(user): Better error detail to client here
      return null;
    }
    Http2HeadersFrame headers = (Http2HeadersFrame) frame;
    if (!Http2Session.PROTORPC.equals(headers.getHeaders().get("content-type"))) {
      return null;
    }
    // Use Path to specify the operation
    String operationName =
        normalizeOperationName(headers.getHeaders().get(Http2Headers.HttpName.PATH.value()));
    if (operationName == null) {
      return null;
    }
    // Create the operation and bind a HTTP2 response operation
    Request op = session.startRequest(operationName,
        Http2Response.builder(frame.getStreamId(), ctx.channel(), new MessageFramer(4096)));
    if (op == null) {
      return null;
    }
    requestRegistry.register(op);
    // Immediately deframe the remaining headers in the frame
    progressHeaders(op, (Http2HeadersFrame) frame);
    return op;
  }

  // TODO(user): This needs proper namespacing support, this is currently just a hack
  private static String normalizeOperationName(String path) {
    return path.substring(1);
  }


  /**
   * Consume a received frame
   */
  private void progress(Operation operation, Http2StreamFrame frame) {
    if (frame instanceof Http2HeadersFrame) {
      progressHeaders(operation, (Http2HeadersFrame) frame);
    } else if (frame instanceof Http2DataFrame) {
      progressPayload(operation, (Http2DataFrame) frame);
    } else if (frame instanceof Http2RstStreamFrame) {
      // Cancel
      operation.close(null);
      finish(operation);
    } else {
      // TODO(user): More refined handling for PING, GO_AWAY, SYN_STREAM, WINDOW_UPDATE, SETTINGS
      operation.close(null);
      finish(operation);
    }
  }

  /**
   * Consume headers in the frame. Any header starting with ':' is considered reserved
   */
  private void progressHeaders(Operation operation, Http2HeadersFrame frame) {
    // TODO(user): Currently we do not do anything with HTTP2 headers
    if (frame.isEndOfStream()) {
      finish(operation);
    }
  }

  private void progressPayload(Operation operation, Http2DataFrame frame) {
    try {

      // Copy the data buffer.
      // TODO(user): Need to decide whether to use pooling or not.
      ByteBuf dataCopy = frame.content().copy();

      if (operation == null) {
        return;
      }
      ByteBufDeframer deframer = getOrCreateDeframer(operation);
      deframer.deframe(dataCopy, operation);
      if (frame.isEndOfStream()) {
        finish(operation);
      }

    } finally {
      frame.release();
    }
  }

  /**
   * Called when a HTTP2 stream is closed.
   */
  private void finish(Operation operation) {
    disposeDeframer(operation);
    requestRegistry.remove(operation.getId());
    if (operation.getPhase() != Phase.CLOSED) {
      operation.close(Status.OK);
    }
  }

  public ByteBufDeframer getOrCreateDeframer(Operation operation) {
    ByteBufDeframer deframer = operation.get(ByteBufDeframer.class);
    if (deframer == null) {
      deframer = new ByteBufDeframer(alloc);
      operation.put(ByteBufDeframer.class, deframer);
    }
    return deframer;
  }

  public void disposeDeframer(Operation operation) {
    ByteBufDeframer deframer = operation.remove(ByteBufDeframer.class);
    if (deframer != null) {
      deframer.dispose();
    }
  }
}
