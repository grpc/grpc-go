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

package io.grpc.transport.okhttp;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.SettableFuture;

import com.squareup.okhttp.CipherSuite;
import com.squareup.okhttp.ConnectionSpec;
import com.squareup.okhttp.OkHttpTlsUpgrader;
import com.squareup.okhttp.TlsVersion;
import com.squareup.okhttp.internal.spdy.ErrorCode;
import com.squareup.okhttp.internal.spdy.FrameReader;
import com.squareup.okhttp.internal.spdy.Header;
import com.squareup.okhttp.internal.spdy.HeadersMode;
import com.squareup.okhttp.internal.spdy.Http20Draft16;
import com.squareup.okhttp.internal.spdy.OkHttpSettingsUtil;
import com.squareup.okhttp.internal.spdy.Settings;
import com.squareup.okhttp.internal.spdy.Variant;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodType;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.transport.ClientStreamListener;
import io.grpc.transport.ClientTransport;

import okio.Buffer;
import okio.BufferedSink;
import okio.BufferedSource;
import okio.ByteString;
import okio.Okio;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.Iterator;
import java.util.LinkedList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.net.ssl.SSLSocketFactory;

/**
 * A okhttp-based {@link ClientTransport} implementation.
 */
public class OkHttpClientTransport implements ClientTransport {
  public static final ConnectionSpec DEFAULT_CONNECTION_SPEC =
      new ConnectionSpec.Builder(ConnectionSpec.MODERN_TLS)
      .cipherSuites(
          // The following items should be sync with Netty's Http2SecurityUtil.CIPHERS.
          CipherSuite.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
          CipherSuite.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
          CipherSuite.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
          CipherSuite.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
          CipherSuite.TLS_DHE_RSA_WITH_AES_128_GCM_SHA256,
          CipherSuite.TLS_DHE_DSS_WITH_AES_128_GCM_SHA256,
          CipherSuite.TLS_DHE_RSA_WITH_AES_256_GCM_SHA384,
          CipherSuite.TLS_DHE_DSS_WITH_AES_256_GCM_SHA384)
      .tlsVersions(TlsVersion.TLS_1_2)
      .supportsTlsExtensions(true)
      .build();

  /** The default initial window size in HTTP/2 is 64 KiB for the stream and connection. */
  @VisibleForTesting
  static final int DEFAULT_INITIAL_WINDOW_SIZE = 64 * 1024;

  private static final Map<ErrorCode, Status> ERROR_CODE_TO_STATUS;
  private static final Logger log = Logger.getLogger(OkHttpClientTransport.class.getName());

  static {
    Map<ErrorCode, Status> errorToStatus = new HashMap<ErrorCode, Status>();
    errorToStatus.put(ErrorCode.NO_ERROR, Status.OK);
    errorToStatus.put(ErrorCode.PROTOCOL_ERROR,
        Status.INTERNAL.withDescription("Protocol error"));
    errorToStatus.put(ErrorCode.INVALID_STREAM,
        Status.INTERNAL.withDescription("Invalid stream"));
    errorToStatus.put(ErrorCode.UNSUPPORTED_VERSION,
        Status.INTERNAL.withDescription("Unsupported version"));
    errorToStatus.put(ErrorCode.STREAM_IN_USE,
        Status.INTERNAL.withDescription("Stream in use"));
    errorToStatus.put(ErrorCode.STREAM_ALREADY_CLOSED,
        Status.INTERNAL.withDescription("Stream already closed"));
    errorToStatus.put(ErrorCode.INTERNAL_ERROR,
        Status.INTERNAL.withDescription("Internal error"));
    errorToStatus.put(ErrorCode.FLOW_CONTROL_ERROR,
        Status.INTERNAL.withDescription("Flow control error"));
    errorToStatus.put(ErrorCode.STREAM_CLOSED,
        Status.INTERNAL.withDescription("Stream closed"));
    errorToStatus.put(ErrorCode.FRAME_TOO_LARGE,
        Status.INTERNAL.withDescription("Frame too large"));
    errorToStatus.put(ErrorCode.REFUSED_STREAM,
        Status.INTERNAL.withDescription("Refused stream"));
    errorToStatus.put(ErrorCode.CANCEL, Status.CANCELLED.withDescription("Cancelled"));
    errorToStatus.put(ErrorCode.COMPRESSION_ERROR,
        Status.INTERNAL.withDescription("Compression error"));
    errorToStatus.put(ErrorCode.INVALID_CREDENTIALS,
        Status.PERMISSION_DENIED.withDescription("Invalid credentials"));
    ERROR_CODE_TO_STATUS = Collections.unmodifiableMap(errorToStatus);
  }

  private final InetSocketAddress address;
  private final String authorityHost;
  private final String defaultAuthority;
  private Listener listener;
  private FrameReader frameReader;
  private AsyncFrameWriter frameWriter;
  private OutboundFlowController outboundFlow;
  private final Object lock = new Object();
  @GuardedBy("lock")
  private int nextStreamId;
  private final Map<Integer, OkHttpClientStream> streams =
      Collections.synchronizedMap(new HashMap<Integer, OkHttpClientStream>());
  private final Executor executor;
  private int connectionUnacknowledgedBytesRead;
  private ClientFrameHandler clientFrameHandler;
  // The status used to finish all active streams when the transport is closed.
  @GuardedBy("lock")
  private boolean goAway;
  @GuardedBy("lock")
  private Status goAwayStatus;
  @GuardedBy("lock")
  private boolean stopped;
  private SSLSocketFactory sslSocketFactory;
  private Socket socket;
  @GuardedBy("lock")
  private int maxConcurrentStreams = Integer.MAX_VALUE;
  @GuardedBy("lock")
  private LinkedList<PendingStream> pendingStreams = new LinkedList<PendingStream>();
  private ConnectionSpec connectionSpec = DEFAULT_CONNECTION_SPEC;

  OkHttpClientTransport(InetSocketAddress address, String authorityHost, Executor executor,
      @Nullable SSLSocketFactory sslSocketFactory, @Nullable ConnectionSpec connectionSpec) {
    this.address = Preconditions.checkNotNull(address);
    this.authorityHost = authorityHost;
    defaultAuthority = authorityHost + ":" + address.getPort();
    this.executor = Preconditions.checkNotNull(executor);
    // Client initiated streams are odd, server initiated ones are even. Server should not need to
    // use it. We start clients at 3 to avoid conflicting with HTTP negotiation.
    nextStreamId = 3;
    this.sslSocketFactory = sslSocketFactory;
    if (connectionSpec != null) {
      this.connectionSpec = connectionSpec;
    }
  }

  /**
   * Create a transport connected to a fake peer for test.
   */
  @VisibleForTesting
  OkHttpClientTransport(Executor executor, FrameReader frameReader, AsyncFrameWriter frameWriter,
      int nextStreamId, Socket socket) {
    address = null;
    authorityHost = null;
    defaultAuthority = "notarealauthority:80";
    this.executor = Preconditions.checkNotNull(executor);
    this.frameReader = Preconditions.checkNotNull(frameReader);
    this.frameWriter = Preconditions.checkNotNull(frameWriter);
    this.socket = Preconditions.checkNotNull(socket);
    this.outboundFlow = new OutboundFlowController(this, frameWriter);
    this.nextStreamId = nextStreamId;
  }

  @Override
  public OkHttpClientStream newStream(MethodDescriptor<?, ?> method,
                                      Metadata.Headers headers,
                                      ClientStreamListener listener) {
    Preconditions.checkNotNull(method, "method");
    Preconditions.checkNotNull(headers, "headers");
    Preconditions.checkNotNull(listener, "listener");

    OkHttpClientStream clientStream =
        OkHttpClientStream.newStream(listener, frameWriter, this, outboundFlow, method.getType());

    String defaultPath = "/" + method.getName();
    List<Header> requestHeaders =
        Headers.createRequestHeaders(headers, defaultPath, defaultAuthority);

    SettableFuture<Void> pendingFuture = null;
    synchronized (lock) {
      if (goAway) {
        clientStream.transportReportStatus(goAwayStatus, true, new Metadata.Trailers());
      } else if (streams.size() >= maxConcurrentStreams) {
        pendingFuture = SettableFuture.create();
        pendingStreams.add(new PendingStream(clientStream, pendingFuture, requestHeaders));
      } else {
        startStream(clientStream, requestHeaders);
      }
    }

    if (pendingFuture != null) {
      try {
        pendingFuture.get();
      } catch (InterruptedException e) {
        // Restore the interrupt.
        Thread.currentThread().interrupt();
        clientStream.cancel();
        throw new RuntimeException(e);
      } catch (ExecutionException e) {
        clientStream.cancel();
        throw new RuntimeException(e.getCause() != null ? e.getCause() : e);
      }
    }

    return clientStream;
  }

  @GuardedBy("lock")
  private void startStream(OkHttpClientStream stream, List<Header> requestHeaders) {
    Preconditions.checkState(stream.id() == null, "StreamId already assigned");
    stream.id(nextStreamId);
    streams.put(stream.id(), stream);
    frameWriter.synStream(false, false, stream.id(), 0, requestHeaders);
    // For unary and server streaming, there will be a data frame soon, no need to flush the header.
    if (stream.getType() != MethodType.UNARY
        && stream.getType() != MethodType.SERVER_STREAMING) {
      frameWriter.flush();
    }
    if (nextStreamId >= Integer.MAX_VALUE - 2) {
      onGoAway(Integer.MAX_VALUE, Status.INTERNAL.withDescription("Stream ids exhausted"));
    } else {
      nextStreamId += 2;
    }
  }

  private void startPendingStreams() {
    synchronized (lock) {
      while (!pendingStreams.isEmpty() && streams.size() < maxConcurrentStreams) {
        PendingStream pendingStream = pendingStreams.poll();
        startStream(pendingStream.clientStream, pendingStream.requestHeaders);
        pendingStream.createdFuture.set(null);
      }
    }
  }

  private void failPendingStreams(Status status) {
    LinkedList<PendingStream> streams;
    synchronized (lock) {
      streams = pendingStreams;
      pendingStreams = new LinkedList<PendingStream>();
    }
    for (PendingStream stream : streams) {
      stream.clientStream.transportReportStatus(
          status, true, new Metadata.Trailers());
      stream.createdFuture.set(null);
    }
  }

  @Override
  public void start(Listener listener) {
    this.listener = Preconditions.checkNotNull(listener, "listener");
    // We set host to null for test.
    if (address != null) {
      BufferedSource source;
      BufferedSink sink;
      try {
        socket = new Socket(address.getAddress(), address.getPort());
        if (sslSocketFactory != null) {
          socket = OkHttpTlsUpgrader.upgrade(
              sslSocketFactory, socket, authorityHost, address.getPort(), connectionSpec);
        }
        socket.setTcpNoDelay(true);
        source = Okio.buffer(Okio.source(socket));
        sink = Okio.buffer(Okio.sink(socket));
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
      Variant variant = new Http20Draft16();
      frameReader = variant.newReader(source, true);
      frameWriter = new AsyncFrameWriter(variant.newWriter(sink, true), this, executor);
      outboundFlow = new OutboundFlowController(this, frameWriter);
      frameWriter.connectionPreface();
      Settings settings = new Settings();
      frameWriter.settings(settings);
    }

    clientFrameHandler = new ClientFrameHandler();
    executor.execute(clientFrameHandler);
  }

  @Override
  public void shutdown() {
    boolean normalClose;
    synchronized (lock) {
      normalClose = !goAway;
    }
    if (normalClose) {
      // Send GOAWAY with lastGoodStreamId of 0, since we don't expect any server-initiated streams.
      // The GOAWAY is part of graceful shutdown.
      if (frameWriter != null) {
        frameWriter.goAway(0, ErrorCode.NO_ERROR, new byte[0]);
      }
      onGoAway(Integer.MAX_VALUE, Status.INTERNAL.withDescription("Transport stopped"));
    }
    stopIfNecessary();
  }

  @VisibleForTesting
  ClientFrameHandler getHandler() {
    return clientFrameHandler;
  }

  Map<Integer, OkHttpClientStream> getStreams() {
    return streams;
  }

  @VisibleForTesting
  int getPendingStreamSize() {
    synchronized (lock) {
      return pendingStreams.size();
    }
  }

  /**
   * Finish all active streams due to a failure, then close the transport.
   */
  void abort(Throwable failureCause) {
    log.log(Level.SEVERE, "Transport failed", failureCause);
    onGoAway(0, Status.INTERNAL.withCause(failureCause));
  }

  private void onGoAway(int lastKnownStreamId, Status status) {
    boolean notifyShutdown;
    ArrayList<OkHttpClientStream> goAwayStreams = new ArrayList<OkHttpClientStream>();
    synchronized (lock) {
      notifyShutdown = !goAway;
      goAway = true;
      goAwayStatus = status;
      synchronized (streams) {
        Iterator<Map.Entry<Integer, OkHttpClientStream>> it = streams.entrySet().iterator();
        while (it.hasNext()) {
          Map.Entry<Integer, OkHttpClientStream> entry = it.next();
          if (entry.getKey() > lastKnownStreamId) {
            goAwayStreams.add(entry.getValue());
            it.remove();
          }
        }
      }
    }

    if (notifyShutdown) {
      listener.transportShutdown();
    }
    for (OkHttpClientStream stream : goAwayStreams) {
      stream.transportReportStatus(status, false, new Metadata.Trailers());
    }
    failPendingStreams(status);
    stopIfNecessary();
  }

  /**
   * Called when a stream is closed.
   *
   * <p> Return false if the stream has already finished.
   */
  boolean finishStream(int streamId, @Nullable Status status) {
    OkHttpClientStream stream;
    stream = streams.remove(streamId);
    if (stream != null) {
      if (status != null) {
        boolean isCancelled = status.getCode() == Code.CANCELLED;
        stream.transportReportStatus(status, isCancelled, new Metadata.Trailers());
      }
      startPendingStreams();
      return true;
    }
    return false;
  }

  /**
   * When the transport is in goAway states, we should stop it once all active streams finish.
   */
  void stopIfNecessary() {
    boolean shouldStop;
    synchronized (lock) {
      shouldStop = (goAway && streams.size() == 0);
      if (shouldStop) {
        if (stopped) {
          // We've already stopped, don't stop again.
          shouldStop = false;
        }
        stopped = true;
      }
    }
    if (shouldStop) {
      // Wait for the frame writer to close.
      if (frameWriter != null) {
        frameWriter.close();
        // Close the socket to break out the reader thread, which will close the
        // frameReader and notify the listener.
        try {
          socket.close();
        } catch (IOException e) {
          log.log(Level.WARNING, "Failed closing socekt", e);
        }
      }
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
   * Runnable which reads frames and dispatches them to in flight calls.
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
        abort(ioe);
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
      final OkHttpClientStream stream;
      stream = streams.get(streamId);
      if (stream == null) {
        frameWriter.rstStream(streamId, ErrorCode.INVALID_STREAM);
        return;
      }

      // Wait until the frame is complete.
      in.require(length);

      Buffer buf = new Buffer();
      buf.write(in.buffer(), length);
      stream.transportDataReceived(buf, inFinished);

      // connection window update
      connectionUnacknowledgedBytesRead += length;
      if (connectionUnacknowledgedBytesRead >= DEFAULT_INITIAL_WINDOW_SIZE / 2) {
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
      OkHttpClientStream stream;
      stream = streams.get(streamId);
      if (stream == null) {
        frameWriter.rstStream(streamId, ErrorCode.INVALID_STREAM);
        return;
      }
      stream.transportHeadersReceived(headerBlock, inFinished);
    }

    @Override
    public void rstStream(int streamId, ErrorCode errorCode) {
      if (finishStream(streamId, toGrpcStatus(errorCode))) {
        stopIfNecessary();
      }
    }

    @Override
    public void settings(boolean clearPrevious, Settings settings) {
      if (OkHttpSettingsUtil.isSet(settings, OkHttpSettingsUtil.MAX_CONCURRENT_STREAMS)) {
        int receivedMaxConcurrentStreams = OkHttpSettingsUtil.get(
            settings, OkHttpSettingsUtil.MAX_CONCURRENT_STREAMS);
        synchronized (lock) {
          maxConcurrentStreams = receivedMaxConcurrentStreams;
        }
      }
      try {
        frameWriter.ackSettings(settings);
      } catch (IOException e) {
        abort(e);
      }
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
      onGoAway(lastGoodStreamId, Status.UNAVAILABLE.withDescription("Go away"));
    }

    @Override
    public void pushPromise(int streamId, int promisedStreamId, List<Header> requestHeaders)
        throws IOException {
      // We don't accept server initiated stream.
      frameWriter.rstStream(streamId, ErrorCode.PROTOCOL_ERROR);
    }

    @Override
    public void windowUpdate(int streamId, long delta) {
      outboundFlow.windowUpdate(streamId, (int) delta);
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

  private static class PendingStream {
    final OkHttpClientStream clientStream;
    final SettableFuture<Void> createdFuture;
    final List<Header> requestHeaders;

    PendingStream(OkHttpClientStream clientStream,
        SettableFuture<Void> createdFuture, List<Header> requestHeaders) {
      this.clientStream = clientStream;
      this.createdFuture = createdFuture;
      this.requestHeaders = requestHeaders;
    }
  }
}
