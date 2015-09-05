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

package io.grpc.internal;

import static io.grpc.internal.GrpcUtil.AUTHORITY_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.TIMEOUT_KEY;
import static io.grpc.internal.GrpcUtil.USER_AGENT_KEY;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.ListenableFuture;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;

import java.io.InputStream;
import java.util.LinkedList;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * Implementation of {@link ClientCall}.
 */
final class ClientCallImpl<ReqT, RespT> extends ClientCall<ReqT, RespT> {
  private final MethodDescriptor<ReqT, RespT> method;
  private final SerializingExecutor callExecutor;
  private final boolean unaryRequest;
  private final CallOptions callOptions;
  private ClientStream stream;
  private volatile ScheduledFuture<?> deadlineCancellationFuture;
  private boolean cancelCalled;
  private boolean halfCloseCalled;
  private final ClientTransportProvider clientTransportProvider;
  private String userAgent;
  private ScheduledExecutorService deadlineCancellationExecutor;
  private DecompressorRegistry decompressorRegistry = DecompressorRegistry.getDefaultInstance();

  ClientCallImpl(MethodDescriptor<ReqT, RespT> method, SerializingExecutor executor,
      CallOptions callOptions, ClientTransportProvider clientTransportProvider,
      ScheduledExecutorService deadlineCancellationExecutor) {
    this.method = method;
    this.callExecutor = executor;
    this.unaryRequest = method.getType() == MethodType.UNARY
        || method.getType() == MethodType.SERVER_STREAMING;
    this.callOptions = callOptions;
    this.clientTransportProvider = clientTransportProvider;
    this.deadlineCancellationExecutor = deadlineCancellationExecutor;
  }

  /**
   * Provider of {@link ClientTransport}s.
   */
  interface ClientTransportProvider {
    /**
     * @return a future for client transport. If no more transports can be created, e.g., channel is
     *         shut down, the future's value will be {@code null}.
     */
    ListenableFuture<ClientTransport> get(CallOptions callOptions);
  }


  ClientCallImpl<ReqT, RespT> setUserAgent(String userAgent) {
    this.userAgent = userAgent;
    return this;
  }

  ClientCallImpl<ReqT, RespT> setDecompressorRegistry(DecompressorRegistry decompressorRegistry) {
    this.decompressorRegistry = decompressorRegistry;
    return this;
  }

  @Override
  public void start(Listener<RespT> observer, Metadata headers) {
    Preconditions.checkState(stream == null, "Already started");

    // Hack to propagate authority.  This should be properly pass to the transport.newStream
    // somehow.
    headers.removeAll(AUTHORITY_KEY);
    if (callOptions.getAuthority() != null) {
      headers.put(AUTHORITY_KEY, callOptions.getAuthority());
    }

    // Fill out the User-Agent header.
    headers.removeAll(USER_AGENT_KEY);
    if (userAgent != null) {
      headers.put(USER_AGENT_KEY, userAgent);
    }

    headers.removeAll(MESSAGE_ENCODING_KEY);
    Compressor compressor = callOptions.getCompressor();
    if (compressor != null && compressor != Codec.Identity.NONE) {
      headers.put(MESSAGE_ENCODING_KEY, compressor.getMessageEncoding());
    }

    headers.removeAll(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY);
    for (String encoding : decompressorRegistry.getAdvertisedMessageEncodings()) {
      headers.put(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY, encoding);
    }

    ClientStreamListener listener = new ClientStreamListenerImpl(observer);
    ListenableFuture<ClientTransport> transportFuture = clientTransportProvider.get(callOptions);
    if (transportFuture.isDone()) {
      // Try to skip DelayedStream when possible to avoid the overhead of a volatile read in the
      // fast path. If that fails, stream will stay null and DelayedStream will be created.
      ClientTransport transport;
      try {
        transport = transportFuture.get();
        if (transport != null && updateTimeoutHeader(headers)) {
          try {
            stream = transport.newStream(method, headers, listener);
          } catch (IllegalStateException e) {
            // The transport is already shut down. This may due to a race condition between Channel
            // shutting down idle transports. Retry in DelayedStream.
          }
        }
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
      } catch (Exception e) {
        // Fall through to DelayedStream
      }
    }
    if (stream == null) {
      stream = new DelayedStream(headers, listener);
    }

    stream.setDecompressionRegistry(decompressorRegistry);
    if (compressor != null) {
      stream.setCompressor(compressor);
    }

    // Start the deadline timer after stream creation because it will close the stream
    Long timeoutMicros = getRemainingTimeoutMicros();
    if (timeoutMicros != null) {
      deadlineCancellationFuture = startDeadlineTimer(timeoutMicros);
    }
  }

  /**
   * Based on the deadline, calculate and set the timeout to the given headers.
   *
   * @return {@code false} if deadline already exceeded
   */
  private boolean updateTimeoutHeader(Metadata headers) {
    // Fill out timeout on the headers
    headers.removeAll(TIMEOUT_KEY);
    // Convert the deadline to timeout. Timeout is more favorable than deadline on the wire
    // because timeout tolerates the clock difference between machines.
    Long timeoutMicros = getRemainingTimeoutMicros();
    if (timeoutMicros != null) {
      if (timeoutMicros <= 0) {
        return false;
      }
      headers.put(TIMEOUT_KEY, timeoutMicros);
    }
    return true;
  }

  /**
   * Return the remaining amout of microseconds before the deadline is reached.
   *
   * <p>{@code null} if deadline is not set. Negative value if already expired.
   */
  @Nullable
  private Long getRemainingTimeoutMicros() {
    Long deadlineNanoTime = callOptions.getDeadlineNanoTime();
    if (deadlineNanoTime == null) {
      return null;
    }
    return TimeUnit.NANOSECONDS.toMicros(deadlineNanoTime - System.nanoTime());
  }

  @Override
  public void request(int numMessages) {
    Preconditions.checkState(stream != null, "Not started");
    stream.request(numMessages);
  }

  @Override
  public void cancel() {
    if (cancelCalled) {
      return;
    }
    cancelCalled = true;
    // Cancel is called in exception handling cases, so it may be the case that the
    // stream was never successfully created.
    if (stream != null) {
      stream.cancel(Status.CANCELLED);
    }
  }

  @Override
  public void halfClose() {
    Preconditions.checkState(stream != null, "Not started");
    Preconditions.checkState(!cancelCalled, "call was cancelled");
    Preconditions.checkState(!halfCloseCalled, "call already half-closed");
    halfCloseCalled = true;
    stream.halfClose();
  }

  @Override
  public void sendMessage(ReqT message) {
    Preconditions.checkState(stream != null, "Not started");
    Preconditions.checkState(!cancelCalled, "call was cancelled");
    Preconditions.checkState(!halfCloseCalled, "call was half-closed");
    boolean failed = true;
    try {
      InputStream messageIs = method.streamRequest(message);
      stream.writeMessage(messageIs);
      failed = false;
    } finally {
      // TODO(notcarl): Find out if messageIs needs to be closed.
      if (failed) {
        cancel();
      }
    }
    // For unary requests, we don't flush since we know that halfClose should be coming soon. This
    // allows us to piggy-back the END_STREAM=true on the last message frame without opening the
    // possibility of broken applications forgetting to call halfClose without noticing.
    if (!unaryRequest) {
      stream.flush();
    }
  }

  @Override
  public boolean isReady() {
    return stream.isReady();
  }

  private ScheduledFuture<?> startDeadlineTimer(long timeoutMicros) {
    return deadlineCancellationExecutor.schedule(new Runnable() {
      @Override
      public void run() {
        // DelayedStream.cancel() is safe to call from a thread that is different from where the
        // stream is created.
        stream.cancel(Status.DEADLINE_EXCEEDED);
      }
    }, timeoutMicros, TimeUnit.MICROSECONDS);
  }

  private class ClientStreamListenerImpl implements ClientStreamListener {
    private final Listener<RespT> observer;
    private boolean closed;

    public ClientStreamListenerImpl(Listener<RespT> observer) {
      Preconditions.checkNotNull(observer);
      this.observer = observer;
    }

    @Override
    public void headersRead(final Metadata headers) {
      callExecutor.execute(new Runnable() {
        @Override
        public void run() {
          try {
            if (closed) {
              return;
            }

            observer.onHeaders(headers);
          } catch (Throwable t) {
            cancel();
            throw Throwables.propagate(t);
          }
        }
      });
    }

    @Override
    public void messageRead(final InputStream message) {
      callExecutor.execute(new Runnable() {
        @Override
        public void run() {
          try {
            if (closed) {
              return;
            }

            try {
              observer.onMessage(method.parseResponse(message));
            } finally {
              message.close();
            }
          } catch (Throwable t) {
            cancel();
            throw Throwables.propagate(t);
          }
        }
      });
    }

    @Override
    public void closed(Status status, Metadata trailers) {
      Long timeoutMicros = getRemainingTimeoutMicros();
      if (status.getCode() == Status.Code.CANCELLED && timeoutMicros != null) {
        // When the server's deadline expires, it can only reset the stream with CANCEL and no
        // description. Since our timer may be delayed in firing, we double-check the deadline and
        // turn the failure into the likely more helpful DEADLINE_EXCEEDED status.
        if (timeoutMicros <= 0) {
          status = Status.DEADLINE_EXCEEDED;
          // Replace trailers to prevent mixing sources of status and trailers.
          trailers = new Metadata();
        }
      }
      final Status savedStatus = status;
      final Metadata savedTrailers = trailers;
      callExecutor.execute(new Runnable() {
        @Override
        public void run() {
          closed = true;
          // manually optimize the volatile read
          ScheduledFuture<?> future = deadlineCancellationFuture;
          if (future != null) {
            future.cancel(false);
          }
          observer.onClose(savedStatus, savedTrailers);
        }
      });
    }

    @Override
    public void onReady() {
      callExecutor.execute(new Runnable() {
        @Override
        public void run() {
          observer.onReady();
        }
      });
    }
  }

  /**
   * A stream that queues requests before the transport is available, and delegates to a real stream
   * implementation when the transport is available.
   *
   * <p>{@code ClientStream} itself doesn't require thread-safety. However, the state of {@code
   * DelayedStream} may be internally altered by different threads, thus internal synchronization is
   * necessary.
   */
  private class DelayedStream implements ClientStream {
    final Metadata headers;
    final ClientStreamListener listener;

    // Volatile to be readable without synchronization in the fast path.
    // Writes are also done within synchronized(this).
    volatile ClientStream realStream;

    @GuardedBy("this")
    Compressor compressor;
    // Can be either a Decompressor or a String
    @GuardedBy("this")
    Object decompressor;
    @GuardedBy("this")
    DecompressorRegistry decompressionRegistry;
    @GuardedBy("this")
    final LinkedList<InputStream> pendingMessages = new LinkedList<InputStream>();
    @GuardedBy("this")
    boolean pendingHalfClose;
    @GuardedBy("this")
    int pendingFlowControlRequests;
    @GuardedBy("this")
    boolean pendingFlush;

    /**
     * Get a transport and try to create a stream on it.
     */
    private class StreamCreationTask implements Runnable {
      final ListenableFuture<ClientTransport> transportFuture;

      StreamCreationTask() {
        this.transportFuture = Preconditions.checkNotNull(
            clientTransportProvider.get(callOptions), "transportFuture");
      }

      @Override
      public void run() {
        if (transportFuture.isDone()) {
          ClientTransport transport;
          try {
            transport = transportFuture.get();
          } catch (Exception e) {
            maybeClosePrematurely(Status.fromThrowable(e));
            if (e instanceof InterruptedException) {
              Thread.currentThread().interrupt();
            }
            return;
          }
          if (transport == null) {
            maybeClosePrematurely(Status.UNAVAILABLE.withDescription("Channel is shutdown"));
            return;
          }
          createStream(transport);
        } else {
          transportFuture.addListener(this, callExecutor);
        }
      }
    }

    DelayedStream(Metadata headers, ClientStreamListener listener) {
      this.headers = headers;
      this.listener = listener;
      new StreamCreationTask().run();
    }

    /**
     * Creates a stream on a presumably usable transport.
     */
    private void createStream(ClientTransport transport) {
      synchronized (this) {
        if (realStream == NOOP_CLIENT_STREAM) {
          // Already cancelled
          return;
        }
        Preconditions.checkState(realStream == null, "Stream already created: %s", realStream);
        if (!updateTimeoutHeader(headers)) {
          maybeClosePrematurely(Status.DEADLINE_EXCEEDED);
          return;
        }
        try {
          realStream = transport.newStream(method, headers, listener);
        } catch (IllegalStateException e) {
          // The transport is already shut down. This may due to a race condition between Channel
          // shutting down idle transports. Retry StreamCreationTask with a new transport, but
          // schedule it in an executor to avoid recursion.
          callExecutor.execute(new StreamCreationTask());
          return;
        }
        Preconditions.checkNotNull(realStream, transport.toString() + " returned null stream");
        if (compressor != null) {
          realStream.setCompressor(compressor);
        }
        if (this.decompressionRegistry != null) {
          realStream.setDecompressionRegistry(this.decompressionRegistry);
        }
        for (InputStream message : pendingMessages) {
          realStream.writeMessage(message);
        }
        pendingMessages.clear();
        if (pendingHalfClose) {
          realStream.halfClose();
          pendingHalfClose = false;
        }
        if (pendingFlowControlRequests > 0) {
          realStream.request(pendingFlowControlRequests);
          pendingFlowControlRequests = 0;
        }
        if (pendingFlush) {
          realStream.flush();
          pendingFlush = false;
        }
      }
    }

    private void maybeClosePrematurely(final Status reason) {
      synchronized (this) {
        if (realStream == null) {
          realStream = NOOP_CLIENT_STREAM;
          callExecutor.execute(new Runnable() {
            @Override
            public void run() {
              listener.closed(reason, new Metadata());
            }
          });
        }
      }
    }

    @Override
    public void writeMessage(InputStream message) {
      if (realStream == null) {
        synchronized (this) {
          if (realStream == null) {
            pendingMessages.add(message);
            return;
          }
        }
      }
      realStream.writeMessage(message);
    }

    @Override
    public void flush() {
      if (realStream == null) {
        synchronized (this) {
          if (realStream == null) {
            pendingFlush = true;
            return;
          }
        }
      }
      realStream.flush();
    }

    @Override
    public void cancel(Status reason) {
      maybeClosePrematurely(reason);
      realStream.cancel(reason);
    }

    @Override
    public void halfClose() {
      if (realStream == null) {
        synchronized (this) {
          if (realStream == null) {
            pendingHalfClose = true;
            return;
          }
        }
      }
      realStream.halfClose();
    }

    @Override
    public void request(int numMessages) {
      if (realStream == null) {
        synchronized (this) {
          if (realStream == null) {
            pendingFlowControlRequests += numMessages;
            return;
          }
        }
      }
      realStream.request(numMessages);
    }

    @Override
    public synchronized void setCompressor(Compressor c) {
      compressor = c;
      if (realStream != null) {
        realStream.setCompressor(c);
      }
    }

    @Override
    public synchronized void setDecompressionRegistry(DecompressorRegistry registry) {
      this.decompressionRegistry = registry;
      if (realStream != null) {
        realStream.setDecompressionRegistry(registry);
      }
    }

    @Override
    public boolean isReady() {
      if (realStream == null) {
        synchronized (this) {
          if (realStream == null) {
            return false;
          }
        }
      }
      return realStream.isReady();
    }
  }

  private static final ClientStream NOOP_CLIENT_STREAM = new ClientStream() {
    @Override public void writeMessage(InputStream message) {}

    @Override public void flush() {}

    @Override public void cancel(Status reason) {}

    @Override public void halfClose() {}

    @Override public void request(int numMessages) {}

    @Override public void setCompressor(Compressor c) {}

    /**
     * Always returns {@code false}, since this is only used when the startup of the {@link
     * ClientCall} fails (i.e. the {@link ClientCall} is closed).
     */
    @Override public boolean isReady() {
      return false;
    }

    @Override
    public void setDecompressionRegistry(DecompressorRegistry registry) {}

    @Override
    public String toString() {
      return "NOOP_CLIENT_STREAM";
    }
  };
}

