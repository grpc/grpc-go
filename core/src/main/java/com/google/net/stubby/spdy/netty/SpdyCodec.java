package com.google.net.stubby.spdy.netty;

import com.google.net.stubby.Operation;
import com.google.net.stubby.Operation.Phase;
import com.google.net.stubby.Request;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Session;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.MessageFramer;
import com.google.net.stubby.transport.Transport;
import com.google.net.stubby.transport.Transport.Code;

import io.netty.channel.ChannelHandlerAdapter;
import io.netty.channel.ChannelHandlerContext;
import io.netty.handler.codec.spdy.DefaultSpdyRstStreamFrame;
import io.netty.handler.codec.spdy.SpdyDataFrame;
import io.netty.handler.codec.spdy.SpdyHeaders;
import io.netty.handler.codec.spdy.SpdyHeadersFrame;
import io.netty.handler.codec.spdy.SpdyRstStreamFrame;
import io.netty.handler.codec.spdy.SpdyStreamFrame;
import io.netty.handler.codec.spdy.SpdyStreamStatus;
import io.netty.handler.codec.spdy.SpdySynStreamFrame;

/**
 * Codec used by clients and servers to interpret SPDY frames in the context of an ongoing
 * request-response dialog
 */
public class SpdyCodec extends ChannelHandlerAdapter {

  private final boolean client;
  private final RequestRegistry requestRegistry;
  private final Session session;

  /**
   * Constructor used by servers, takes a session which will receive operation events.
   */
  public SpdyCodec(Session session, RequestRegistry requestRegistry) {
    this.client = false;
    this.session = session;
    this.requestRegistry = requestRegistry;
  }

  /**
   * Constructor used by clients to send operations to a remote server
   */
  public SpdyCodec(RequestRegistry requestRegistry) {
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
    if (!(msg instanceof SpdyStreamFrame)) {
      return;
    }
    SpdyStreamFrame frame = (SpdyStreamFrame) msg;
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
            sendRstStream(ctx, frame.getStreamId(), SpdyStreamStatus.REFUSED_STREAM);
          }
        }
      } else {
        // Consume the frame
        progress(client ? operation.getResponse() : operation, frame);
      }
    } catch (Throwable e) {
      closeWithInternalError(operation, e);
      sendRstStream(ctx, frame.getStreamId(), SpdyStreamStatus.INTERNAL_ERROR);
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
   * Writes the Spdy RST Stream frame to the remote endpoint, indicating a stream failure.
   */
  private void sendRstStream(ChannelHandlerContext ctx, int streamId, SpdyStreamStatus status) {
    DefaultSpdyRstStreamFrame frame = new DefaultSpdyRstStreamFrame(streamId, status.getCode());
    ctx.writeAndFlush(frame);
  }

  /**
   * Start the Request operation on the server
   */
  private Request serverStart(ChannelHandlerContext ctx, SpdyStreamFrame frame) {
    if (!(frame instanceof SpdySynStreamFrame)) {
      // TODO(user): Better error detail to client here
      return null;
    }
    SpdySynStreamFrame headers = (SpdySynStreamFrame) frame;
    if (!SpdySession.PROTORPC.equals(headers.headers().get("content-type"))) {
      return null;
    }
    // Use Path to specify the operation
    String operationName =
        normalizeOperationName(headers.headers().get(SpdyHeaders.HttpNames.PATH));
    if (operationName == null) {
      return null;
    }
    // Create the operation and bind a SPDY response operation
    Request op = session.startRequest(operationName,
        SpdyResponse.builder(frame.getStreamId(), ctx.channel(), new MessageFramer(4096)));
    if (op == null) {
      return null;
    }
    requestRegistry.register(op);
    // Immediately deframe the remaining headers in the frame
    progressHeaders(op, (SpdyHeadersFrame) frame);
    return op;
  }

  // TODO(user): This needs proper namespacing support, this is currently just a hack
  private static String normalizeOperationName(String path) {
    return path.substring(1);
  }


  /**
   * Consume a received frame
   */
  private void progress(Operation operation, SpdyStreamFrame frame) {
    if (frame instanceof SpdyHeadersFrame) {
      progressHeaders(operation, (SpdyHeadersFrame) frame);
    } else if (frame instanceof SpdyDataFrame) {
      progressPayload(operation, (SpdyDataFrame) frame);
    } else if (frame instanceof SpdyRstStreamFrame) {
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
   * Consume headers in the frame. Any header starting with ';' is considered reserved
   */
  private void progressHeaders(Operation operation, SpdyHeadersFrame frame) {
    // TODO(user): Currently we do not do anything with SPDY headers
    if (frame.isLast()) {
      finish(operation);
    }
  }

  private void progressPayload(Operation operation, SpdyDataFrame frame) {
    if (operation == null) {
      return;
    }
    ByteBufDeframer deframer = operation.get(ByteBufDeframer.class);
    if (deframer == null) {
      deframer = new ByteBufDeframer();
      operation.put(ByteBufDeframer.class, deframer);
    }
    deframer.deframe(frame.content(), operation);
    if (frame.isLast()) {
      finish(operation);
    }
  }

  /**
   * Called when a SPDY stream is closed.
   */
  private void finish(Operation operation) {
    requestRegistry.remove(operation.getId());
    if (operation.getPhase() != Phase.CLOSED) {
      operation.close(Status.OK);
    }
  }
}
