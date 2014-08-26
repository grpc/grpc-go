package com.google.net.stubby.newtransport.okhttp;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableMap;
import com.google.common.io.ByteBuffers;
import com.google.common.io.ByteStreams;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.AbstractClientTransport;
import com.google.net.stubby.newtransport.AbstractStream;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.InputStreamDeframer;
import com.google.net.stubby.newtransport.StreamListener;
import com.google.net.stubby.newtransport.StreamState;
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
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.Iterator;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Executor;

import javax.annotation.concurrent.GuardedBy;

/**
 * A okhttp-based {@link ClientTransport} implementation.
 */
public class OkHttpClientTransport extends AbstractClientTransport {
  /** The default initial window size in HTTP/2 is 64 KiB for the stream and connection. */
  @VisibleForTesting
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
  private Object lock = new Object();
  @GuardedBy("lock")
  private int nextStreamId;
  private final Map<Integer, OkHttpClientStream> streams =
      Collections.synchronizedMap(new HashMap<Integer, OkHttpClientStream>());
  private final Executor executor;
  private int unacknowledgedBytesRead;
  private ClientFrameHandler clientFrameHandler;
  // The status used to finish all active streams when the transport is closed.
  @GuardedBy("lock")
  private boolean goAway;
  @GuardedBy("lock")
  private Status goAwayStatus;

  OkHttpClientTransport(String host, int port, Executor executor) {
    this.host = Preconditions.checkNotNull(host);
    this.port = port;
    this.executor = Preconditions.checkNotNull(executor);
    // Client initiated streams are odd, server initiated ones are even. Server should not need to
    // use it. We start clients at 3 to avoid conflicting with HTTP negotiation.
    nextStreamId = 3;
  }

  /**
   * Create a transport connected to a fake peer for test.
   */
  @VisibleForTesting
  OkHttpClientTransport(Executor executor, FrameReader frameReader, AsyncFrameWriter frameWriter,
      int nextStreamId) {
    host = null;
    port = -1;
    this.executor = Preconditions.checkNotNull(executor);
    this.frameReader = Preconditions.checkNotNull(frameReader);
    this.frameWriter = Preconditions.checkNotNull(frameWriter);
    this.nextStreamId = nextStreamId;
  }

  @Override
  protected ClientStream newStreamInternal(MethodDescriptor<?, ?> method, StreamListener listener) {
    return new OkHttpClientStream(method, listener);
  }

  @Override
  protected void doStart() {
    // We set host to null for test.
    if (host != null) {
      BufferedSource source;
      BufferedSink sink;
      try {
        Socket socket = new Socket(host, port);
        source = Okio.buffer(Okio.source(socket));
        sink = Okio.buffer(Okio.sink(socket));
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
      Variant variant = new Http20Draft12();
      frameReader = variant.newReader(source, true);
      frameWriter = new AsyncFrameWriter(variant.newWriter(sink, true), this, executor);
    }

    notifyStarted();
    clientFrameHandler = new ClientFrameHandler();
    executor.execute(clientFrameHandler);
  }

  @Override
  protected void doStop() {
    boolean normalClose;
    synchronized (lock) {
      normalClose = !goAway;
    }
    if (normalClose) {
      abort(new Status(Code.INTERNAL, "Transport stopped"));
      // Send GOAWAY with lastGoodStreamId of 0, since we don't expect any server-initiated streams.
      // The GOAWAY is part of graceful shutdown.
      frameWriter.goAway(0, ErrorCode.NO_ERROR, new byte[0]);
    }
    stopIfNecessary();
  }

  @VisibleForTesting
  ClientFrameHandler getHandler() {
    return clientFrameHandler;
  }

  @VisibleForTesting
  Map<Integer, OkHttpClientStream> getStreams() {
    return streams;
  }

  /**
   * Finish all active streams with given status, then close the transport.
   */
  void abort(Status status) {
    onGoAway(-1, status);
  }

  private void onGoAway(int lastKnownStreamId, Status status) {
    ArrayList<OkHttpClientStream> goAwayStreams = new ArrayList<OkHttpClientStream>();
    synchronized (lock) {
      goAway = true;
      goAwayStatus = status;
      Iterator<Map.Entry<Integer, OkHttpClientStream>> it = streams.entrySet().iterator();
      while (it.hasNext()) {
        Map.Entry<Integer, OkHttpClientStream> entry = it.next();
        if (entry.getKey() > lastKnownStreamId) {
          goAwayStreams.add(entry.getValue());
          it.remove();
        }
      }
    }

    // Starting stop, go into STOPPING state so that Channel know this Transport should not be used
    // further, will become STOPPED once all streams are complete.
    stopAsync();

    for (OkHttpClientStream stream : goAwayStreams) {
      stream.setStatus(status);
    }
  }

  /**
   * Called when a stream is closed.
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
   * When the transport is in goAway states, we should stop it once all active streams finish.
   */
  private void stopIfNecessary() {
    boolean shouldStop;
    synchronized (lock) {
      shouldStop = (goAway && streams.size() == 0);
    }
    if (shouldStop) {
      frameWriter.close();
      try {
        frameReader.close();
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
      notifyStopped();
    }
  }

  /**
   * Returns a Grpc status corresponding to the given ErrorCode.
   */
  @VisibleForTesting
  static Status toGrpcStatus(ErrorCode code) {
    return ERROR_CODE_TO_STATUS.get(code);
  }

  /**
   * Runnable which reads frames and dispatches them to in flight calls
   */
  @VisibleForTesting
  class ClientFrameHandler implements FrameReader.Handler, Runnable {
    ClientFrameHandler() {}

    @Override
    public void run() {
      String threadName = Thread.currentThread().getName();
      Thread.currentThread().setName("OkHttpClientTransport");
      try {
        // Read until the underlying socket closes.
        while (frameReader.nextFrame(this)) {
        }
      } catch (IOException ioe) {
        abort(Status.fromThrowable(ioe));
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
        if (finishStream(streamId, Status.OK)) {
          stopIfNecessary();
        }
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
      if (finishStream(streamId, toGrpcStatus(errorCode))) {
        stopIfNecessary();
      }
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
      onGoAway(lastGoodStreamId, new Status(Code.UNAVAILABLE, "Go away"));
    }

    @Override
    public void pushPromise(int streamId, int promisedStreamId, List<Header> requestHeaders)
        throws IOException {
      // We don't accept server initiated stream.
      frameWriter.rstStream(streamId, ErrorCode.PROTOCOL_ERROR);
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

  @GuardedBy("lock")
  private void assignStreamId(OkHttpClientStream stream) {
    Preconditions.checkState(stream.streamId == 0, "StreamId already assigned");
    stream.streamId = nextStreamId;
    streams.put(stream.streamId, stream);
    if (nextStreamId >= Integer.MAX_VALUE - 2) {
      onGoAway(Integer.MAX_VALUE, new Status(Code.INTERNAL, "Stream id exhaust"));
    } else {
      nextStreamId += 2;
    }
  }

  /**
   * Client stream for the okhttp transport.
   */
  @VisibleForTesting
  class OkHttpClientStream extends AbstractStream implements ClientStream {
    int streamId;
    final InputStreamDeframer deframer;
    int unacknowledgedBytesRead;

    OkHttpClientStream(MethodDescriptor<?, ?> method, StreamListener listener) {
      super(listener);
      deframer = new InputStreamDeframer(inboundMessageHandler());
      synchronized (lock) {
        if (goAway) {
          setStatus(goAwayStatus);
          return;
        }
        assignStreamId(this);
      }
      frameWriter.synStream(false, false, streamId, 0,
          Headers.createRequestHeaders(method.getName()));
    }

    InputStreamDeframer getDeframer() {
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
      if (streamId == 0) {
        // This should only happens when the stream was failed in constructor.
        Preconditions.checkState(state() == StreamState.CLOSED, "A unclosed stream has no id");
      }
      outboundPhase = Phase.STATUS;
      if (finishStream(streamId, toGrpcStatus(ErrorCode.CANCEL))) {
        frameWriter.rstStream(streamId, ErrorCode.CANCEL);
        stopIfNecessary();
      }
    }
  }
}
