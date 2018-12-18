/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.inprocess;

import static com.google.common.base.Preconditions.checkNotNull;
import static io.grpc.internal.GrpcUtil.TIMEOUT_KEY;
import static java.lang.Math.max;

import com.google.common.base.MoreObjects;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Compressor;
import io.grpc.Deadline;
import io.grpc.Decompressor;
import io.grpc.DecompressorRegistry;
import io.grpc.Grpc;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalLogId;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.SecurityLevel;
import io.grpc.ServerStreamTracer;
import io.grpc.Status;
import io.grpc.internal.ClientStream;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ConnectionClientTransport;
import io.grpc.internal.GrpcAttributes;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.InUseStateAggregator;
import io.grpc.internal.ManagedClientTransport;
import io.grpc.internal.NoopClientStream;
import io.grpc.internal.ObjectPool;
import io.grpc.internal.ServerStream;
import io.grpc.internal.ServerStreamListener;
import io.grpc.internal.ServerTransport;
import io.grpc.internal.ServerTransportListener;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.StreamListener;
import java.io.InputStream;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

@ThreadSafe
final class InProcessTransport implements ServerTransport, ConnectionClientTransport {
  private static final Logger log = Logger.getLogger(InProcessTransport.class.getName());

  private final InternalLogId logId;
  private final String name;
  private final int clientMaxInboundMetadataSize;
  private final String authority;
  private final String userAgent;
  private int serverMaxInboundMetadataSize;
  private ObjectPool<ScheduledExecutorService> serverSchedulerPool;
  private ScheduledExecutorService serverScheduler;
  private ServerTransportListener serverTransportListener;
  private Attributes serverStreamAttributes;
  private ManagedClientTransport.Listener clientTransportListener;
  @GuardedBy("this")
  private boolean shutdown;
  @GuardedBy("this")
  private boolean terminated;
  @GuardedBy("this")
  private Status shutdownStatus;
  @GuardedBy("this")
  private Set<InProcessStream> streams = new HashSet<InProcessStream>();
  @GuardedBy("this")
  private List<ServerStreamTracer.Factory> serverStreamTracerFactories;
  private final Attributes attributes = Attributes.newBuilder()
      .set(GrpcAttributes.ATTR_SECURITY_LEVEL, SecurityLevel.PRIVACY_AND_INTEGRITY)
      .build();
  @GuardedBy("this")
  private final InUseStateAggregator<InProcessStream> inUseState =
      new InUseStateAggregator<InProcessStream>() {
        @Override
        protected void handleInUse() {
          clientTransportListener.transportInUse(true);
        }

        @Override
        protected void handleNotInUse() {
          clientTransportListener.transportInUse(false);
        }
      };

  public InProcessTransport(
      String name, int maxInboundMetadataSize, String authority, String userAgent) {
    this.name = name;
    this.clientMaxInboundMetadataSize = maxInboundMetadataSize;
    this.authority = authority;
    this.userAgent = GrpcUtil.getGrpcUserAgent("inprocess", userAgent);
    logId = InternalLogId.allocate(getClass(), name);
  }

  @CheckReturnValue
  @Override
  public synchronized Runnable start(ManagedClientTransport.Listener listener) {
    this.clientTransportListener = listener;
    InProcessServer server = InProcessServer.findServer(name);
    if (server != null) {
      serverMaxInboundMetadataSize = server.getMaxInboundMetadataSize();
      serverSchedulerPool = server.getScheduledExecutorServicePool();
      serverScheduler = serverSchedulerPool.getObject();
      serverStreamTracerFactories = server.getStreamTracerFactories();
      // Must be semi-initialized; past this point, can begin receiving requests
      serverTransportListener = server.register(this);
    }
    if (serverTransportListener == null) {
      shutdownStatus = Status.UNAVAILABLE.withDescription("Could not find server: " + name);
      final Status localShutdownStatus = shutdownStatus;
      return new Runnable() {
        @Override
        public void run() {
          synchronized (InProcessTransport.this) {
            notifyShutdown(localShutdownStatus);
            notifyTerminated();
          }
        }
      };
    }
    return new Runnable() {
      @Override
      @SuppressWarnings("deprecation")
      public void run() {
        synchronized (InProcessTransport.this) {
          Attributes serverTransportAttrs = Attributes.newBuilder()
              .set(Grpc.TRANSPORT_ATTR_REMOTE_ADDR, new InProcessSocketAddress(name))
              .set(Grpc.TRANSPORT_ATTR_LOCAL_ADDR, new InProcessSocketAddress(name))
              .build();
          serverStreamAttributes = serverTransportListener.transportReady(serverTransportAttrs);
          clientTransportListener.transportReady();
        }
      }
    };
  }

  @Override
  public synchronized ClientStream newStream(
      final MethodDescriptor<?, ?> method, final Metadata headers, final CallOptions callOptions) {
    if (shutdownStatus != null) {
      return failedClientStream(
          StatsTraceContext.newClientContext(callOptions, headers), shutdownStatus);
    }

    headers.put(GrpcUtil.USER_AGENT_KEY, userAgent);

    if (serverMaxInboundMetadataSize != Integer.MAX_VALUE) {
      int metadataSize = metadataSize(headers);
      if (metadataSize > serverMaxInboundMetadataSize) {
        // Other transports would compute a status with:
        //   GrpcUtil.httpStatusToGrpcStatus(431 /* Request Header Fields Too Large */);
        // However, that isn't handled specially today, so we'd leak HTTP-isms even though we're
        // in-process. We go ahead and make a Status, which may need to be updated if
        // statuscodes.md is updated.
        Status status = Status.RESOURCE_EXHAUSTED.withDescription(
            String.format(
                "Request metadata larger than %d: %d",
                serverMaxInboundMetadataSize,
                metadataSize));
        return failedClientStream(
            StatsTraceContext.newClientContext(callOptions, headers), status);
      }
    }

    return new InProcessStream(method, headers, callOptions, authority).clientStream;
  }

  private ClientStream failedClientStream(
      final StatsTraceContext statsTraceCtx, final Status status) {
    return new NoopClientStream() {
        @Override
        public void start(ClientStreamListener listener) {
          statsTraceCtx.clientOutboundHeaders();
          statsTraceCtx.streamClosed(status);
          listener.closed(status, new Metadata());
        }
      };
  }

  @Override
  public synchronized void ping(final PingCallback callback, Executor executor) {
    if (terminated) {
      final Status shutdownStatus = this.shutdownStatus;
      executor.execute(new Runnable() {
        @Override
        public void run() {
          callback.onFailure(shutdownStatus.asRuntimeException());
        }
      });
    } else {
      executor.execute(new Runnable() {
        @Override
        public void run() {
          callback.onSuccess(0);
        }
      });
    }
  }

  @Override
  public synchronized void shutdown(Status reason) {
    // Can be called multiple times: once for ManagedClientTransport, once for ServerTransport.
    if (shutdown) {
      return;
    }
    shutdownStatus = reason;
    notifyShutdown(reason);
    if (streams.isEmpty()) {
      notifyTerminated();
    }
  }

  @Override
  public synchronized void shutdown() {
    shutdown(Status.UNAVAILABLE.withDescription("InProcessTransport shutdown by the server-side"));
  }

  @Override
  public void shutdownNow(Status reason) {
    checkNotNull(reason, "reason");
    List<InProcessStream> streamsCopy;
    synchronized (this) {
      shutdown(reason);
      if (terminated) {
        return;
      }
      streamsCopy = new ArrayList<>(streams);
    }
    for (InProcessStream stream : streamsCopy) {
      stream.clientStream.cancel(reason);
    }
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("logId", logId.getId())
        .add("name", name)
        .toString();
  }

  @Override
  public InternalLogId getLogId() {
    return logId;
  }

  @Override
  public Attributes getAttributes() {
    return attributes;
  }

  @Override
  public ScheduledExecutorService getScheduledExecutorService() {
    return serverScheduler;
  }

  @Override
  public ListenableFuture<SocketStats> getStats() {
    SettableFuture<SocketStats> ret = SettableFuture.create();
    ret.set(null);
    return ret;
  }

  private synchronized void notifyShutdown(Status s) {
    if (shutdown) {
      return;
    }
    shutdown = true;
    clientTransportListener.transportShutdown(s);
  }

  private synchronized void notifyTerminated() {
    if (terminated) {
      return;
    }
    terminated = true;
    if (serverScheduler != null) {
      serverScheduler = serverSchedulerPool.returnObject(serverScheduler);
    }
    clientTransportListener.transportTerminated();
    if (serverTransportListener != null) {
      serverTransportListener.transportTerminated();
    }
  }

  private static int metadataSize(Metadata metadata) {
    byte[][] serialized = InternalMetadata.serialize(metadata);
    if (serialized == null) {
      return 0;
    }
    // Calculate based on SETTINGS_MAX_HEADER_LIST_SIZE in RFC 7540 §6.5.2. We could use something
    // different, but it's "sane."
    long size = 0;
    for (int i = 0; i < serialized.length; i += 2) {
      size += 32 + serialized[i].length + serialized[i + 1].length;
    }
    size = Math.min(size, Integer.MAX_VALUE);
    return (int) size;
  }

  private class InProcessStream {
    private final InProcessClientStream clientStream;
    private final InProcessServerStream serverStream;
    private final CallOptions callOptions;
    private final Metadata headers;
    private final MethodDescriptor<?, ?> method;
    private volatile String authority;

    private InProcessStream(
        MethodDescriptor<?, ?> method, Metadata headers, CallOptions callOptions,
        String authority) {
      this.method = checkNotNull(method, "method");
      this.headers = checkNotNull(headers, "headers");
      this.callOptions = checkNotNull(callOptions, "callOptions");
      this.authority = authority;
      this.clientStream = new InProcessClientStream(callOptions, headers);
      this.serverStream = new InProcessServerStream(method, headers);
    }

    // Can be called multiple times due to races on both client and server closing at same time.
    private void streamClosed() {
      synchronized (InProcessTransport.this) {
        boolean justRemovedAnElement = streams.remove(this);
        if (GrpcUtil.shouldBeCountedForInUse(callOptions)) {
          inUseState.updateObjectInUse(this, false);
        }
        if (streams.isEmpty() && justRemovedAnElement) {
          if (shutdown) {
            notifyTerminated();
          }
        }
      }
    }

    private class InProcessServerStream implements ServerStream {
      final StatsTraceContext statsTraceCtx;
      @GuardedBy("this")
      private ClientStreamListener clientStreamListener;
      @GuardedBy("this")
      private int clientRequested;
      @GuardedBy("this")
      private ArrayDeque<StreamListener.MessageProducer> clientReceiveQueue =
          new ArrayDeque<StreamListener.MessageProducer>();
      @GuardedBy("this")
      private Status clientNotifyStatus;
      @GuardedBy("this")
      private Metadata clientNotifyTrailers;
      // Only is intended to prevent double-close when client cancels.
      @GuardedBy("this")
      private boolean closed;
      @GuardedBy("this")
      private int outboundSeqNo;

      InProcessServerStream(MethodDescriptor<?, ?> method, Metadata headers) {
        statsTraceCtx = StatsTraceContext.newServerContext(
            serverStreamTracerFactories, method.getFullMethodName(), headers);
      }

      private synchronized void setListener(ClientStreamListener listener) {
        clientStreamListener = listener;
      }

      @Override
      public void setListener(ServerStreamListener serverStreamListener) {
        clientStream.setListener(serverStreamListener);
      }

      @Override
      public void request(int numMessages) {
        boolean onReady = clientStream.serverRequested(numMessages);
        if (onReady) {
          synchronized (this) {
            if (!closed) {
              clientStreamListener.onReady();
            }
          }
        }
      }

      // This method is the only reason we have to synchronize field accesses.
      /**
       * Client requested more messages.
       *
       * @return whether onReady should be called on the server
       */
      private synchronized boolean clientRequested(int numMessages) {
        if (closed) {
          return false;
        }
        boolean previouslyReady = clientRequested > 0;
        clientRequested += numMessages;
        while (clientRequested > 0 && !clientReceiveQueue.isEmpty()) {
          clientRequested--;
          clientStreamListener.messagesAvailable(clientReceiveQueue.poll());
        }
        // Attempt being reentrant-safe
        if (closed) {
          return false;
        }
        if (clientReceiveQueue.isEmpty() && clientNotifyStatus != null) {
          closed = true;
          clientStream.statsTraceCtx.clientInboundTrailers(clientNotifyTrailers);
          clientStream.statsTraceCtx.streamClosed(clientNotifyStatus);
          clientStreamListener.closed(clientNotifyStatus, clientNotifyTrailers);
        }
        boolean nowReady = clientRequested > 0;
        return !previouslyReady && nowReady;
      }

      private void clientCancelled(Status status) {
        internalCancel(status);
      }

      @Override
      public synchronized void writeMessage(InputStream message) {
        if (closed) {
          return;
        }
        statsTraceCtx.outboundMessage(outboundSeqNo);
        statsTraceCtx.outboundMessageSent(outboundSeqNo, -1, -1);
        clientStream.statsTraceCtx.inboundMessage(outboundSeqNo);
        clientStream.statsTraceCtx.inboundMessageRead(outboundSeqNo, -1, -1);
        outboundSeqNo++;
        StreamListener.MessageProducer producer = new SingleMessageProducer(message);
        if (clientRequested > 0) {
          clientRequested--;
          clientStreamListener.messagesAvailable(producer);
        } else {
          clientReceiveQueue.add(producer);
        }
      }

      @Override
      public void flush() {}

      @Override
      public synchronized boolean isReady() {
        if (closed) {
          return false;
        }
        return clientRequested > 0;
      }

      @Override
      public void writeHeaders(Metadata headers) {
        if (clientMaxInboundMetadataSize != Integer.MAX_VALUE) {
          int metadataSize = metadataSize(headers);
          if (metadataSize > clientMaxInboundMetadataSize) {
            Status serverStatus = Status.CANCELLED.withDescription("Client cancelled the RPC");
            clientStream.serverClosed(serverStatus, serverStatus);
            // Other transports provide very little information in this case. We go ahead and make a
            // Status, which may need to be updated if statuscodes.md is updated.
            Status failedStatus = Status.RESOURCE_EXHAUSTED.withDescription(
                String.format(
                    "Response header metadata larger than %d: %d",
                    clientMaxInboundMetadataSize,
                    metadataSize));
            notifyClientClose(failedStatus, new Metadata());
            return;
          }
        }

        synchronized (this) {
          if (closed) {
            return;
          }

          clientStream.statsTraceCtx.clientInboundHeaders();
          clientStreamListener.headersRead(headers);
        }
      }

      @Override
      public void close(Status status, Metadata trailers) {
        // clientStream.serverClosed must happen before clientStreamListener.closed, otherwise
        // clientStreamListener.closed can trigger clientStream.cancel (see code in
        // ClientCalls.blockingUnaryCall), which may race with clientStream.serverClosed as both are
        // calling internalCancel().
        clientStream.serverClosed(Status.OK, status);

        if (clientMaxInboundMetadataSize != Integer.MAX_VALUE) {
          int statusSize = status.getDescription() == null ? 0 : status.getDescription().length();
          // Go ahead and throw in the status description's length, since that could be very long.
          int metadataSize = metadataSize(trailers) + statusSize;
          if (metadataSize > clientMaxInboundMetadataSize) {
            // Override the status for the client, but not the server. Transports do not guarantee
            // notifying the server of the failure.

            // Other transports provide very little information in this case. We go ahead and make a
            // Status, which may need to be updated if statuscodes.md is updated.
            status = Status.RESOURCE_EXHAUSTED.withDescription(
                String.format(
                    "Response header metadata larger than %d: %d",
                    clientMaxInboundMetadataSize,
                    metadataSize));
            trailers = new Metadata();
          }
        }

        notifyClientClose(status, trailers);
      }

      /** clientStream.serverClosed() must be called before this method */
      private void notifyClientClose(Status status, Metadata trailers) {
        Status clientStatus = stripCause(status);
        synchronized (this) {
          if (closed) {
            return;
          }
          if (clientReceiveQueue.isEmpty()) {
            closed = true;
            clientStream.statsTraceCtx.clientInboundTrailers(trailers);
            clientStream.statsTraceCtx.streamClosed(clientStatus);
            clientStreamListener.closed(clientStatus, trailers);
          } else {
            clientNotifyStatus = clientStatus;
            clientNotifyTrailers = trailers;
          }
        }

        streamClosed();
      }

      @Override
      public void cancel(Status status) {
        if (!internalCancel(Status.CANCELLED.withDescription("server cancelled stream"))) {
          return;
        }
        clientStream.serverClosed(status, status);
        streamClosed();
      }

      private synchronized boolean internalCancel(Status clientStatus) {
        if (closed) {
          return false;
        }
        closed = true;
        StreamListener.MessageProducer producer;
        while ((producer = clientReceiveQueue.poll()) != null) {
          InputStream message;
          while ((message = producer.next()) != null) {
            try {
              message.close();
            } catch (Throwable t) {
              log.log(Level.WARNING, "Exception closing stream", t);
            }
          }
        }
        clientStream.statsTraceCtx.streamClosed(clientStatus);
        clientStreamListener.closed(clientStatus, new Metadata());
        return true;
      }

      @Override
      public void setMessageCompression(boolean enable) {
        // noop
      }

      @Override
      public void setCompressor(Compressor compressor) {}

      @Override
      public void setDecompressor(Decompressor decompressor) {}

      @Override public Attributes getAttributes() {
        return serverStreamAttributes;
      }

      @Override
      public String getAuthority() {
        return InProcessStream.this.authority;
      }

      @Override
      public StatsTraceContext statsTraceContext() {
        return statsTraceCtx;
      }
    }

    private class InProcessClientStream implements ClientStream {
      final StatsTraceContext statsTraceCtx;
      final CallOptions callOptions;
      @GuardedBy("this")
      private ServerStreamListener serverStreamListener;
      @GuardedBy("this")
      private int serverRequested;
      @GuardedBy("this")
      private ArrayDeque<StreamListener.MessageProducer> serverReceiveQueue =
          new ArrayDeque<StreamListener.MessageProducer>();
      @GuardedBy("this")
      private boolean serverNotifyHalfClose;
      // Only is intended to prevent double-close when server closes.
      @GuardedBy("this")
      private boolean closed;
      @GuardedBy("this")
      private int outboundSeqNo;

      InProcessClientStream(CallOptions callOptions, Metadata headers) {
        this.callOptions = callOptions;
        statsTraceCtx = StatsTraceContext.newClientContext(callOptions, headers);
      }

      private synchronized void setListener(ServerStreamListener listener) {
        this.serverStreamListener = listener;
      }

      @Override
      public void request(int numMessages) {
        boolean onReady = serverStream.clientRequested(numMessages);
        if (onReady) {
          synchronized (this) {
            if (!closed) {
              serverStreamListener.onReady();
            }
          }
        }
      }

      // This method is the only reason we have to synchronize field accesses.
      /**
       * Client requested more messages.
       *
       * @return whether onReady should be called on the server
       */
      private synchronized boolean serverRequested(int numMessages) {
        if (closed) {
          return false;
        }
        boolean previouslyReady = serverRequested > 0;
        serverRequested += numMessages;
        while (serverRequested > 0 && !serverReceiveQueue.isEmpty()) {
          serverRequested--;
          serverStreamListener.messagesAvailable(serverReceiveQueue.poll());
        }
        if (serverReceiveQueue.isEmpty() && serverNotifyHalfClose) {
          serverNotifyHalfClose = false;
          serverStreamListener.halfClosed();
        }
        boolean nowReady = serverRequested > 0;
        return !previouslyReady && nowReady;
      }

      private void serverClosed(Status serverListenerStatus, Status serverTracerStatus) {
        internalCancel(serverListenerStatus, serverTracerStatus);
      }

      @Override
      public synchronized void writeMessage(InputStream message) {
        if (closed) {
          return;
        }
        statsTraceCtx.outboundMessage(outboundSeqNo);
        statsTraceCtx.outboundMessageSent(outboundSeqNo, -1, -1);
        serverStream.statsTraceCtx.inboundMessage(outboundSeqNo);
        serverStream.statsTraceCtx.inboundMessageRead(outboundSeqNo, -1, -1);
        outboundSeqNo++;
        StreamListener.MessageProducer producer = new SingleMessageProducer(message);
        if (serverRequested > 0) {
          serverRequested--;
          serverStreamListener.messagesAvailable(producer);
        } else {
          serverReceiveQueue.add(producer);
        }
      }

      @Override
      public void flush() {}

      @Override
      public synchronized boolean isReady() {
        if (closed) {
          return false;
        }
        return serverRequested > 0;
      }

      // Must be thread-safe for shutdownNow()
      @Override
      public void cancel(Status reason) {
        Status serverStatus = stripCause(reason);
        if (!internalCancel(serverStatus, serverStatus)) {
          return;
        }
        serverStream.clientCancelled(reason);
        streamClosed();
      }

      private synchronized boolean internalCancel(
          Status serverListenerStatus, Status serverTracerStatus) {
        if (closed) {
          return false;
        }
        closed = true;

        StreamListener.MessageProducer producer;
        while ((producer = serverReceiveQueue.poll()) != null) {
          InputStream message;
          while ((message = producer.next()) != null) {
            try {
              message.close();
            } catch (Throwable t) {
              log.log(Level.WARNING, "Exception closing stream", t);
            }
          }
        }
        serverStream.statsTraceCtx.streamClosed(serverTracerStatus);
        serverStreamListener.closed(serverListenerStatus);
        return true;
      }

      @Override
      public synchronized void halfClose() {
        if (closed) {
          return;
        }
        if (serverReceiveQueue.isEmpty()) {
          serverStreamListener.halfClosed();
        } else {
          serverNotifyHalfClose = true;
        }
      }

      @Override
      public void setMessageCompression(boolean enable) {}

      @Override
      public void setAuthority(String string) {
        InProcessStream.this.authority = string;
      }

      @Override
      public void start(ClientStreamListener listener) {
        serverStream.setListener(listener);

        synchronized (InProcessTransport.this) {
          statsTraceCtx.clientOutboundHeaders();
          streams.add(InProcessTransport.InProcessStream.this);
          if (GrpcUtil.shouldBeCountedForInUse(callOptions)) {
            inUseState.updateObjectInUse(InProcessTransport.InProcessStream.this, true);
          }
          serverTransportListener.streamCreated(serverStream, method.getFullMethodName(), headers);
        }
      }

      @Override
      public Attributes getAttributes() {
        return Attributes.EMPTY;
      }

      @Override
      public void setCompressor(Compressor compressor) {}

      @Override
      public void setFullStreamDecompression(boolean fullStreamDecompression) {}

      @Override
      public void setDecompressorRegistry(DecompressorRegistry decompressorRegistry) {}

      @Override
      public void setMaxInboundMessageSize(int maxSize) {}

      @Override
      public void setMaxOutboundMessageSize(int maxSize) {}

      @Override
      public void setDeadline(Deadline deadline) {
        headers.discardAll(TIMEOUT_KEY);
        long effectiveTimeout = max(0, deadline.timeRemaining(TimeUnit.NANOSECONDS));
        headers.put(TIMEOUT_KEY, effectiveTimeout);
      }
    }
  }

  /**
   * Returns a new status with the same code and description, but stripped of any other information
   * (i.e. cause).
   *
   * <p>This is, so that the InProcess transport behaves in the same way as the other transports,
   * when exchanging statuses between client and server and vice versa.
   */
  private static Status stripCause(Status status) {
    if (status == null) {
      return null;
    }
    return Status
        .fromCodeValue(status.getCode().value())
        .withDescription(status.getDescription());
  }

  private static class SingleMessageProducer implements StreamListener.MessageProducer {
    private InputStream message;

    private SingleMessageProducer(InputStream message) {
      this.message = message;
    }

    @Nullable
    @Override
    public InputStream next() {
      InputStream messageToReturn = message;
      message = null;
      return messageToReturn;
    }
  }
}
