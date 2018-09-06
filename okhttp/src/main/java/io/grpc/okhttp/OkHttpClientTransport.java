/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.okhttp;

import static com.google.common.base.Preconditions.checkState;
import static io.grpc.internal.GrpcUtil.TIMER_SERVICE;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.MoreObjects;
import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import com.squareup.okhttp.Credentials;
import com.squareup.okhttp.HttpUrl;
import com.squareup.okhttp.Request;
import com.squareup.okhttp.internal.http.StatusLine;
import io.grpc.Attributes;
import io.grpc.CallCredentials;
import io.grpc.CallOptions;
import io.grpc.Grpc;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalLogId;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.SecurityLevel;
import io.grpc.Status;
import io.grpc.Status.Code;
import io.grpc.StatusException;
import io.grpc.internal.ClientStreamListener.RpcProgress;
import io.grpc.internal.ConnectionClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.Http2Ping;
import io.grpc.internal.KeepAliveManager;
import io.grpc.internal.KeepAliveManager.ClientKeepAlivePinger;
import io.grpc.internal.ProxyParameters;
import io.grpc.internal.SerializingExecutor;
import io.grpc.internal.SharedResourceHolder;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.TransportTracer;
import io.grpc.okhttp.AsyncFrameWriter.TransportExceptionHandler;
import io.grpc.okhttp.internal.ConnectionSpec;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.FrameReader;
import io.grpc.okhttp.internal.framed.FrameWriter;
import io.grpc.okhttp.internal.framed.Header;
import io.grpc.okhttp.internal.framed.HeadersMode;
import io.grpc.okhttp.internal.framed.Http2;
import io.grpc.okhttp.internal.framed.Settings;
import io.grpc.okhttp.internal.framed.Variant;
import java.io.EOFException;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.net.URI;
import java.util.Collections;
import java.util.EnumMap;
import java.util.HashMap;
import java.util.Iterator;
import java.util.LinkedList;
import java.util.List;
import java.util.Map;
import java.util.Random;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.net.ssl.HostnameVerifier;
import javax.net.ssl.SSLSession;
import javax.net.ssl.SSLSocket;
import javax.net.ssl.SSLSocketFactory;
import okio.Buffer;
import okio.BufferedSink;
import okio.BufferedSource;
import okio.ByteString;
import okio.Okio;
import okio.Source;
import okio.Timeout;

/**
 * A okhttp-based {@link ConnectionClientTransport} implementation.
 */
class OkHttpClientTransport implements ConnectionClientTransport, TransportExceptionHandler {
  private static final Map<ErrorCode, Status> ERROR_CODE_TO_STATUS = buildErrorCodeToStatusMap();
  private static final Logger log = Logger.getLogger(OkHttpClientTransport.class.getName());
  private static final OkHttpClientStream[] EMPTY_STREAM_ARRAY = new OkHttpClientStream[0];

  private static Map<ErrorCode, Status> buildErrorCodeToStatusMap() {
    Map<ErrorCode, Status> errorToStatus = new EnumMap<ErrorCode, Status>(ErrorCode.class);
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
    return Collections.unmodifiableMap(errorToStatus);
  }

  private final InetSocketAddress address;
  private final String defaultAuthority;
  private final String userAgent;
  private final Random random = new Random();
  // Returns new unstarted stopwatches
  private final Supplier<Stopwatch> stopwatchFactory;
  private Listener listener;
  private FrameReader testFrameReader;
  private AsyncFrameWriter frameWriter;
  private OutboundFlowController outboundFlow;
  private final Object lock = new Object();
  private final InternalLogId logId = InternalLogId.allocate(getClass().getName());
  @GuardedBy("lock")
  private int nextStreamId;
  @GuardedBy("lock")
  private final Map<Integer, OkHttpClientStream> streams =
      new HashMap<Integer, OkHttpClientStream>();
  private final Executor executor;
  // Wrap on executor, to guarantee some operations be executed serially.
  private final SerializingExecutor serializingExecutor;
  private final int maxMessageSize;
  private int connectionUnacknowledgedBytesRead;
  private ClientFrameHandler clientFrameHandler;
  // Caution: Not synchronized, new value can only be safely read after the connection is complete.
  private Attributes attributes = Attributes.EMPTY;
  /**
   * Indicates the transport is in go-away state: no new streams will be processed, but existing
   * streams may continue.
   */
  @GuardedBy("lock")
  private Status goAwayStatus;
  @GuardedBy("lock")
  private boolean goAwaySent;
  @GuardedBy("lock")
  private Http2Ping ping;
  @GuardedBy("lock")
  private boolean stopped;
  @GuardedBy("lock")
  private boolean inUse;
  private SSLSocketFactory sslSocketFactory;
  private HostnameVerifier hostnameVerifier;
  private Socket socket;
  @GuardedBy("lock")
  private int maxConcurrentStreams = 0;
  @SuppressWarnings("JdkObsolete") // Usage is bursty; want low memory usage when empty
  @GuardedBy("lock")
  private LinkedList<OkHttpClientStream> pendingStreams = new LinkedList<OkHttpClientStream>();
  private final ConnectionSpec connectionSpec;
  private FrameWriter testFrameWriter;
  private ScheduledExecutorService scheduler;
  private KeepAliveManager keepAliveManager;
  private boolean enableKeepAlive;
  private long keepAliveTimeNanos;
  private long keepAliveTimeoutNanos;
  private boolean keepAliveWithoutCalls;
  private final Runnable tooManyPingsRunnable;
  @GuardedBy("lock")
  private final TransportTracer transportTracer;
  @GuardedBy("lock")
  private InternalChannelz.Security securityInfo;

  @VisibleForTesting
  @Nullable
  final ProxyParameters proxy;

  // The following fields should only be used for test.
  Runnable connectingCallback;
  SettableFuture<Void> connectedFuture;


  OkHttpClientTransport(InetSocketAddress address, String authority, @Nullable String userAgent,
      Executor executor, @Nullable SSLSocketFactory sslSocketFactory,
      @Nullable HostnameVerifier hostnameVerifier, ConnectionSpec connectionSpec,
      int maxMessageSize, @Nullable ProxyParameters proxy, Runnable tooManyPingsRunnable,
      TransportTracer transportTracer) {
    this.address = Preconditions.checkNotNull(address, "address");
    this.defaultAuthority = authority;
    this.maxMessageSize = maxMessageSize;
    this.executor = Preconditions.checkNotNull(executor, "executor");
    serializingExecutor = new SerializingExecutor(executor);
    // Client initiated streams are odd, server initiated ones are even. Server should not need to
    // use it. We start clients at 3 to avoid conflicting with HTTP negotiation.
    nextStreamId = 3;
    this.sslSocketFactory = sslSocketFactory;
    this.hostnameVerifier = hostnameVerifier;
    this.connectionSpec = Preconditions.checkNotNull(connectionSpec, "connectionSpec");
    this.stopwatchFactory = GrpcUtil.STOPWATCH_SUPPLIER;
    this.userAgent = GrpcUtil.getGrpcUserAgent("okhttp", userAgent);
    this.proxy = proxy;
    this.tooManyPingsRunnable =
        Preconditions.checkNotNull(tooManyPingsRunnable, "tooManyPingsRunnable");
    this.transportTracer = Preconditions.checkNotNull(transportTracer);
    initTransportTracer();
  }

  /**
   * Create a transport connected to a fake peer for test.
   */
  @VisibleForTesting
  OkHttpClientTransport(
      String userAgent,
      Executor executor,
      FrameReader frameReader,
      FrameWriter testFrameWriter,
      int nextStreamId,
      Socket socket,
      Supplier<Stopwatch> stopwatchFactory,
      @Nullable Runnable connectingCallback,
      SettableFuture<Void> connectedFuture,
      int maxMessageSize,
      Runnable tooManyPingsRunnable,
      TransportTracer transportTracer) {
    address = null;
    this.maxMessageSize = maxMessageSize;
    defaultAuthority = "notarealauthority:80";
    this.userAgent = GrpcUtil.getGrpcUserAgent("okhttp", userAgent);
    this.executor = Preconditions.checkNotNull(executor, "executor");
    serializingExecutor = new SerializingExecutor(executor);
    this.testFrameReader = Preconditions.checkNotNull(frameReader, "frameReader");
    this.testFrameWriter = Preconditions.checkNotNull(testFrameWriter, "testFrameWriter");
    this.socket = Preconditions.checkNotNull(socket, "socket");
    this.nextStreamId = nextStreamId;
    this.stopwatchFactory = stopwatchFactory;
    this.connectionSpec = null;
    this.connectingCallback = connectingCallback;
    this.connectedFuture = Preconditions.checkNotNull(connectedFuture, "connectedFuture");
    this.proxy = null;
    this.tooManyPingsRunnable =
        Preconditions.checkNotNull(tooManyPingsRunnable, "tooManyPingsRunnable");
    this.transportTracer = Preconditions.checkNotNull(transportTracer, "transportTracer");
    initTransportTracer();
  }

  private void initTransportTracer() {
    synchronized (lock) { // to make @GuardedBy linter happy
      transportTracer.setFlowControlWindowReader(new TransportTracer.FlowControlReader() {
        @Override
        public TransportTracer.FlowControlWindows read() {
          synchronized (lock) {
            long local = -1; // okhttp does not track the local window size
            long remote = outboundFlow == null ? -1 : outboundFlow.windowUpdate(null, 0);
            return new TransportTracer.FlowControlWindows(local, remote);
          }
        }
      });
    }
  }

  /**
   * Enable keepalive with custom delay and timeout.
   */
  void enableKeepAlive(boolean enable, long keepAliveTimeNanos,
      long keepAliveTimeoutNanos, boolean keepAliveWithoutCalls) {
    enableKeepAlive = enable;
    this.keepAliveTimeNanos = keepAliveTimeNanos;
    this.keepAliveTimeoutNanos = keepAliveTimeoutNanos;
    this.keepAliveWithoutCalls = keepAliveWithoutCalls;
  }

  private boolean isForTest() {
    return address == null;
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
        Stopwatch stopwatch = stopwatchFactory.get();
        stopwatch.start();
        p = ping = new Http2Ping(data, stopwatch);
        writePing = true;
        transportTracer.reportKeepAliveSent();
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
  public OkHttpClientStream newStream(final MethodDescriptor<?, ?> method,
      final Metadata headers, CallOptions callOptions) {
    Preconditions.checkNotNull(method, "method");
    Preconditions.checkNotNull(headers, "headers");
    StatsTraceContext statsTraceCtx = StatsTraceContext.newClientContext(callOptions, headers);
    return new OkHttpClientStream(
        method,
        headers,
        frameWriter,
        OkHttpClientTransport.this,
        outboundFlow,
        lock,
        maxMessageSize,
        defaultAuthority,
        userAgent,
        statsTraceCtx,
        transportTracer);
  }

  @GuardedBy("lock")
  void streamReadyToStart(OkHttpClientStream clientStream) {
    if (goAwayStatus != null) {
      clientStream.transportState().transportReportStatus(
          goAwayStatus, RpcProgress.REFUSED, true, new Metadata());
    } else if (streams.size() >= maxConcurrentStreams) {
      pendingStreams.add(clientStream);
      setInUse();
    } else {
      startStream(clientStream);
    }
  }

  @GuardedBy("lock")
  private void startStream(OkHttpClientStream stream) {
    Preconditions.checkState(
        stream.id() == OkHttpClientStream.ABSENT_ID, "StreamId already assigned");
    streams.put(nextStreamId, stream);
    setInUse();
    stream.transportState().start(nextStreamId);
    // For unary and server streaming, there will be a data frame soon, no need to flush the header.
    if ((stream.getType() != MethodType.UNARY && stream.getType() != MethodType.SERVER_STREAMING)
        || stream.useGet()) {
      frameWriter.flush();
    }
    if (nextStreamId >= Integer.MAX_VALUE - 2) {
      // Make sure nextStreamId greater than all used id, so that mayHaveCreatedStream() performs
      // correctly.
      nextStreamId = Integer.MAX_VALUE;
      startGoAway(Integer.MAX_VALUE, ErrorCode.NO_ERROR,
          Status.UNAVAILABLE.withDescription("Stream ids exhausted"));
    } else {
      nextStreamId += 2;
    }
  }

  /**
   * Starts pending streams, returns true if at least one pending stream is started.
   */
  @GuardedBy("lock")
  private boolean startPendingStreams() {
    boolean hasStreamStarted = false;
    while (!pendingStreams.isEmpty() && streams.size() < maxConcurrentStreams) {
      OkHttpClientStream stream = pendingStreams.poll();
      startStream(stream);
      hasStreamStarted = true;
    }
    return hasStreamStarted;
  }

  /**
   * Removes given pending stream, used when a pending stream is cancelled.
   */
  @GuardedBy("lock")
  void removePendingStream(OkHttpClientStream pendingStream) {
    pendingStreams.remove(pendingStream);
    maybeClearInUse();
  }

  @Override
  public Runnable start(Listener listener) {
    this.listener = Preconditions.checkNotNull(listener, "listener");

    if (enableKeepAlive) {
      scheduler = SharedResourceHolder.get(TIMER_SERVICE);
      keepAliveManager = new KeepAliveManager(
          new ClientKeepAlivePinger(this), scheduler, keepAliveTimeNanos, keepAliveTimeoutNanos,
          keepAliveWithoutCalls);
      keepAliveManager.onTransportStarted();
    }

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
            startPendingStreams();
          }
          frameWriter.becomeConnected(testFrameWriter, socket);
          connectedFuture.set(null);
          return;
        }

        // Use closed source on failure so that the reader immediately shuts down.
        BufferedSource source = Okio.buffer(new Source() {
          @Override
          public long read(Buffer sink, long byteCount) {
            return -1;
          }

          @Override
          public Timeout timeout() {
            return Timeout.NONE;
          }

          @Override
          public void close() {}
        });
        Variant variant = new Http2();
        BufferedSink sink;
        Socket sock;
        SSLSession sslSession = null;
        try {
          if (proxy == null) {
            sock = new Socket(address.getAddress(), address.getPort());
          } else {
            sock = createHttpProxySocket(
                address, proxy.proxyAddress, proxy.username, proxy.password);
          }

          if (sslSocketFactory != null) {
            SSLSocket sslSocket = OkHttpTlsUpgrader.upgrade(
                sslSocketFactory, hostnameVerifier, sock, getOverridenHost(), getOverridenPort(),
                connectionSpec);
            sslSession = sslSocket.getSession();
            sock = sslSocket;
          }
          sock.setTcpNoDelay(true);
          source = Okio.buffer(Okio.source(sock));
          sink = Okio.buffer(Okio.sink(sock));
          // The return value of OkHttpTlsUpgrader.upgrade is an SSLSocket that has this info
          attributes = Attributes
              .newBuilder()
              .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, sock.getRemoteSocketAddress())
              .set(Grpc.TRANSPORT_ATTR_SSL_SESSION, sslSession)
              .set(CallCredentials.ATTR_SECURITY_LEVEL,
                  sslSession == null ? SecurityLevel.NONE : SecurityLevel.PRIVACY_AND_INTEGRITY)
              .build();
        } catch (StatusException e) {
          startGoAway(0, ErrorCode.INTERNAL_ERROR, e.getStatus());
          return;
        } catch (Exception e) {
          onException(e);
          return;
        } finally {
          clientFrameHandler = new ClientFrameHandler(variant.newReader(source, true));
          executor.execute(clientFrameHandler);
        }

        FrameWriter rawFrameWriter;
        synchronized (lock) {
          socket = Preconditions.checkNotNull(sock, "socket");
          maxConcurrentStreams = Integer.MAX_VALUE;
          startPendingStreams();
          if (sslSession != null) {
            securityInfo = new InternalChannelz.Security(new InternalChannelz.Tls(sslSession));
          }
        }

        rawFrameWriter = variant.newWriter(sink, true);
        frameWriter.becomeConnected(rawFrameWriter, socket);

        try {
          // Do these with the raw FrameWriter, so that they will be done in this thread,
          // and before any possible pending stream operations.
          rawFrameWriter.connectionPreface();
          Settings settings = new Settings();
          rawFrameWriter.settings(settings);
        } catch (Exception e) {
          onException(e);
          return;
        }
      }
    });
    return null;
  }

  private Socket createHttpProxySocket(InetSocketAddress address, InetSocketAddress proxyAddress,
      String proxyUsername, String proxyPassword) throws IOException, StatusException {
    try {
      Socket sock;
      // The proxy address may not be resolved
      if (proxyAddress.getAddress() != null) {
        sock = new Socket(proxyAddress.getAddress(), proxyAddress.getPort());
      } else {
        sock = new Socket(proxyAddress.getHostName(), proxyAddress.getPort());
      }
      sock.setTcpNoDelay(true);

      Source source = Okio.source(sock);
      BufferedSink sink = Okio.buffer(Okio.sink(sock));

      // Prepare headers and request method line
      Request proxyRequest = createHttpProxyRequest(address, proxyUsername, proxyPassword);
      HttpUrl url = proxyRequest.httpUrl();
      String requestLine = String.format("CONNECT %s:%d HTTP/1.1", url.host(), url.port());

      // Write request to socket
      sink.writeUtf8(requestLine).writeUtf8("\r\n");
      for (int i = 0, size = proxyRequest.headers().size(); i < size; i++) {
        sink.writeUtf8(proxyRequest.headers().name(i))
            .writeUtf8(": ")
            .writeUtf8(proxyRequest.headers().value(i))
            .writeUtf8("\r\n");
      }
      sink.writeUtf8("\r\n");
      // Flush buffer (flushes socket and sends request)
      sink.flush();

      // Read status line, check if 2xx was returned
      StatusLine statusLine = StatusLine.parse(readUtf8LineStrictUnbuffered(source));
      // Drain rest of headers
      while (!readUtf8LineStrictUnbuffered(source).equals("")) {}
      if (statusLine.code < 200 || statusLine.code >= 300) {
        Buffer body = new Buffer();
        try {
          sock.shutdownOutput();
          source.read(body, 1024);
        } catch (IOException ex) {
          body.writeUtf8("Unable to read body: " + ex.toString());
        }
        try {
          sock.close();
        } catch (IOException ignored) {
          // ignored
        }
        String message = String.format(
            "Response returned from proxy was not successful (expected 2xx, got %d %s). "
              + "Response body:\n%s",
            statusLine.code, statusLine.message, body.readUtf8());
        throw Status.UNAVAILABLE.withDescription(message).asException();
      }
      return sock;
    } catch (IOException e) {
      throw Status.UNAVAILABLE.withDescription("Failed trying to connect with proxy").withCause(e)
          .asException();
    }
  }

  private Request createHttpProxyRequest(InetSocketAddress address, String proxyUsername,
      String proxyPassword) {
    HttpUrl tunnelUrl = new HttpUrl.Builder()
        .scheme("https")
        .host(address.getHostName())
        .port(address.getPort())
        .build();
    Request.Builder request = new Request.Builder()
        .url(tunnelUrl)
        .header("Host", tunnelUrl.host() + ":" + tunnelUrl.port())
        .header("User-Agent", userAgent);

    // If we have proxy credentials, set them right away
    if (proxyUsername != null && proxyPassword != null) {
      request.header("Proxy-Authorization", Credentials.basic(proxyUsername, proxyPassword));
    }
    return request.build();
  }

  private static String readUtf8LineStrictUnbuffered(Source source) throws IOException {
    Buffer buffer = new Buffer();
    while (true) {
      if (source.read(buffer, 1) == -1) {
        throw new EOFException("\\n not found: " + buffer.readByteString().hex());
      }
      if (buffer.getByte(buffer.size() - 1) == '\n') {
        return buffer.readUtf8LineStrict();
      }
    }
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("logId", logId.getId())
        .add("address", address)
        .toString();
  }

  @Override
  public InternalLogId getLogId() {
    return logId;
  }

  /**
   * Gets the overridden authority hostname.  If the authority is overridden to be an invalid
   * authority, uri.getHost() will (rightly) return null, since the authority is no longer
   * an actual service.  This method overrides the behavior for practical reasons.  For example,
   * if an authority is in the form "invalid_authority" (note the "_"), rather than return null,
   * we return the input.  This is because the return value, in conjunction with getOverridenPort,
   * are used by the SSL library to reconstruct the actual authority.  It /already/ has a
   * connection to the port, independent of this function.
   *
   * <p>Note: if the defaultAuthority has a port number in it and is also bad, this code will do
   * the wrong thing.  An example wrong behavior would be "invalid_host:443".   Registry based
   * authorities do not have ports, so this is even more wrong than before.  Sorry.
   */
  @VisibleForTesting
  String getOverridenHost() {
    URI uri = GrpcUtil.authorityToUri(defaultAuthority);
    if (uri.getHost() != null) {
      return uri.getHost();
    }

    return defaultAuthority;
  }

  @VisibleForTesting
  int getOverridenPort() {
    URI uri = GrpcUtil.authorityToUri(defaultAuthority);
    if (uri.getPort() != -1) {
      return uri.getPort();
    }

    return address.getPort();
  }

  @Override
  public void shutdown(Status reason) {
    synchronized (lock) {
      if (goAwayStatus != null) {
        return;
      }

      goAwayStatus = reason;
      listener.transportShutdown(goAwayStatus);
      stopIfNecessary();
    }
  }

  @Override
  public void shutdownNow(Status reason) {
    shutdown(reason);
    synchronized (lock) {
      Iterator<Map.Entry<Integer, OkHttpClientStream>> it = streams.entrySet().iterator();
      while (it.hasNext()) {
        Map.Entry<Integer, OkHttpClientStream> entry = it.next();
        it.remove();
        entry.getValue().transportState().transportReportStatus(reason, false, new Metadata());
      }

      for (OkHttpClientStream stream : pendingStreams) {
        stream.transportState().transportReportStatus(reason, true, new Metadata());
      }
      pendingStreams.clear();
      maybeClearInUse();

      stopIfNecessary();
    }
  }

  @Override
  public Attributes getAttributes() {
    return attributes;
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
  @Override
  public void onException(Throwable failureCause) {
    Preconditions.checkNotNull(failureCause, "failureCause");
    Status status = Status.UNAVAILABLE.withCause(failureCause);
    startGoAway(0, ErrorCode.INTERNAL_ERROR, status);
  }

  /**
   * Send GOAWAY to the server, then finish all active streams and close the transport.
   */
  private void onError(ErrorCode errorCode, String moreDetail) {
    startGoAway(0, errorCode, toGrpcStatus(errorCode).augmentDescription(moreDetail));
  }

  private void startGoAway(int lastKnownStreamId, ErrorCode errorCode, Status status) {
    synchronized (lock) {
      if (goAwayStatus == null) {
        goAwayStatus = status;
        listener.transportShutdown(status);
      }
      if (errorCode != null && !goAwaySent) {
        // Send GOAWAY with lastGoodStreamId of 0, since we don't expect any server-initiated
        // streams. The GOAWAY is part of graceful shutdown.
        goAwaySent = true;
        frameWriter.goAway(0, errorCode, new byte[0]);
      }

      Iterator<Map.Entry<Integer, OkHttpClientStream>> it = streams.entrySet().iterator();
      while (it.hasNext()) {
        Map.Entry<Integer, OkHttpClientStream> entry = it.next();
        if (entry.getKey() > lastKnownStreamId) {
          it.remove();
          entry.getValue().transportState().transportReportStatus(
              status, RpcProgress.REFUSED, false, new Metadata());
        }
      }

      for (OkHttpClientStream stream : pendingStreams) {
        stream.transportState().transportReportStatus(
            status, RpcProgress.REFUSED, true, new Metadata());
      }
      pendingStreams.clear();
      maybeClearInUse();

      stopIfNecessary();
    }
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
   * @param stopDelivery interrupt queued messages in the deframer
   * @param errorCode reset the stream with this ErrorCode if not null.
   * @param trailers the trailers received if not null
   */
  void finishStream(
      int streamId,
      @Nullable Status status,
      RpcProgress rpcProgress,
      boolean stopDelivery,
      @Nullable ErrorCode errorCode,
      @Nullable Metadata trailers) {
    synchronized (lock) {
      OkHttpClientStream stream = streams.remove(streamId);
      if (stream != null) {
        if (errorCode != null) {
          frameWriter.rstStream(streamId, ErrorCode.CANCEL);
        }
        if (status != null) {
          stream
              .transportState()
              .transportReportStatus(
                  status,
                  rpcProgress,
                  stopDelivery,
                  trailers != null ? trailers : new Metadata());
        }
        if (!startPendingStreams()) {
          stopIfNecessary();
          maybeClearInUse();
        }
      }
    }
  }

  /**
   * When the transport is in goAway state, we should stop it once all active streams finish.
   */
  @GuardedBy("lock")
  private void stopIfNecessary() {
    if (!(goAwayStatus != null && streams.isEmpty() && pendingStreams.isEmpty())) {
      return;
    }
    if (stopped) {
      return;
    }
    stopped = true;

    if (keepAliveManager != null) {
      keepAliveManager.onTransportTermination();
      // KeepAliveManager should stop using the scheduler after onTransportTermination gets called.
      scheduler = SharedResourceHolder.release(TIMER_SERVICE, scheduler);
    }

    if (ping != null) {
      ping.failed(getPingFailure());
      ping = null;
    }

    if (!goAwaySent) {
      // Send GOAWAY with lastGoodStreamId of 0, since we don't expect any server-initiated
      // streams. The GOAWAY is part of graceful shutdown.
      goAwaySent = true;
      frameWriter.goAway(0, ErrorCode.NO_ERROR, new byte[0]);
    }

    // We will close the underlying socket in the writing thread to break out the reader
    // thread, which will close the frameReader and notify the listener.
    frameWriter.close();
  }

  @GuardedBy("lock")
  private void maybeClearInUse() {
    if (inUse) {
      if (pendingStreams.isEmpty() && streams.isEmpty()) {
        inUse = false;
        listener.transportInUse(false);
        if (keepAliveManager != null) {
          // We don't have any active streams. No need to do keepalives any more.
          // Again, we have to call this inside the lock to avoid the race between onTransportIdle
          // and onTransportActive.
          keepAliveManager.onTransportIdle();
        }
      }
    }
  }

  @GuardedBy("lock")
  private void setInUse() {
    if (!inUse) {
      inUse = true;
      listener.transportInUse(true);
      if (keepAliveManager != null) {
        // We have a new stream. We might need to do keepalives now.
        // Note that we have to do this inside the lock to avoid calling
        // KeepAliveManager.onTransportActive and KeepAliveManager.onTransportIdle in the wrong
        // order.
        keepAliveManager.onTransportActive();
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

  @Override
  public ListenableFuture<SocketStats> getStats() {
    SettableFuture<SocketStats> ret = SettableFuture.create();
    synchronized (lock) {
      if (socket == null) {
        ret.set(new SocketStats(
            transportTracer.getStats(),
            /*local=*/ null,
            /*remote=*/ null,
            new InternalChannelz.SocketOptions.Builder().build(),
            /*security=*/ null));
      } else {
        ret.set(new SocketStats(
            transportTracer.getStats(),
            socket.getLocalSocketAddress(),
            socket.getRemoteSocketAddress(),
            Utils.getSocketOptions(socket),
            securityInfo));
      }
      return ret;
    }
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
      if (!GrpcUtil.IS_RESTRICTED_APPENGINE) {
        Thread.currentThread().setName("OkHttpClientTransport");
      }
      try {
        // Read until the underlying socket closes.
        while (frameReader.nextFrame(this)) {
          if (keepAliveManager != null) {
            keepAliveManager.onDataReceived();
          }
        }
        // frameReader.nextFrame() returns false when the underlying read encounters an IOException,
        // it may be triggered by the socket closing, in such case, the startGoAway() will do
        // nothing, otherwise, we finish all streams since it's a real IO issue.
        startGoAway(0, ErrorCode.INTERNAL_ERROR,
            Status.UNAVAILABLE.withDescription("End of stream or IOException"));
      } catch (Throwable t) {
        // TODO(madongfly): Send the exception message to the server.
        startGoAway(
            0, 
            ErrorCode.PROTOCOL_ERROR, 
            Status.UNAVAILABLE.withDescription("error in frame handler").withCause(t));
      } finally {
        try {
          frameReader.close();
        } catch (IOException ex) {
          log.log(Level.INFO, "Exception closing frame reader", ex);
        }
        listener.transportTerminated();
        if (!GrpcUtil.IS_RESTRICTED_APPENGINE) {
          // Restore the original thread name.
          Thread.currentThread().setName(threadName);
        }
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
          in.skip(length);
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
          stream.transportState().transportDataReceived(buf, inFinished);
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
          stream.transportState().transportHeadersReceived(headerBlock, inFinished);
        }
      }
      if (unknownStream) {
        // We don't expect any server-initiated streams.
        onError(ErrorCode.PROTOCOL_ERROR, "Received header for unknown stream: " + streamId);
      }
    }

    @Override
    public void rstStream(int streamId, ErrorCode errorCode) {
      Status status = toGrpcStatus(errorCode).augmentDescription("Rst Stream");
      boolean stopDelivery =
          (status.getCode() == Code.CANCELLED || status.getCode() == Code.DEADLINE_EXCEEDED);
      finishStream(
          streamId, status,
          errorCode == ErrorCode.REFUSED_STREAM ? RpcProgress.REFUSED : RpcProgress.PROCESSED,
          stopDelivery, null, null);
    }

    @Override
    public void settings(boolean clearPrevious, Settings settings) {
      boolean outboundWindowSizeIncreased = false;
      synchronized (lock) {
        if (OkHttpSettingsUtil.isSet(settings, OkHttpSettingsUtil.MAX_CONCURRENT_STREAMS)) {
          int receivedMaxConcurrentStreams = OkHttpSettingsUtil.get(
              settings, OkHttpSettingsUtil.MAX_CONCURRENT_STREAMS);
          maxConcurrentStreams = receivedMaxConcurrentStreams;
        }

        if (OkHttpSettingsUtil.isSet(settings, OkHttpSettingsUtil.INITIAL_WINDOW_SIZE)) {
          int initialWindowSize = OkHttpSettingsUtil.get(
              settings, OkHttpSettingsUtil.INITIAL_WINDOW_SIZE);
          outboundWindowSizeIncreased = outboundFlow.initialOutboundWindowSize(initialWindowSize);
        }
        if (firstSettings) {
          listener.transportReady();
          firstSettings = false;
        }

        // The changed settings are not finalized until SETTINGS acknowledgment frame is sent. Any
        // writes due to update in settings must be sent after SETTINGS acknowledgment frame,
        // otherwise it will cause a stream error (RST_STREAM).
        frameWriter.ackSettings(settings);

        // send any pending bytes / streams
        if (outboundWindowSizeIncreased) {
          outboundFlow.writeStreams();
        }
        startPendingStreams();
      }
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
      if (errorCode == ErrorCode.ENHANCE_YOUR_CALM) {
        String data = debugData.utf8();
        log.log(Level.WARNING, String.format(
            "%s: Received GOAWAY with ENHANCE_YOUR_CALM. Debug data: %s", this, data));
        if ("too_many_pings".equals(data)) {
          tooManyPingsRunnable.run();
        }
      }
      Status status = GrpcUtil.Http2Error.statusForCode(errorCode.httpCode)
          .augmentDescription("Received Goaway");
      if (debugData.size() > 0) {
        // If a debug message was provided, use it.
        status = status.augmentDescription(debugData.utf8());
      }
      startGoAway(lastGoodStreamId, null, status);
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
          finishStream(
              streamId, Status.INTERNAL.withDescription(errorMsg), RpcProgress.PROCESSED, false,
              ErrorCode.PROTOCOL_ERROR, null);
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
