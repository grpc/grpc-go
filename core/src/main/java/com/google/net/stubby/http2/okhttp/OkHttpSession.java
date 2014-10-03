package com.google.net.stubby.http2.okhttp;

import com.google.common.collect.ImmutableMap;
import com.google.common.io.ByteStreams;
import com.google.common.io.CountingInputStream;
import com.google.common.io.CountingOutputStream;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Operation;
import com.google.net.stubby.Operation.Phase;
import com.google.net.stubby.Request;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Response;
import com.google.net.stubby.Session;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.InputStreamDeframer;
import com.google.net.stubby.transport.MessageFramer;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.FrameReader;
import com.squareup.okhttp.internal.spdy.FrameWriter;
import com.squareup.okhttp.internal.spdy.Header;
import com.squareup.okhttp.internal.spdy.HeadersMode;
import com.squareup.okhttp.internal.spdy.Http20Draft14;
import com.squareup.okhttp.internal.spdy.Settings;
import com.squareup.okhttp.internal.spdy.Variant;

import okio.BufferedSink;
import okio.BufferedSource;
import okio.ByteString;
import okio.Okio;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.util.List;
import java.util.concurrent.Executor;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Basic implementation of {@link Session} using OkHttp
 */
public class OkHttpSession implements Session {

  private static final ImmutableMap<ErrorCode, Status> ERROR_CODE_TO_STATUS = ImmutableMap
      .<ErrorCode, Status>builder()
      .put(ErrorCode.NO_ERROR, Status.OK)
      .put(ErrorCode.PROTOCOL_ERROR, Status.INTERNAL.withDescription("Protocol error"))
      .put(ErrorCode.INVALID_STREAM, Status.INTERNAL.withDescription("Invalid stream"))
      .put(ErrorCode.UNSUPPORTED_VERSION,
          Status.INTERNAL.withDescription("Unsupported version"))
      .put(ErrorCode.STREAM_IN_USE, Status.INTERNAL.withDescription("Stream in use"))
      .put(ErrorCode.STREAM_ALREADY_CLOSED,
          Status.INTERNAL.withDescription("Stream already closed"))
      .put(ErrorCode.INTERNAL_ERROR, Status.INTERNAL.withDescription("Internal error"))
      .put(ErrorCode.FLOW_CONTROL_ERROR, Status.INTERNAL.withDescription("Flow control error"))
      .put(ErrorCode.STREAM_CLOSED, Status.INTERNAL.withDescription("Stream closed"))
      .put(ErrorCode.FRAME_TOO_LARGE, Status.INTERNAL.withDescription("Frame too large"))
      .put(ErrorCode.REFUSED_STREAM, Status.INTERNAL.withDescription("Refused stream"))
      .put(ErrorCode.CANCEL, Status.CANCELLED.withDescription("Cancelled"))
      .put(ErrorCode.COMPRESSION_ERROR, Status.INTERNAL.withDescription("Compression error"))
      .put(ErrorCode.INVALID_CREDENTIALS,
          Status.PERMISSION_DENIED.withDescription("Invalid credentials"))
      .build();

  public static Session startClient(Socket socket, RequestRegistry requestRegistry,
                                    Executor executor) {
    try {
      return new OkHttpSession(socket, requestRegistry, executor);
    } catch (IOException ioe) {
      throw new RuntimeException(ioe);
    }
  }

  public static Session startServer(Socket socket, Session server, RequestRegistry requestRegistry,
                                    Executor executor) {
    try {
      return new OkHttpSession(socket, server, requestRegistry, executor);
    } catch (IOException ioe) {
      throw new RuntimeException(ioe);
    }
  }

  private final String defaultAuthority;
  private final FrameReader frameReader;
  private final FrameWriter frameWriter;
  private final AtomicInteger sessionId;
  private final Session serverSession;
  private final RequestRegistry requestRegistry;
  private final CountingInputStream countingInputStream;
  private final CountingOutputStream countingOutputStream;

  /**
   * Construct a client-side session
   */
  private OkHttpSession(Socket socket, RequestRegistry requestRegistry,
      Executor executor) throws IOException {
    Variant variant = new Http20Draft14();
    // TODO(user): use Okio.buffer(Socket)
    countingInputStream = new CountingInputStream(socket.getInputStream());
    countingOutputStream = new CountingOutputStream(socket.getOutputStream());

    BufferedSource source = Okio.buffer(Okio.source(countingInputStream));
    BufferedSink sink = Okio.buffer(Okio.sink(countingOutputStream));
    frameReader = variant.newReader(source, true);
    frameWriter = variant.newWriter(sink, true);

    sessionId = new AtomicInteger(1);
    this.serverSession = null;
    this.requestRegistry = requestRegistry;
    executor.execute(new FrameHandler());

    // Determine the default :authority header to use.
    InetSocketAddress remoteAddress = (InetSocketAddress) socket.getRemoteSocketAddress();
    defaultAuthority = remoteAddress.getHostString() + ":" + remoteAddress.getPort();
  }

  /**
   * Construct a server-side session
   */
  private OkHttpSession(Socket socket, Session server,
      RequestRegistry requestRegistry, Executor executor) throws IOException {
    Variant variant = new Http20Draft14();
    // TODO(user): use Okio.buffer(Socket)
    countingInputStream = new CountingInputStream(socket.getInputStream());
    countingOutputStream = new CountingOutputStream(socket.getOutputStream());

    BufferedSource source = Okio.buffer(Okio.source(countingInputStream));
    BufferedSink sink = Okio.buffer(Okio.sink(countingOutputStream));
    frameReader = variant.newReader(source, true);
    frameWriter = variant.newWriter(sink, true);

    sessionId = new AtomicInteger(1);
    this.serverSession = server;
    this.requestRegistry = requestRegistry;
    executor.execute(new FrameHandler());

    // Authority is not used for server-side sessions.
    defaultAuthority = null;
  }

  @Override
  public String toString() {
    return "in=" + countingInputStream.getCount() + ";out=" + countingOutputStream.getCount();
  }

  private int getNextStreamId() {
    // Client initiated streams are odd, server initiated ones are even
    // We start clients at 3 to avoid conflicting with HTTP negotiation
    return (sessionId.getAndIncrement() * 2) + (isClient() ? 1 : 0);
  }

  private boolean isClient() {
    return serverSession == null;
  }

  @Override
  public Request startRequest(String operationName, Metadata.Headers headers,
      Response.ResponseBuilder responseBuilder) {
    int nextStreamId = getNextStreamId();
    Response response = responseBuilder.build(nextStreamId);
    String defaultPath = "/" + operationName;
    Http2Request request = new Http2Request(frameWriter,
        headers,
        defaultPath,
        defaultAuthority,
        response,
        requestRegistry,
        new MessageFramer(4096));
    return request;
  }

  /**
   * Close and remove any requests that still reside in the registry.
   */
  private void closeAllRequests(Status status) {
    for (Integer id : requestRegistry.getAllRequests()) {
      Request request = requestRegistry.remove(id);
      if (request != null && request.getPhase() != Phase.CLOSED) {
        request.close(status);
      }
    }
  }

  /**
   * Runnable which reads frames and dispatches them to in flight calls
   */
  private class FrameHandler implements FrameReader.Handler, Runnable {

    private FrameHandler() {}

    @Override
    public void run() {
      String threadName = Thread.currentThread().getName();
      Thread.currentThread().setName(isClient() ? "OkHttpClientSession" : "OkHttpServerSession");
      try {
        // Read until the underlying socket closes.
        while (frameReader.nextFrame(this)) {
        }
      } catch (Throwable ioe) {
        ioe.printStackTrace();
        closeAllRequests(Status.INTERNAL.withCause(ioe));
      } finally {
        // Restore the original thread name.
        Thread.currentThread().setName(threadName);
      }
    }

    /**
     * Lookup the operation bound to the specified stream id.
     */
    private Operation getOperation(int streamId) {
      Request request = requestRegistry.lookup(streamId);
      if (request == null) {
        return null;
      }
      if (isClient()) {
        return request.getResponse();
      }
      return request;
    }


    /**
     * Handle a HTTP2 DATA frame
     */
    @Override
    public void data(boolean inFinished, int streamId, BufferedSource in, int length)
        throws IOException {
      final Operation op = getOperation(streamId);
      if (op == null) {
        frameWriter.rstStream(streamId, ErrorCode.INVALID_STREAM);
        return;
      }
      InputStreamDeframer deframer = op.get(InputStreamDeframer.class);
      if (deframer == null) {
        deframer = new InputStreamDeframer();
        op.put(InputStreamDeframer.class, deframer);
      }

      // Wait until the frame is complete.
      in.require(length);

      // Protect against empty data frames used to just denote the end of stream.
      if (length > 0) {
        deframer.deframe(ByteStreams.limit(in.inputStream(), length), op);
      }

      if (inFinished) {
        finish(streamId);
        op.close(Status.OK);
      }
    }

    /**
     * Called when a HTTP2 stream is closed.
     */
    private void finish(int streamId) {
      Request request = requestRegistry.remove(streamId);
      if (request != null && request.getPhase() != Phase.CLOSED) {
        request.close(Status.OK);
      }
    }

    /**
     * Handle HTTP2 HEADER & CONTINUATION frames
     */
    @Override
    public void headers(boolean arg0,
        boolean inFinished,
        int streamId,
        int associatedStreamId,
        List<Header> headers,
        HeadersMode headersMode) {
      Operation op = getOperation(streamId);

      // Start an Operation for SYN_STREAM
      if (op == null && headersMode == HeadersMode.HTTP_20_HEADERS) {
        // TODO(user): Throwing inside this method seems to cause a request to
        // hang indefinitely ... possibly an OkHttp bug? We should investigate
        // this and come up with a solution that works for any handler method that encounters
        // an exception.
        String path = findReservedHeader(Header.TARGET_PATH.utf8(), headers);
        if (path == null) {
          try {
            // The :path MUST be provided. This is a protocol error.
            frameWriter.rstStream(streamId, ErrorCode.PROTOCOL_ERROR);
            frameWriter.flush();
            return;
          } catch (IOException e) {
            throw new RuntimeException(e);
          }
        }

        byte[][] binaryHeaders = new byte[headers.size() * 2][];
        for (int i = 0; i < headers.size(); i++) {
          Header header = headers.get(i);
          binaryHeaders[i * 2] = header.name.toByteArray();
          binaryHeaders[(i * 2) + 1] = header.value.toByteArray();
        }
        Metadata.Headers grpcHeaders = new Metadata.Headers(binaryHeaders);
        grpcHeaders.setPath(path);
        grpcHeaders.setAuthority(findReservedHeader(Header.TARGET_AUTHORITY.utf8(), headers));
        Request request = serverSession.startRequest(path, grpcHeaders,
            Http2Response.builder(streamId, frameWriter, new MessageFramer(4096)));
        requestRegistry.register(request);
        op = request;
      }
      if (op == null) {
        return;
      }
      // TODO(user): Do we do anything with non-reserved header here? We could just
      // pass them as context to the operation?
      if (inFinished) {
        finish(streamId);
      }
    }

    private String findReservedHeader(String name, List<Header> headers) {
      for (Header header : headers) {
        // Reserved headers must come before non-reserved headers, so we can exit the loop
        // early if we see a non-reserved header.
        String headerString = header.name.utf8();
        if (!headerString.startsWith(":")) {
          break;
        }
        if (headerString.equals(name)) {
          return header.value.utf8();
        }
      }
      return null;
    }

    @Override
    public void rstStream(int streamId, ErrorCode errorCode) {
      try {
        Operation op = getOperation(streamId);
        if (op == null) {
          return;
        }
        op.close(ERROR_CODE_TO_STATUS.get(errorCode));
      } finally {
        finish(streamId);
      }
    }

    @Override
    public void settings(boolean clearPrevious, Settings settings) {
      // not impl
    }

    @Override
    public void ping(boolean reply, int payload1, int payload2) {
      // noop
    }

    @Override
    public void ackSettings() {
      // fixme
    }

    @Override
    public void goAway(int arg0, ErrorCode arg1, ByteString arg2) {
      // fixme
    }

    @Override
    public void pushPromise(int arg0, int arg1, List<Header> arg2) throws IOException {
      // fixme
    }

    @Override
    public void windowUpdate(int arg0, long arg1) {
      // noop
    }

    @Override
    public void alternateService(int streamId,
        String origin,
        ByteString protocol,
        String host,
        int port,
        long maxAge) {
      // TODO(user): Is this required?

    }

    @Override
    public void priority(int streamId, int streamDependency, int weight, boolean exclusive) {
      // noop
    }
  }
}
