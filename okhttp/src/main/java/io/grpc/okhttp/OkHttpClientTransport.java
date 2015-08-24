/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.okhttp;

import static com.google.common.base.Preconditions.checkState;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Ticker;
import com.google.common.util.concurrent.SettableFuture;

import com.squareup.okhttp.ConnectionSpec;
import com.squareup.okhttp.OkHttpTlsUpgrader;
import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.FrameReader;
import com.squareup.okhttp.internal.spdy.FrameWriter;
import com.squareup.okhttp.internal.spdy.Header;
import com.squareup.okhttp.internal.spdy.HeadersMode;
import com.squareup.okhttp.internal.spdy.Http2;
import com.squareup.okhttp.internal.spdy.OkHttpSettingsUtil;
import com.squareup.okhttp.internal.spdy.Settings;
import com.squareup.okhttp.internal.spdy.Variant;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.Http2Ping;
import io.grpc.internal.SerializingExecutor;

import okio.Buffer;
import okio.BufferedSink;
import okio.BufferedSource;
import okio.ByteString;
import okio.Okio;

import java.io.IOException;
import java.net.Socket;
import java.util.Collections;
import java.util.HashMap;
import java.util.Iterator;
import java.util.LinkedList;
import java.util.List;
import java.util.Map;
import java.util.Random;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.net.ssl.SSLSocketFactory;

/**
 * A okhttp-based {@link ClientTransport} implementation.
 */
class OkHttpClientTransport implements ClientTransport {
  private static final Map<ErrorCode, Status> ERROR_CODE_TO_STATUS;
  private static final Logger log = Logger.getLogger(OkHttpClientTransport.class.getName());
  private static final OkHttpClientStream[] EMPTY_STREAM_ARRAY = new OkHttpClientStream[0];

  static {
    Map<ErrorCode, Status> errorToStatus = new HashMap<ErrorCode, Status>();
    errorToStatus.put(ErrorCode.NO_ERROR,
        Status.INTERNAL.withDescription("No error: A GRPC status of OK should have been sent"));
    errorToStatus.put(ErrorCode.PROTOCOL_ERROR,
        Status.INTERNAL.withDescription("Protocol error"));
    errorToStatus.put(ErrorCode.INTERNAL_ERROR,
        Status.INTERNAL.withDescription("Internal error"));
    errorToStatus.put(ErrorCode.FLOW_CONTROL_ERROR,
        Status.INTERNAL.withDescription("Flow control error"));
    errorToStatus.put(ErrorCode.STREAM_CLOSED,
        Status.INTERNAL.withDescription("Stream closed"));
    errorToStatus.put(ErrorCode.FRAME_TOO_LARGE,
        Status.INTERNAL.withDescription("Frame too large"));
    errorToStatus.put(ErrorCode.REFUSED_STREAM,
        Status.UNAVAILABLE.withDescription("Refused stream"));
    errorToStatus.put(ErrorCode.CANCEL,
        Status.CANCELLED.withDescription("Cancelled"));
    errorToStatus.put(ErrorCode.COMPRESSION_ERROR,
        Status.INTERNAL.withDescription("Compression error"));
    errorToStatus.put(ErrorCode.CONNECT_ERROR,
        Status.INTERNAL.withDescription("Connect error"));
    errorToStatus.put(ErrorCode.ENHANCE_YOUR_CALM,
        Status.RESOURCE_EXHAUSTED.withDescription("Enhance your calm"));
    errorToStatus.put(ErrorCode.INADEQUATE_SECURITY,
        Status.PERMISSION_DENIED.withDescription("Inadequate security"));
    ERROR_CODE_TO_STATUS = Collections.unmodifiableMap(errorToStatus);
  }

  private final String host;
  private final int port;
  private final String authorityHost;
  private final String defaultAuthority;
  private final Random random = new Random();
  private final Ticker ticker;
  private Listener listener;
  private FrameReader testFrameReader;
  private AsyncFrameWriter frameWriter;
  private OutboundFlowController outboundFlow;
  private final Object lock = new Object();
  @GuardedBy("lock")
  private int nextStreamId;
  @GuardedBy("lock")
  private final Map<Integer, OkHttpClientStream> streams =
      new HashMap<Integer, OkHttpClientStream>();
  private final Executor executor;
  // Wrap on executor, to guarantee some operations be executed serially.
  private final SerializingExecutor serializingExecutor;
  private int connectionUnacknowledgedBytesRead;
  private ClientFrameHandler clientFrameHandler;
  // The status used to finish all active streams when the transport is closed.
  @GuardedBy("lock")
  private boolean goAway;
  @GuardedBy("lock")
  private Status goAwayStatus;
  @GuardedBy("lock")
  private Http2Ping ping;
  @GuardedBy("lock")
  private boolean stopped;
  private SSLSocketFactory sslSocketFactory;
  private Socket socket;
  @GuardedBy("lock")
  private int maxConcurrentStreams = 0;
  @GuardedBy("lock")
  private LinkedList<OkHttpClientStream> pendingStreams = new LinkedList<OkHttpClientStream>();
  private final ConnectionSpec connectionSpec;
  private FrameWriter testFrameWriter;

  // The following fields should only be used for test.
  Runnable connectingCallback;
  SettableFuture<Void> connectedFuture;

  OkHttpClientTransport(String host, int port, String authorityHost, Executor executor,
      @Nullable SSLSocketFactory sslSocketFactory, ConnectionSpec connectionSpec) {
    this.host = Preconditions.checkNotNull(host);
    this.port = port;
    this.authorityHost = authorityHost;
    defaultAuthority = authorityHost + ":" + port;
    this.executor = Preconditions.checkNotNull(executor);
    serializingExecutor = new SerializingExecutor(executor);
    // Client initiated streams are odd, server initiated ones are even. Server should not need to
    // use it. We start clients at 3 to avoid conflicting with HTTP negotiation.
    nextStreamId = 3;
    this.sslSocketFactory = sslSocketFactory;
    this.connectionSpec = Preconditions.checkNotNull(connectionSpec);
    this.ticker = Ticker.systemTicker();
  }

  /**
   * Create a transport connected to a fake peer for test.
   */
  @VisibleForTesting
  OkHttpClientTransport(Executor executor, FrameReader frameReader, FrameWriter testFrameWriter,
      int nextStreamId, Socket socket, Ticker ticker,
      @Nullable Runnable connectingCallback, SettableFuture<Void> connectedFuture) {
    host = null;
    port = 0;
    authorityHost = null;
    defaultAuthority = "notarealauthority:80";
    this.executor = Preconditions.checkNotNull(executor);
    serializingExecutor = new SerializingExecutor(executor);
    this.testFrameReader = Preconditions.checkNotNull(frameReader);
    this.testFrameWriter = Preconditions.checkNotNull(testFrameWriter);
    this.socket = Preconditions.checkNotNull(socket);
    this.nextStreamId = nextStreamId;
    this.ticker = ticker;
    this.connectionSpec = null;
    this.connectingCallback = connectingCallback;
    this.connectedFuture = Preconditions.checkNotNull(connectedFuture);
  }

  private boolean isForTest() {
    return host == null;
  }

  @Override
  public void ping(final PingCallback callback, Executor executor) {
    checkState(frameWriter != null);
    long data = 0;
    Http2Ping p;
    boolean writePing;
    synchronized (lock) {
      if (stopped) {
        Http2Ping.notifyFailed(callback, executor, getPingFailure());
        return;
      }
      if (ping != null) {
        // we only allow one outstanding ping at a time, so just add the callback to
        // any outstanding operation
        p = ping;
        writePing = false;
      } else {
        // set outstanding operation and then write the ping after releasing lock
        data = random.nextLong();
        p = ping = new Http2Ping(data, Stopwatch.createStarted(ticker));
        writePing = true;
      }
    }
    if (writePing) {
      frameWriter.ping(false, (int) (data >>> 32), (int) data);
    }
    // If transport concurrently failed/stopped since we released the lock above, this could
    // immediately invoke callback (which we shouldn't do while holding a lock)
    p.addCallback(callback, executor);
  }

  @Override
  public OkHttpClientStream newStream(MethodDescriptor<?, ?> method,
                                      Metadata headers,
                                      ClientStreamListener listener) {
    Preconditions.checkNotNull(method, "method");
    Preconditions.checkNotNull(headers, "headers");
    Preconditions.checkNotNull(listener, "listener");

    String defaultPath = "/" + method.getFullMethodName();
    OkHttpClientStream clientStream = OkHttpClientStream.newStream(
        listener, frameWriter, this, outboundFlow, method.getType(), lock,
        Headers.createRequestHeaders(headers, defaultPath, defaultAuthority));

    synchronized (lock) {
      if (goAway) {
        clientStream.transportReportStatus(goAwayStatus, true, new Metadata());
      } else if (streams.size() >= maxConcurrentStreams) {
        pendingStreams.add(clientStream);
      } else {
        startStream(clientStream);
      }
    }

    return clientStream;
  }

  @GuardedBy("lock")
  private void startStream(OkHttpClientStream stream) {
    Preconditions.checkState(stream.id() == null, "StreamId already assigned");
    streams.put(nextStreamId, stream);
    stream.start(nextStreamId);
    stream.allocated();
    // For unary and server streaming, there will be a data frame soon, no need to flush the header.
    if (stream.getType() != MethodType.UNARY
        && stream.getType() != MethodType.SERVER_STREAMING) {
      frameWriter.flush();
    }
    if (nextStreamId >= Integer.MAX_VALUE - 2) {
      // Make sure nextStreamId greater than all used id, so that mayHaveCreatedStream() performs
      // correctly.
      nextStreamId = Integer.MAX_VALUE;
      onGoAway(Integer.MAX_VALUE, Status.INTERNAL.withDescription("Stream ids exhausted"));
    } else {
      nextStreamId += 2;
    }
  }

  /**
   * Starts pending streams, returns true if at least one pending stream is started.
   */
  private boolean startPendingStreams() {
    boolean hasStreamStarted = false;
    synchronized (lock) {
      // No need to check goAway since the pendingStreams will be cleared when goAway
      // becomes true.
      while (!pendingStreams.isEmpty() && streams.size() < maxConcurrentStreams) {
        OkHttpClientStream stream = pendingStreams.poll();
        startStream(stream);
        hasStreamStarted = true;
      }
    }
    return hasStreamStarted;
  }

  /**
   * Removes given pending stream, used when a pending stream is cancelled.
   */
  @GuardedBy("lock")
  void removePendingStream(OkHttpClientStream pendingStream) {
    pendingStreams.remove(pendingStream);
  }

  @Override
  public void start(Listener listener) {
    this.listener = Preconditions.checkNotNull(listener, "listener");

    frameWriter = new AsyncFrameWriter(this, serializingExecutor);
    outboundFlow = new OutboundFlowController(this, frameWriter);

    // Connecting in the serializingExecutor, so that some stream operations like synStream
    // will be executed after connected.
    serializingExecutor.execute(new Runnable() {
      @Override
      public void run() {
        if (isForTest()) {
          if (connectingCallback != null) {
            connectingCallback.run();
          }
          clientFrameHandler = new ClientFrameHandler(testFrameReader);
          executor.execute(clientFrameHandler);
          synchronized (lock) {
            maxConcurrentStreams = Integer.MAX_VALUE;
          }
          frameWriter.becomeConnected(testFrameWriter, socket);
          startPendingStreams();
          connectedFuture.set(null);
          return;
        }

        BufferedSource source;
        BufferedSink sink;
        Socket sock;
        try {
          sock = new Socket(host, port);
          if (sslSocketFactory != null) {
            sock = OkHttpTlsUpgrader.upgrade(
                sslSocketFactory, sock, authorityHost, port, connectionSpec);
          }
          sock.setTcpNoDelay(true);
          source = Okio.buffer(Okio.source(sock));
          sink = Okio.buffer(Okio.sink(sock));
        } catch (IOException e) {
          onIoException(e);
          // (and probably do all of this work asynchronously instead of in calling thread)
          throw new RuntimeException(e);
        }

        FrameWriter rawFrameWriter;
        synchronized (lock) {
          if (stopped) {
            // In case user called shutdown() during the connecting.
            try {
              sock.close();
            } catch (IOException e) {
              log.log(Level.WARNING, "Failed closing socket", e);
            }
            return;
          }
          socket = sock;
          maxConcurrentStreams = Integer.MAX_VALUE;
        }

        Variant variant = new Http2();
        rawFrameWriter = variant.newWriter(sink, true);
        frameWriter.becomeConnected(rawFrameWriter, socket);

        try {
          // Do these with the raw FrameWriter, so that they will be done in this thread,
          // and before any possible pending stream operations.
          rawFrameWriter.connectionPreface();
          Settings settings = new Settings();
          rawFrameWriter.settings(settings);
        } catch (IOException e) {
          onIoException(e);
          throw new RuntimeException(e);
        }

        clientFrameHandler = new ClientFrameHandler(variant.newReader(source, true));
        executor.execute(clientFrameHandler);
        startPendingStreams();
      }
    });
  }

  @Override
  public void shutdown() {
    synchronized (lock) {
      if (goAway) {
        return;
      }
    }

    // Send GOAWAY with lastGoodStreamId of 0, since we don't expect any server-initiated streams.
    // The GOAWAY is part of graceful shutdown.
    frameWriter.goAway(0, ErrorCode.NO_ERROR, new byte[0]);

    onGoAway(Integer.MAX_VALUE, Status.UNAVAILABLE.withDescription("Transport stopped"));
  }

  /**
   * Gets all active streams as an array.
   */
  OkHttpClientStream[] getActiveStreams() {
    synchronized (lock) {
      return streams.values().toArray(EMPTY_STREAM_ARRAY);
    }
  }

  @VisibleForTesting
  ClientFrameHandler getHandler() {
    return clientFrameHandler;
  }

  @VisibleForTesting
  int getPendingStreamSize() {
    synchronized (lock) {
      return pendingStreams.size();
    }
  }

  /**
   * Finish all active streams due to an IOException, then close the transport.
   */
  void onIoException(IOException failureCause) {
    log.log(Level.SEVERE, "Transport failed", failureCause);
    onGoAway(0, Status.UNAVAILABLE.withCause(failureCause));
  }

  /**
   * Send GOAWAY to the server, then finish all active streams and close the transport.
   */
  private void onError(ErrorCode errorCode, String moreDetail) {
    frameWriter.goAway(0, errorCode, new byte[0]);
    onGoAway(0, toGrpcStatus(errorCode).augmentDescription(moreDetail));
  }

  private void onGoAway(int lastKnownStreamId, Status status) {
    boolean notifyShutdown;
    synchronized (lock) {
      notifyShutdown = !goAway;
      goAway = true;
      goAwayStatus = status;
      Iterator<Map.Entry<Integer, OkHttpClientStream>> it = streams.entrySet().iterator();
      while (it.hasNext()) {
        Map.Entry<Integer, OkHttpClientStream> entry = it.next();
        if (entry.getKey() > lastKnownStreamId) {
          it.remove();
          entry.getValue().transportReportStatus(status, false, new Metadata());
        }
      }

      for (OkHttpClientStream stream : pendingStreams) {
        stream.transportReportStatus(status, true, new Metadata());
      }
      pendingStreams.clear();
    }

    if (notifyShutdown) {
      // TODO(madongfly): Another thread may called stopIfNecessary() and closed the socket, so that
      // the reading thread calls listener.transportTerminated() and race with this call.
      listener.transportShutdown(status);
    }

    stopIfNecessary();
  }

  /**
   * Called when a stream is closed, we do things like:
   * <ul>
   * <li>Removing the stream from the map.
   * <li>Optionally reporting the status.
   * <li>Starting pending streams if we can.
   * <li>Stopping the transport if this is the last live stream under a go-away status.
   * </ul>
   *
   * @param streamId the Id of the stream.
   * @param status the final status of this stream, null means no need to report.
   * @param errorCode reset the stream with this ErrorCode if not null.
   */
  void finishStream(int streamId, @Nullable Status status, @Nullable ErrorCode errorCode) {
    synchronized (lock) {
      OkHttpClientStream stream = streams.remove(streamId);
      if (stream != null) {
        if (errorCode != null) {
          frameWriter.rstStream(streamId, ErrorCode.CANCEL);
        }
        if (status != null) {
          boolean isCancelled = (status.getCode() == Code.CANCELLED
              || status.getCode() == Code.DEADLINE_EXCEEDED);
          stream.transportReportStatus(status, isCancelled, new Metadata());
        }
        if (!startPendingStreams()) {
          stopIfNecessary();
        }
      }
    }
  }

  /**
   * When the transport is in goAway states, we should stop it once all active streams finish.
   */
  void stopIfNecessary() {
    synchronized (lock) {
      if (goAway && streams.size() == 0) {
        if (!stopped) {
          stopped = true;
          // We will close the underlying socket in the writing thread to break out the reader
          // thread, which will close the frameReader and notify the listener.
          frameWriter.close();

          if (ping != null) {
            ping.failed(getPingFailure());
            ping = null;
          }
        }
      }
    }
  }

  private Throwable getPingFailure() {
    synchronized (lock) {
      if (goAwayStatus != null) {
        return goAwayStatus.asException();
      } else {
        return Status.UNAVAILABLE.withDescription("Connection closed").asException();
      }
    }
  }

  boolean mayHaveCreatedStream(int streamId) {
    synchronized (lock) {
      return streamId < nextStreamId && (streamId & 1) == 1;
    }
  }

  OkHttpClientStream getStream(int streamId) {
    synchronized (lock) {
      return streams.get(streamId);
    }
  }

  /**
   * Returns a Grpc status corresponding to the given ErrorCode.
   */
  @VisibleForTesting
  static Status toGrpcStatus(ErrorCode code) {
    Status status = ERROR_CODE_TO_STATUS.get(code);
    return status != null ? status : Status.UNKNOWN.withDescription(
        "Unknown http2 error code: " + code.httpCode);
  }

  /**
   * Runnable which reads frames and dispatches them to in flight calls.
   */
  @VisibleForTesting
  class ClientFrameHandler implements FrameReader.Handler, Runnable {
    FrameReader frameReader;
    boolean firstSettings = true;

    ClientFrameHandler(FrameReader frameReader) {
      this.frameReader = frameReader;
    }

    @Override
    public void run() {
      String threadName = Thread.currentThread().getName();
      Thread.currentThread().setName("OkHttpClientTransport");
      try {
        // Read until the underlying socket closes.
        while (frameReader.nextFrame(this)) {
        }
      } catch (IOException ioe) {
        // We send GoAway here because OkHttp wraps many protocol errors as IOException.
        // TODO(madongfly): Send the exception message to the server.
        frameWriter.goAway(0, ErrorCode.PROTOCOL_ERROR, new byte[0]);
        onIoException(ioe);
      } finally {
        try {
          frameReader.close();
        } catch (IOException ex) {
          log.log(Level.INFO, "Exception closing frame reader", ex);
        }
        listener.transportTerminated();
        // Restore the original thread name.
        Thread.currentThread().setName(threadName);
      }
    }

    /**
     * Handle a HTTP2 DATA frame.
     */
    @Override
    public void data(boolean inFinished, int streamId, BufferedSource in, int length)
        throws IOException {
      OkHttpClientStream stream = getStream(streamId);
      if (stream == null) {
        if (mayHaveCreatedStream(streamId)) {
          frameWriter.rstStream(streamId, ErrorCode.INVALID_STREAM);
        } else {
          onError(ErrorCode.PROTOCOL_ERROR, "Received data for unknown stream: " + streamId);
          return;
        }
      } else {
        // Wait until the frame is complete.
        in.require(length);

        Buffer buf = new Buffer();
        buf.write(in.buffer(), length);
        synchronized (lock) {
          stream.transportDataReceived(buf, inFinished);
        }
      }

      // connection window update
      connectionUnacknowledgedBytesRead += length;
      if (connectionUnacknowledgedBytesRead >= Utils.DEFAULT_WINDOW_SIZE / 2) {
        frameWriter.windowUpdate(0, connectionUnacknowledgedBytesRead);
        connectionUnacknowledgedBytesRead = 0;
      }
    }

    /**
     * Handle HTTP2 HEADER and CONTINUATION frames.
     */
    @Override
    public void headers(boolean outFinished,
        boolean inFinished,
        int streamId,
        int associatedStreamId,
        List<Header> headerBlock,
        HeadersMode headersMode) {
      boolean unknownStream = false;
      synchronized (lock) {
        OkHttpClientStream stream = streams.get(streamId);
        if (stream == null) {
          if (mayHaveCreatedStream(streamId)) {
            frameWriter.rstStream(streamId, ErrorCode.INVALID_STREAM);
          } else {
            unknownStream = true;
          }
        } else {
          stream.transportHeadersReceived(headerBlock, inFinished);
        }
      }
      if (unknownStream) {
        // We don't expect any server-initiated streams.
        onError(ErrorCode.PROTOCOL_ERROR, "Received header for unknown stream: " + streamId);
      }
    }

    @Override
    public void rstStream(int streamId, ErrorCode errorCode) {
      finishStream(streamId, toGrpcStatus(errorCode), null);
    }

    @Override
    public void settings(boolean clearPrevious, Settings settings) {
      synchronized (lock) {
        if (OkHttpSettingsUtil.isSet(settings, OkHttpSettingsUtil.MAX_CONCURRENT_STREAMS)) {
          int receivedMaxConcurrentStreams = OkHttpSettingsUtil.get(
              settings, OkHttpSettingsUtil.MAX_CONCURRENT_STREAMS);
          maxConcurrentStreams = receivedMaxConcurrentStreams;
        }

        if (OkHttpSettingsUtil.isSet(settings, OkHttpSettingsUtil.INITIAL_WINDOW_SIZE)) {
          int initialWindowSize = OkHttpSettingsUtil.get(
              settings, OkHttpSettingsUtil.INITIAL_WINDOW_SIZE);
          outboundFlow.initialOutboundWindowSize(initialWindowSize);
        }
        if (firstSettings) {
          listener.transportReady();
          firstSettings = false;
        }
      }

      frameWriter.ackSettings(settings);
    }

    @Override
    public void ping(boolean ack, int payload1, int payload2) {
      if (!ack) {
        frameWriter.ping(true, payload1, payload2);
      } else {
        Http2Ping p = null;
        long ackPayload = (((long) payload1) << 32) | (payload2 & 0xffffffffL);
        synchronized (lock) {
          if (ping != null) {
            if (ping.payload() == ackPayload) {
              p = ping;
              ping = null;
            } else {
              log.log(Level.WARNING, String.format("Received unexpected ping ack. "
                  + "Expecting %d, got %d", ping.payload(), ackPayload));
            }
          } else {
            log.warning("Received unexpected ping ack. No ping outstanding");
          }
        }
        // don't complete it while holding lock since callbacks could run immediately
        if (p != null) {
          p.complete();
        }
      }
    }

    @Override
    public void ackSettings() {
      // Do nothing currently.
    }

    @Override
    public void goAway(int lastGoodStreamId, ErrorCode errorCode, ByteString debugData) {
      Status status = GrpcUtil.Http2Error.statusForCode(errorCode.httpCode);
      if (debugData != null && debugData.size() > 0) {
        // If a debug message was provided, use it.
        status.augmentDescription(debugData.utf8());
      }
      onGoAway(lastGoodStreamId, status);
    }

    @Override
    public void pushPromise(int streamId, int promisedStreamId, List<Header> requestHeaders)
        throws IOException {
      // We don't accept server initiated stream.
      frameWriter.rstStream(streamId, ErrorCode.PROTOCOL_ERROR);
    }

    @Override
    public void windowUpdate(int streamId, long delta) {
      if (delta == 0) {
        String errorMsg = "Received 0 flow control window increment.";
        if (streamId == 0) {
          onError(ErrorCode.PROTOCOL_ERROR, errorMsg);
        } else {
          finishStream(streamId,
              Status.INTERNAL.withDescription(errorMsg), ErrorCode.PROTOCOL_ERROR);
        }
        return;
      }

      boolean unknownStream = false;
      synchronized (lock) {
        if (streamId == Utils.CONNECTION_STREAM_ID) {
          outboundFlow.windowUpdate(null, (int) delta);
          return;
        }

        OkHttpClientStream stream = streams.get(streamId);
        if (stream != null) {
          outboundFlow.windowUpdate(stream, (int) delta);
        } else if (!mayHaveCreatedStream(streamId)) {
          unknownStream = true;
        }
      }
      if (unknownStream) {
        onError(ErrorCode.PROTOCOL_ERROR,
            "Received window_update for unknown stream: " + streamId);
      }
    }

    @Override
    public void priority(int streamId, int streamDependency, int weight, boolean exclusive) {
      // Ignore priority change.
      // TODO(madongfly): log
    }

    @Override
    public void alternateService(int streamId, String origin, ByteString protocol, String host,
        int port, long maxAge) {
      // TODO(madongfly): Deal with alternateService propagation
    }
  }
}
