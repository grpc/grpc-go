package com.google.net.stubby.http2.netty;

import com.google.net.stubby.NoOpRequest;
import com.google.net.stubby.Operation;
import com.google.net.stubby.Operation.Phase;
import com.google.net.stubby.Request;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Response;
import com.google.net.stubby.Session;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.MessageFramer;
import com.google.net.stubby.transport.Transport.Code;

import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandlerContext;
import io.netty.handler.codec.http2.AbstractHttp2ConnectionHandler;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Settings;

/**
 * Codec used by clients and servers to interpret HTTP2 frames in the context of an ongoing
 * request-response dialog
 */
public class Http2Codec extends AbstractHttp2ConnectionHandler {

  public static final int PADDING = 0;
  private final boolean client;
  private final RequestRegistry requestRegistry;
  private final Session session;
  private Http2Codec.Http2Writer http2Writer;

  /**
   * Constructor used by servers, takes a session which will receive operation events.
   */
  public Http2Codec(Session session, RequestRegistry requestRegistry) {
    super(true, true);
    // TODO(user): Use connection.isServer when not private in base class
    this.client = false;
    this.session = session;
    this.requestRegistry = requestRegistry;
  }

  /**
   * Constructor used by clients to send operations to a remote server
   */
  public Http2Codec(RequestRegistry requestRegistry) {
    super(false, true);
    this.client = true;
    this.session = null;
    this.requestRegistry = requestRegistry;
  }

  @Override
  public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
    http2Writer = new Http2Writer(ctx);
  }

  public Http2Writer getWriter() {
    return http2Writer;
  }

  @Override
  public void onDataRead(ChannelHandlerContext ctx, int streamId, ByteBuf data, int padding,
                  boolean endOfStream, boolean endOfSegment, boolean compressed)
      throws Http2Exception {
    Request request = requestRegistry.lookup(streamId);
    if (request == null) {
      // Stream may have been terminated already or this is just plain spurious
        throw Http2Exception.format(Http2Error.STREAM_CLOSED, "Stream does not exist");
    }
    Operation operation = client ? request.getResponse() : request;
    try {
      ByteBufDeframer deframer = getOrCreateDeframer(operation, ctx);
      deframer.deframe(data, operation);
      if (endOfStream) {
        finish(operation);
      }
    } catch (Throwable e) {
      // TODO(user): Need to disambiguate between stream corruption as well as client/server
      // generated errors. For stream corruption we always just send reset stream. For
      // clients we will also generally reset-stream on error, servers may send a more detailed
      // status.
      Status status = Status.fromThrowable(e);
      closeWithError(request, status);
    }
  }

  @Override
  public void onHeadersRead(ChannelHandlerContext ctx, int streamId, Http2Headers headers,
                     int streamDependency, short weight, boolean exclusive, int padding,
                     boolean endStream, boolean endSegment) throws Http2Exception {
    Request operation = requestRegistry.lookup(streamId);
    if (operation == null) {
      if (client) {
        // For clients an operation must already exist in the registry
        throw Http2Exception.format(Http2Error.REFUSED_STREAM, "Stream does not exist");
      } else {
        operation = serverStart(ctx, streamId, headers);
        if (operation == null) {
          closeWithError(new NoOpRequest(createResponse(new Http2Writer(ctx), streamId).build()),
              new Status(Code.NOT_FOUND));
        }
      }
    }
    if (endStream) {
      finish(client ? operation.getResponse() : operation);
    }
  }

  @Override
  public void onPriorityRead(ChannelHandlerContext ctx, int streamId, int streamDependency,
                      short weight, boolean exclusive) throws Http2Exception {
    // TODO
  }

  @Override
  public void onRstStreamRead(ChannelHandlerContext ctx, int streamId, long errorCode)
      throws Http2Exception {
    Request request = requestRegistry.lookup(streamId);
    if (request != null) {
      closeWithError(request, new Status(Code.CANCELLED, "Stream reset"));
      requestRegistry.remove(streamId);
    }
  }

  @Override
  public void onSettingsAckRead(ChannelHandlerContext ctx) throws Http2Exception {
    // TOOD
  }

  @Override
  public void onSettingsRead(ChannelHandlerContext ctx, Http2Settings settings)
      throws Http2Exception {
    // TOOD
  }

  @Override
  public void onPingRead(ChannelHandlerContext ctx, ByteBuf data) throws Http2Exception {
    // TODO
  }

  @Override
  public void onPingAckRead(ChannelHandlerContext ctx, ByteBuf data) throws Http2Exception {
    // TODO
  }

  @Override
  public void onPushPromiseRead(ChannelHandlerContext ctx, int streamId, int promisedStreamId,
                         Http2Headers headers, int padding) throws Http2Exception {
    // TODO
  }

  @Override
  public void onGoAwayRead(ChannelHandlerContext ctx, int lastStreamId, long errorCode,
                           ByteBuf debugData) throws Http2Exception {
    // TODO
  }

  @Override
  public void onWindowUpdateRead(ChannelHandlerContext ctx, int streamId, int windowSizeIncrement)
      throws Http2Exception {
    // TODO
  }

  @Override
  public void onAltSvcRead(ChannelHandlerContext ctx, int streamId, long maxAge, int port,
                    ByteBuf protocolId, String host, String origin) throws Http2Exception {
    // TODO
  }

  @Override
  public void onBlockedRead(ChannelHandlerContext ctx, int streamId) throws Http2Exception {
    // TODO
  }

  /**
   * Closes the request and its associated response with an internal error.
   */
  private void closeWithError(Request request, Status status) {
    try {
      request.close(status);
      request.getResponse().close(status);
    } finally {
      requestRegistry.remove(request.getId());
      disposeDeframer(request);
    }
  }

  /**
   * Create an HTTP2 response handler
   */
  private Response.ResponseBuilder createResponse(Http2Writer writer, int streamId) {
    return Http2Response.builder(streamId, writer, new MessageFramer(4096));
  }

  /**
   * Start the Request operation on the server
   */
  private Request serverStart(ChannelHandlerContext ctx, int streamId, Http2Headers headers) {
    if (!Http2Session.PROTORPC.equals(headers.get("content-type"))) {
      return null;
    }
    // Use Path to specify the operation
    String operationName =
        normalizeOperationName(headers.get(Http2Headers.HttpName.PATH.value()));
    if (operationName == null) {
      return null;
    }
    // Create the operation and bind a HTTP2 response operation
    Request op = session.startRequest(operationName, createResponse(new Http2Writer(ctx),
        streamId));
    if (op == null) {
      return null;
    }
    requestRegistry.register(op);
    return op;
  }

  // TODO(user): This needs proper namespacing support, this is currently just a hack
  private static String normalizeOperationName(String path) {
    return path.substring(1);
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

  public ByteBufDeframer getOrCreateDeframer(Operation operation, ChannelHandlerContext ctx) {
    ByteBufDeframer deframer = operation.get(ByteBufDeframer.class);
    if (deframer == null) {
      deframer = new ByteBufDeframer(ctx.alloc());
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

  public class Http2Writer {
    private final ChannelHandlerContext ctx;

    public Http2Writer(ChannelHandlerContext ctx) {
      this.ctx = ctx;
    }

    public ChannelFuture writeData(int streamId, ByteBuf data, boolean endStream,
                                   boolean endSegment, boolean compressed) {
      return Http2Codec.this.writeData(ctx, ctx.newPromise(),
          streamId, data, PADDING, endStream, endSegment, compressed);
    }

    public ChannelFuture writeHeaders(int streamId,
                                      Http2Headers headers,
                                      boolean endStream, boolean endSegment) {

      return Http2Codec.this.writeHeaders(ctx, ctx.newPromise(), streamId,
          headers, PADDING, endStream, endSegment);
    }

    public ChannelFuture writeHeaders(int streamId, Http2Headers headers, int streamDependency,
                                      short weight, boolean exclusive,
                                      boolean endStream, boolean endSegment) {
      return Http2Codec.this.writeHeaders(ctx, ctx.newPromise(), streamId,
          headers, streamDependency, weight, exclusive, PADDING, endStream, endSegment);
    }

    public ChannelFuture writeRstStream(int streamId, long errorCode) {
      return Http2Codec.this.writeRstStream(ctx, ctx.newPromise(),
          streamId,
          errorCode);
    }
  }
}
