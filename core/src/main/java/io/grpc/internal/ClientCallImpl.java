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

import static com.google.common.base.Preconditions.checkState;
import static com.google.common.collect.Iterables.addAll;
import static com.google.common.util.concurrent.MoreExecutors.directExecutor;
import static io.grpc.internal.GrpcUtil.ACCEPT_ENCODING_JOINER;
import static io.grpc.internal.GrpcUtil.ACCEPT_ENCODING_SPLITER;
import static io.grpc.internal.GrpcUtil.AUTHORITY_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.TIMEOUT_KEY;
import static io.grpc.internal.GrpcUtil.USER_AGENT_KEY;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;

import java.io.InputStream;
import java.util.Collections;
import java.util.Set;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

import javax.annotation.Nullable;

/**
 * Implementation of {@link ClientCall}.
 */
final class ClientCallImpl<ReqT, RespT> extends ClientCall<ReqT, RespT>
    implements Context.CancellationListener {
  private final MethodDescriptor<ReqT, RespT> method;
  private final Executor callExecutor;
  private final Context context;
  private final boolean unaryRequest;
  private final CallOptions callOptions;
  private ClientStream stream;
  private volatile ScheduledFuture<?> deadlineCancellationFuture;
  private boolean cancelCalled;
  private boolean halfCloseCalled;
  private final ClientTransportProvider clientTransportProvider;
  private String userAgent;
  private ScheduledExecutorService deadlineCancellationExecutor;
  private Set<String> knownMessageEncodingRegistry;
  private DecompressorRegistry decompressorRegistry = DecompressorRegistry.getDefaultInstance();
  private CompressorRegistry compressorRegistry = CompressorRegistry.getDefaultInstance();

  ClientCallImpl(MethodDescriptor<ReqT, RespT> method, Executor executor,
      CallOptions callOptions, ClientTransportProvider clientTransportProvider,
      ScheduledExecutorService deadlineCancellationExecutor) {
    this.method = method;
    // If we know that the executor is a direct executor, we don't need to wrap it with a
    // SerializingExecutor. This is purely for performance reasons.
    // See https://github.com/grpc/grpc-java/issues/368
    this.callExecutor = executor == MoreExecutors.directExecutor()
        ? new SerializeReentrantCallsDirectExecutor()
        : new SerializingExecutor(executor);
    // Propagate the context from the thread which initiated the call to all callbacks.
    this.context = Context.current();
    this.unaryRequest = method.getType() == MethodType.UNARY
        || method.getType() == MethodType.SERVER_STREAMING;
    this.callOptions = callOptions;
    this.clientTransportProvider = clientTransportProvider;
    this.deadlineCancellationExecutor = deadlineCancellationExecutor;
  }

  @Override
  public void cancelled(Context context) {
    cancel();
  }

  /**
   * Provider of {@link ClientTransport}s.
   */
  interface ClientTransportProvider {
    /**
     * @return a future for client transport. If no more transports can be created, e.g., channel is
     *         shut down, the future's value will be {@code null}. If the call is cancelled, it will
     *         also cancel the future.
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

  ClientCallImpl<ReqT, RespT> setCompressorRegistry(CompressorRegistry compressorRegistry) {
    this.compressorRegistry = compressorRegistry;
    return this;
  }

  /**
   * Sets encodings known to be supported by the server.  This set MUST be thread safe, and MAY be
   * modified by any code as it learns about new supported encodings.
   */
  ClientCallImpl<ReqT, RespT> setKnownMessageEncodingRegistry(Set<String> knownMessageEncodings) {
    this.knownMessageEncodingRegistry = knownMessageEncodings;
    return this;
  }

  @VisibleForTesting
  static void prepareHeaders(Metadata headers, CallOptions callOptions, String userAgent,
      Set<String> knownMessageEncodings, DecompressorRegistry decompressorRegistry,
      CompressorRegistry compressorRegistry) {
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
    for (String messageEncoding : knownMessageEncodings) {
      Compressor compressor = compressorRegistry.lookupCompressor(messageEncoding);
      if (compressor != null && compressor != Codec.Identity.NONE) {
        headers.put(MESSAGE_ENCODING_KEY, compressor.getMessageEncoding());
        break;
      }
    }

    headers.removeAll(MESSAGE_ACCEPT_ENCODING_KEY);
    if (!decompressorRegistry.getAdvertisedMessageEncodings().isEmpty()) {
      String acceptEncoding =
          ACCEPT_ENCODING_JOINER.join(decompressorRegistry.getAdvertisedMessageEncodings());
      headers.put(MESSAGE_ACCEPT_ENCODING_KEY, acceptEncoding);
    }
  }

  @Override
  public void start(final Listener<RespT> observer, Metadata headers) {
    Preconditions.checkState(stream == null, "Already started");

    if (context.isCancelled()) {
      // Context is already cancelled so no need to create a real stream, just notify the observer
      // of cancellation via callback on the executor
      stream = NoopClientStream.INSTANCE;
      callExecutor.execute(new ContextRunnable(context) {
        @Override
        public void runInContext() {
          observer.onClose(Status.CANCELLED.withCause(context.cause()), new Metadata());
        }
      });
      return;
    }
    prepareHeaders(headers, callOptions, userAgent,
        knownMessageEncodingRegistry, decompressorRegistry, compressorRegistry);

    ListenableFuture<ClientTransport> transportFuture = clientTransportProvider.get(callOptions);
    ClientStreamListener listener = new ClientStreamListenerImpl(observer, transportFuture);

    if (transportFuture.isDone()) {
      // Try to skip DelayedStream when possible to avoid the overhead of a volatile read in the
      // fast path. If that fails, stream will stay null and DelayedStream will be created.
      ClientTransport transport;
      try {
        transport = transportFuture.get();
        if (transport != null && updateTimeoutHeader(callOptions.getDeadlineNanoTime(), headers)) {
          stream = transport.newStream(method, headers, listener);
        }
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
      } catch (Exception e) {
        // Fall through to DelayedStream
      }
    }
    if (stream == null) {
      DelayedStream delayed;
      stream = delayed = new DelayedStream(listener);
      Futures.addCallback(transportFuture,
          new StreamCreationTask(delayed, headers, method, callOptions, listener),
          transportFuture.isDone() ? directExecutor() : callExecutor);
    }

    stream.setDecompressionRegistry(decompressorRegistry);
    stream.setCompressionRegistry(compressorRegistry);
    if (headers.containsKey(MESSAGE_ENCODING_KEY)) {
      stream.pickCompressor(Collections.singleton(headers.get(MESSAGE_ENCODING_KEY)));
      // TODO(carl-mastrangelo): move this to ClientCall.
      stream.setMessageCompression(true);
    }

    // Start the deadline timer after stream creation because it will close the stream
    Long timeoutNanos = getRemainingTimeoutNanos(callOptions.getDeadlineNanoTime());
    if (timeoutNanos != null) {
      deadlineCancellationFuture = startDeadlineTimer(timeoutNanos);
    }
    // Propagate later Context cancellation to the remote side.
    this.context.addListener(this, MoreExecutors.directExecutor());
  }

  /**
   * Based on the deadline, calculate and set the timeout to the given headers.
   *
   * @return {@code false} if deadline already exceeded
   */
  static boolean updateTimeoutHeader(@Nullable Long deadlineNanoTime, Metadata headers) {
    // Fill out timeout on the headers
    // TODO(carl-mastrangelo): Find out if this should always remove the timeout,
    // even when returning false.
    headers.removeAll(TIMEOUT_KEY);
    // Convert the deadline to timeout. Timeout is more favorable than deadline on the wire
    // because timeout tolerates the clock difference between machines.
    Long timeoutNanos = getRemainingTimeoutNanos(deadlineNanoTime);
    if (timeoutNanos != null) {
      if (timeoutNanos <= 0) {
        return false;
      }
      headers.put(TIMEOUT_KEY, timeoutNanos);
    }
    return true;
  }

  /**
   * Return the remaining amount of nanoseconds before the deadline is reached.
   *
   * <p>{@code null} if deadline is not set. Negative value if already expired.
   */
  @Nullable
  private static Long getRemainingTimeoutNanos(@Nullable Long deadlineNanoTime) {
    if (deadlineNanoTime == null) {
      return null;
    }
    return deadlineNanoTime - System.nanoTime();
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
    try {
      // Cancel is called in exception handling cases, so it may be the case that the
      // stream was never successfully created.
      if (stream != null) {
        stream.cancel(Status.CANCELLED);
      }
    } finally {
      context.removeListener(ClientCallImpl.this);
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
  public void setMessageCompression(boolean enabled) {
    checkState(stream != null, "Not started");
    stream.setMessageCompression(enabled);
  }

  @Override
  public boolean isReady() {
    return stream.isReady();
  }

  private ScheduledFuture<?> startDeadlineTimer(long timeoutNanos) {
    return deadlineCancellationExecutor.schedule(new Runnable() {
      @Override
      public void run() {
        stream.cancel(Status.DEADLINE_EXCEEDED);
      }
    }, timeoutNanos, TimeUnit.NANOSECONDS);
  }

  private class ClientStreamListenerImpl implements ClientStreamListener {
    private final Listener<RespT> observer;
    private final ListenableFuture<ClientTransport> transportFuture;
    private boolean closed;

    public ClientStreamListenerImpl(Listener<RespT> observer,
        ListenableFuture<ClientTransport> transportFuture) {
      this.observer = Preconditions.checkNotNull(observer, "observer");
      this.transportFuture = Preconditions.checkNotNull(transportFuture, "transportFuture");
    }

    @Override
    public void headersRead(final Metadata headers) {
      if (headers.containsKey(MESSAGE_ACCEPT_ENCODING_KEY)) {
        // TODO(carl-mastrangelo): after the first time we contact the server, it almost certainly
        // won't change.  It might be possible to recover performance by not adding to the known
        // encodings if it isn't empty.
        String serverAcceptEncodings = headers.get(MESSAGE_ACCEPT_ENCODING_KEY);
        addAll(knownMessageEncodingRegistry, ACCEPT_ENCODING_SPLITER.split(serverAcceptEncodings));
      }
      callExecutor.execute(new ContextRunnable(context) {
        @Override
        public final void runInContext() {
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
      callExecutor.execute(new ContextRunnable(context) {
        @Override
        public final void runInContext() {
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
      Long timeoutNanos = getRemainingTimeoutNanos(callOptions.getDeadlineNanoTime());
      transportFuture.cancel(false);
      if (status.getCode() == Status.Code.CANCELLED && timeoutNanos != null) {
        // When the server's deadline expires, it can only reset the stream with CANCEL and no
        // description. Since our timer may be delayed in firing, we double-check the deadline and
        // turn the failure into the likely more helpful DEADLINE_EXCEEDED status.
        if (timeoutNanos <= 0) {
          status = Status.DEADLINE_EXCEEDED;
          // Replace trailers to prevent mixing sources of status and trailers.
          trailers = new Metadata();
        }
      }
      ClientTransport transport = null;
      try {
        transport = transportFuture.get();
      } catch (Exception e) {
        // Ignore the exception, and keep transport as null.
      }
      final Status savedStatus = (transport == null
          ? status : status.augmentDescription("transport=" + transport));
      final Metadata savedTrailers = trailers;
      callExecutor.execute(new ContextRunnable(context) {
        @Override
        public final void runInContext() {
          try {
            closed = true;
            // manually optimize the volatile read
            ScheduledFuture<?> future = deadlineCancellationFuture;
            if (future != null) {
              future.cancel(false);
            }
            observer.onClose(savedStatus, savedTrailers);
          } finally {
            context.removeListener(ClientCallImpl.this);
          }
        }
      });
    }

    @Override
    public void onReady() {
      callExecutor.execute(new ContextRunnable(context) {
        @Override
        public final void runInContext() {
          observer.onReady();
        }
      });
    }
  }

  /**
   * Wakes up delayed stream when the transport is ready or failed.
   */
  @VisibleForTesting
  static final class StreamCreationTask implements FutureCallback<ClientTransport> {
    private final DelayedStream stream;
    private final MethodDescriptor<?, ?> method;
    private final Metadata headers;
    private final ClientStreamListener listener;
    private final CallOptions callOptions;

    StreamCreationTask(DelayedStream stream, Metadata headers, MethodDescriptor<?, ?> method,
        CallOptions callOptions, ClientStreamListener listener) {
      this.stream = stream;
      this.headers = headers;
      this.method = method;
      this.callOptions = callOptions;
      this.listener = listener;
    }

    @Override
    public void onSuccess(ClientTransport transport) {
      if (transport == null) {
        stream.maybeClosePrematurely(Status.UNAVAILABLE.withDescription("Channel is shutdown"));
        return;
      }
      if (!updateTimeoutHeader(callOptions.getDeadlineNanoTime(), headers)) {
        stream.maybeClosePrematurely(Status.DEADLINE_EXCEEDED);
        return;
      }
      synchronized (stream) {
        // Must not create a real stream with 'listener' if 'stream' is cancelled, as
        // listener.closed() would be called multiple times and from different threads in a
        // non-synchronized manner.
        if (stream.cancelledPrematurely()) {
          return;
        }
        stream.setStream(transport.newStream(method, headers, listener));
      }
    }

    @Override
    public void onFailure(Throwable t) {
      stream.maybeClosePrematurely(Status.fromThrowable(t));
    }
  }
}
