package com.google.net.stubby.newtransport.okhttp;

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableMap;
import com.google.common.io.ByteBuffers;
import com.google.common.io.ByteStreams;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.Status;
import com.google.net.stubby.http2.okhttp.Headers;
import com.google.net.stubby.newtransport.AbstractClientTransport;
import com.google.net.stubby.newtransport.AbstractStream;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.InputStreamDeframer;
import com.google.net.stubby.newtransport.StreamListener;
import com.google.net.stubby.transport.Transport;
import com.google.net.stubby.transport.Transport.Code;

import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.FrameReader;
import com.squareup.okhttp.internal.spdy.Header;
import com.squareup.okhttp.internal.spdy.HeadersMode;
import com.squareup.okhttp.internal.spdy.Http20Draft12;
import com.squareup.okhttp.internal.spdy.Settings;
import com.squareup.okhttp.internal.spdy.Variant;

import okio.ByteString;
import okio.BufferedSink;
import okio.BufferedSource;
import okio.Okio;
import okio.Buffer;

import java.io.IOException;
import java.net.Socket;
import java.nio.ByteBuffer;
import java.util.Collection;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import javax.annotation.concurrent.GuardedBy;

/**
 * A okhttp-based {@link ClientTransport} implementation.
 */
public class OkHttpClientTransport extends AbstractClientTransport {
  /** The default initial window size in HTTP/2 is 64 KiB for the stream and connection. */
  static final int DEFAULT_INITIAL_WINDOW_SIZE = 64 * 1024;

  private static final ImmutableMap<ErrorCode, Status> ERROR_CODE_TO_STATUS = ImmutableMap
      .<ErrorCode, Status>builder()
      .put(ErrorCode.NO_ERROR, Status.OK)
      .put(ErrorCode.PROTOCOL_ERROR, new Status(Transport.Code.INTERNAL, "Protocol error"))
      .put(ErrorCode.INVALID_STREAM, new Status(Transport.Code.INTERNAL, "Invalid stream"))
      .put(ErrorCode.UNSUPPORTED_VERSION,
          new Status(Transport.Code.INTERNAL, "Unsupported version"))
      .put(ErrorCode.STREAM_IN_USE, new Status(Transport.Code.INTERNAL, "Stream in use"))
      .put(ErrorCode.STREAM_ALREADY_CLOSED,
          new Status(Transport.Code.INTERNAL, "Stream already closed"))
      .put(ErrorCode.INTERNAL_ERROR, new Status(Transport.Code.INTERNAL, "Internal error"))
      .put(ErrorCode.FLOW_CONTROL_ERROR, new Status(Transport.Code.INTERNAL, "Flow control error"))
      .put(ErrorCode.STREAM_CLOSED, new Status(Transport.Code.INTERNAL, "Stream closed"))
      .put(ErrorCode.FRAME_TOO_LARGE, new Status(Transport.Code.INTERNAL, "Frame too large"))
      .put(ErrorCode.REFUSED_STREAM, new Status(Transport.Code.INTERNAL, "Refused stream"))
      .put(ErrorCode.CANCEL, new Status(Transport.Code.CANCELLED, "Cancelled"))
      .put(ErrorCode.COMPRESSION_ERROR, new Status(Transport.Code.INTERNAL, "Compression error"))
      .put(ErrorCode.INVALID_CREDENTIALS,
          new Status(Transport.Code.PERMISSION_DENIED, "Invalid credentials"))
      .build();

  private final String host;
  private final int port;
  private FrameReader frameReader;
  private AsyncFrameWriter frameWriter;
  @GuardedBy("this")
  private int nextStreamId;
  private final Map<Integer, OkHttpClientStream> streams =
      Collections.synchronizedMap(new HashMap<Integer, OkHttpClientStream>());
  private final ExecutorService executor = Executors.newCachedThreadPool();
  private int unacknowledgedBytesRead;

  public OkHttpClientTransport(String host, int port) {
    this.host = host;
    this.port = port;
    // Client initiated streams are odd, server initiated ones are even. Server should not need to
    // use it. We start clients at 3 to avoid conflicting with HTTP negotiation.
    nextStreamId = 3;
  }

  @Override
  protected ClientStream newStreamInternal(MethodDescriptor<?, ?> method, StreamListener listener) {
    return new OkHttpClientStream(method, listener);
  }

  @Override
  protected void doStart() {
    BufferedSource source;
    BufferedSink sink;
    try {
      Socket socket = new Socket(host, port);
      // TODO(user): use SpdyConnection.
      source = Okio.buffer(Okio.source(socket));
      sink = Okio.buffer(Okio.sink(socket));
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
    Variant variant = new Http20Draft12();
    frameReader = variant.newReader(source, true);
    frameWriter = new AsyncFrameWriter(variant.newWriter(sink, true), this, executor);

    executor.execute(new ClientFrameHandler());
    notifyStarted();
  }

  @Override
  protected void doStop() {
    closeAllStreams(new Status(Code.INTERNAL, "Transport stopped"));
    frameWriter.close();
    try {
      frameReader.close();
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
    executor.shutdown();
    notifyStopped();
  }

  /**
   * Close and remove all streams.
   */
  private void closeAllStreams(Status status) {
    Collection<OkHttpClientStream> streamsCopy;
    synchronized (streams) {
      streamsCopy = streams.values();
      streams.clear();
    }
    for (OkHttpClientStream stream : streamsCopy) {
      stream.setStatus(status);
    }
  }

  /**
   * Called when a HTTP2 stream is closed.
   *
   * <p> Return false if the stream has already finished.
   */
  private boolean finishStream(int streamId, Status status) {
    OkHttpClientStream stream;
    stream = streams.remove(streamId);
    if (stream != null) {
      // This is mainly for failed streams, for successfully finished streams, it's a no-op.
      stream.setStatus(status);
      return true;
    }
    return false;
  }

  /**
   * Runnable which reads frames and dispatches them to in flight calls
   */
  private class ClientFrameHandler implements FrameReader.Handler, Runnable {
    private ClientFrameHandler() {}

    @Override
    public void run() {
      String threadName = Thread.currentThread().getName();
      Thread.currentThread().setName("OkHttpClientTransport");
      try {
        // Read until the underlying socket closes.
        while (frameReader.nextFrame(this)) {
        }
      } catch (IOException ioe) {
        ioe.printStackTrace();
        closeAllStreams(new Status(Code.INTERNAL, ioe.getMessage()));
      } finally {
        // Restore the original thread name.
        Thread.currentThread().setName(threadName);
      }
    }

    /**
     * Handle a HTTP2 DATA frame
     */
    @Override
    public void data(boolean inFinished, int streamId, BufferedSource in, int length)
        throws IOException {
      final OkHttpClientStream stream;
      stream = streams.get(streamId);
      if (stream == null) {
        frameWriter.rstStream(streamId, ErrorCode.INVALID_STREAM);
        return;
      }
      InputStreamDeframer deframer = stream.getDeframer();

      // Wait until the frame is complete.
      in.require(length);

      deframer.deliverFrame(ByteStreams.limit(in.inputStream(), length), inFinished);
      unacknowledgedBytesRead += length;
      stream.unacknowledgedBytesRead += length;
      if (unacknowledgedBytesRead >= DEFAULT_INITIAL_WINDOW_SIZE / 2) {
        frameWriter.windowUpdate(0, unacknowledgedBytesRead);
        unacknowledgedBytesRead = 0;
      }
      if (stream.unacknowledgedBytesRead >= DEFAULT_INITIAL_WINDOW_SIZE / 2) {
        frameWriter.windowUpdate(streamId, stream.unacknowledgedBytesRead);
        stream.unacknowledgedBytesRead = 0;
      }
      if (inFinished) {
        finishStream(streamId, Status.OK);
      }
    }

    /**
     * Handle HTTP2 HEADER and CONTINUATION frames
     */
    @Override
    public void headers(boolean outFinished,
        boolean inFinished,
        int streamId,
        int associatedStreamId,
        List<Header> headerBlock,
        HeadersMode headersMode) {
      // TODO(user): handle received headers.
    }

    @Override
    public void rstStream(int streamId, ErrorCode errorCode) {
      finishStream(streamId, ERROR_CODE_TO_STATUS.get(errorCode));
    }

    @Override
    public void settings(boolean clearPrevious, Settings settings) {
      // not impl
      frameWriter.ackSettings();
    }

    @Override
    public void ping(boolean ack, int payload1, int payload2) {
      if (!ack) {
        frameWriter.ping(true, payload1, payload2);
      }
    }

    @Override
    public void ackSettings() {
      // Do nothing currently.
    }

    @Override
    public void goAway(int lastGoodStreamId, ErrorCode errorCode, ByteString debugData) {
      // TODO(user): Log here and implement the real Go away behavior: streams have
      // id <= lastGoodStreamId should not be closed.
      closeAllStreams(new Status(Code.UNAVAILABLE, "Go away"));
      stopAsync();
    }

    @Override
    public void pushPromise(int streamId, int promisedStreamId, List<Header> requestHeaders)
        throws IOException {
      // TODO(user): should send SETTINGS_ENABLE_PUSH=0, then here we should reset it with
      // PROTOCOL_ERROR.
      frameWriter.rstStream(streamId, ErrorCode.REFUSED_STREAM);
    }

    @Override
    public void windowUpdate(int arg0, long arg1) {
      // TODO(user): flow control.
    }

    @Override
    public void priority(int streamId, int streamDependency, int weight, boolean exclusive) {
      // Ignore priority change.
      // TODO(user): log
    }

    @Override
    public void alternateService(int streamId, String origin, ByteString protocol, String host,
        int port, long maxAge) {
      // TODO(user): Deal with alternateService propagation
    }
  }

  /**
   * Client stream for the okhttp transport.
   */
  private class OkHttpClientStream extends AbstractStream implements ClientStream {
    int streamId;
    final InputStreamDeframer deframer;
    int unacknowledgedBytesRead;

    public OkHttpClientStream(MethodDescriptor<?, ?> method, StreamListener listener) {
      super(listener);
      Preconditions.checkState(streamId == 0, "StreamId should be 0");
      synchronized (OkHttpClientTransport.this) {
        streamId = nextStreamId;
        nextStreamId += 2;
        streams.put(streamId, this);
        frameWriter.synStream(false, false, streamId, 0,
            Headers.createRequestHeaders(method.getName()));
      }
      deframer = new InputStreamDeframer(inboundMessageHandler());
    }

    public InputStreamDeframer getDeframer() {
      return deframer;
    }

    @Override
    protected void sendFrame(ByteBuffer frame, boolean endOfStream) {
      Preconditions.checkState(streamId != 0, "streamId should be set");
      Buffer buffer;
      try {
        // Read the data into a buffer.
        // TODO(user): swap to NIO buffers or zero-copy if/when okhttp/okio supports it
        buffer = new Buffer().readFrom(ByteBuffers.newConsumingInputStream(frame));
      } catch (IOException e) {
        throw new RuntimeException(e);
      }

      // Write the data to the remote endpoint.
      frameWriter.data(endOfStream, streamId, buffer);
      frameWriter.flush();
    }

    @Override
    public void cancel() {
      Preconditions.checkState(streamId != 0, "streamId should be set");
      outboundPhase = Phase.STATUS;
      if (finishStream(streamId, ERROR_CODE_TO_STATUS.get(ErrorCode.CANCEL))) {
        frameWriter.rstStream(streamId, ErrorCode.CANCEL);
      }
    }
  }
}
