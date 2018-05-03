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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static com.google.common.util.concurrent.MoreExecutors.directExecutor;
import static io.grpc.Contexts.statusFromCancelled;
import static io.grpc.Status.DEADLINE_EXCEEDED;
import static io.grpc.internal.GrpcUtil.CONTENT_ACCEPT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.CONTENT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;
import static java.lang.Math.max;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.MoreObjects;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.Context.CancellationListener;
import io.grpc.Deadline;
import io.grpc.DecompressorRegistry;
import io.grpc.InternalDecompressorRegistry;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import java.io.InputStream;
import java.nio.charset.Charset;
import java.util.concurrent.CancellationException;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Implementation of {@link ClientCall}.
 */
final class ClientCallImpl<ReqT, RespT> extends ClientCall<ReqT, RespT> {

  private static final Logger log = Logger.getLogger(ClientCallImpl.class.getName());
  private static final byte[] FULL_STREAM_DECOMPRESSION_ENCODINGS
      = "gzip".getBytes(Charset.forName("US-ASCII"));

  private final MethodDescriptor<ReqT, RespT> method;
  private final Executor callExecutor;
  private final CallTracer channelCallsTracer;
  private final Context context;
  private volatile ScheduledFuture<?> deadlineCancellationFuture;
  private final boolean unaryRequest;
  private final CallOptions callOptions;
  private final boolean retryEnabled;
  private ClientStream stream;
  private volatile boolean cancelListenersShouldBeRemoved;
  private boolean cancelCalled;
  private boolean halfCloseCalled;
  private final ClientTransportProvider clientTransportProvider;
  private final CancellationListener cancellationListener = new ContextCancellationListener();
  private final ScheduledExecutorService deadlineCancellationExecutor;
  private boolean fullStreamDecompression;
  private DecompressorRegistry decompressorRegistry = DecompressorRegistry.getDefaultInstance();
  private CompressorRegistry compressorRegistry = CompressorRegistry.getDefaultInstance();

  ClientCallImpl(
      MethodDescriptor<ReqT, RespT> method, Executor executor, CallOptions callOptions,
      ClientTransportProvider clientTransportProvider,
      ScheduledExecutorService deadlineCancellationExecutor,
      CallTracer channelCallsTracer,
      boolean retryEnabled) {
    this.method = method;
    // If we know that the executor is a direct executor, we don't need to wrap it with a
    // SerializingExecutor. This is purely for performance reasons.
    // See https://github.com/grpc/grpc-java/issues/368
    this.callExecutor = executor == directExecutor()
        ? new SerializeReentrantCallsDirectExecutor()
        : new SerializingExecutor(executor);
    this.channelCallsTracer = channelCallsTracer;
    // Propagate the context from the thread which initiated the call to all callbacks.
    this.context = Context.current();
    this.unaryRequest = method.getType() == MethodType.UNARY
        || method.getType() == MethodType.SERVER_STREAMING;
    this.callOptions = callOptions;
    this.clientTransportProvider = clientTransportProvider;
    this.deadlineCancellationExecutor = deadlineCancellationExecutor;
    this.retryEnabled = retryEnabled;
  }

  private final class ContextCancellationListener implements CancellationListener {
    @Override
    public void cancelled(Context context) {
      stream.cancel(statusFromCancelled(context));
    }
  }

  /**
   * Provider of {@link ClientTransport}s.
   */
  // TODO(zdapeng): replace the two APIs with a single API: newStream()
  interface ClientTransportProvider {
    /**
     * Returns a transport for a new call.
     *
     * @param args object containing call arguments.
     */
    ClientTransport get(PickSubchannelArgs args);

    <ReqT> RetriableStream<ReqT> newRetriableStream(
        MethodDescriptor<ReqT, ?> method,
        CallOptions callOptions,
        Metadata headers,
        Context context);

  }

  ClientCallImpl<ReqT, RespT> setFullStreamDecompression(boolean fullStreamDecompression) {
    this.fullStreamDecompression = fullStreamDecompression;
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

  @VisibleForTesting
  static void prepareHeaders(
      Metadata headers,
      DecompressorRegistry decompressorRegistry,
      Compressor compressor,
      boolean fullStreamDecompression) {
    headers.discardAll(MESSAGE_ENCODING_KEY);
    if (compressor != Codec.Identity.NONE) {
      headers.put(MESSAGE_ENCODING_KEY, compressor.getMessageEncoding());
    }

    headers.discardAll(MESSAGE_ACCEPT_ENCODING_KEY);
    byte[] advertisedEncodings =
        InternalDecompressorRegistry.getRawAdvertisedMessageEncodings(decompressorRegistry);
    if (advertisedEncodings.length != 0) {
      headers.put(MESSAGE_ACCEPT_ENCODING_KEY, advertisedEncodings);
    }

    headers.discardAll(CONTENT_ENCODING_KEY);
    headers.discardAll(CONTENT_ACCEPT_ENCODING_KEY);
    if (fullStreamDecompression) {
      headers.put(CONTENT_ACCEPT_ENCODING_KEY, FULL_STREAM_DECOMPRESSION_ENCODINGS);
    }
  }

  @Override
  public void start(final Listener<RespT> observer, Metadata headers) {
    checkState(stream == null, "Already started");
    checkState(!cancelCalled, "call was cancelled");
    checkNotNull(observer, "observer");
    checkNotNull(headers, "headers");

    if (context.isCancelled()) {
      // Context is already cancelled so no need to create a real stream, just notify the observer
      // of cancellation via callback on the executor
      stream = NoopClientStream.INSTANCE;
      class ClosedByContext extends ContextRunnable {
        ClosedByContext() {
          super(context);
        }

        @Override
        public void runInContext() {
          closeObserver(observer, statusFromCancelled(context), new Metadata());
        }
      }

      callExecutor.execute(new ClosedByContext());
      return;
    }
    final String compressorName = callOptions.getCompressor();
    Compressor compressor = null;
    if (compressorName != null) {
      compressor = compressorRegistry.lookupCompressor(compressorName);
      if (compressor == null) {
        stream = NoopClientStream.INSTANCE;
        class ClosedByNotFoundCompressor extends ContextRunnable {
          ClosedByNotFoundCompressor() {
            super(context);
          }

          @Override
          public void runInContext() {
            closeObserver(
                observer,
                Status.INTERNAL.withDescription(
                    String.format("Unable to find compressor by name %s", compressorName)),
                new Metadata());
          }
        }

        callExecutor.execute(new ClosedByNotFoundCompressor());
        return;
      }
    } else {
      compressor = Codec.Identity.NONE;
    }
    prepareHeaders(headers, decompressorRegistry, compressor, fullStreamDecompression);

    Deadline effectiveDeadline = effectiveDeadline();
    boolean deadlineExceeded = effectiveDeadline != null && effectiveDeadline.isExpired();
    if (!deadlineExceeded) {
      logIfContextNarrowedTimeout(
          effectiveDeadline, callOptions.getDeadline(), context.getDeadline());
      if (retryEnabled) {
        stream = clientTransportProvider.newRetriableStream(method, callOptions, headers, context);
      } else {
        ClientTransport transport = clientTransportProvider.get(
            new PickSubchannelArgsImpl(method, headers, callOptions));
        Context origContext = context.attach();
        try {
          stream = transport.newStream(method, headers, callOptions);
        } finally {
          context.detach(origContext);
        }
      }
    } else {
      stream = new FailingClientStream(
          DEADLINE_EXCEEDED.withDescription("deadline exceeded: " + effectiveDeadline));
    }

    if (callOptions.getAuthority() != null) {
      stream.setAuthority(callOptions.getAuthority());
    }
    if (callOptions.getMaxInboundMessageSize() != null) {
      stream.setMaxInboundMessageSize(callOptions.getMaxInboundMessageSize());
    }
    if (callOptions.getMaxOutboundMessageSize() != null) {
      stream.setMaxOutboundMessageSize(callOptions.getMaxOutboundMessageSize());
    }
    if (effectiveDeadline != null) {
      stream.setDeadline(effectiveDeadline);
    }
    stream.setCompressor(compressor);
    if (fullStreamDecompression) {
      stream.setFullStreamDecompression(fullStreamDecompression);
    }
    stream.setDecompressorRegistry(decompressorRegistry);
    channelCallsTracer.reportCallStarted();
    stream.start(new ClientStreamListenerImpl(observer));

    // Delay any sources of cancellation after start(), because most of the transports are broken if
    // they receive cancel before start. Issue #1343 has more details

    // Propagate later Context cancellation to the remote side.
    context.addListener(cancellationListener, directExecutor());
    if (effectiveDeadline != null
        // If the context has the effective deadline, we don't need to schedule an extra task.
        && context.getDeadline() != effectiveDeadline
        // If the channel has been terminated, we don't need to schedule an extra task.
        && deadlineCancellationExecutor != null) {
      deadlineCancellationFuture = startDeadlineTimer(effectiveDeadline);
    }
    if (cancelListenersShouldBeRemoved) {
      // Race detected! ClientStreamListener.closed may have been called before
      // deadlineCancellationFuture was set / context listener added, thereby preventing the future
      // and listener from being cancelled. Go ahead and cancel again, just to be sure it
      // was cancelled.
      removeContextListenerAndCancelDeadlineFuture();
    }
  }

  private static void logIfContextNarrowedTimeout(
      Deadline effectiveDeadline, @Nullable Deadline outerCallDeadline,
      @Nullable Deadline callDeadline) {
    if (!log.isLoggable(Level.FINE) || effectiveDeadline == null
        || outerCallDeadline != effectiveDeadline) {
      return;
    }

    long effectiveTimeout = max(0, effectiveDeadline.timeRemaining(TimeUnit.NANOSECONDS));
    StringBuilder builder = new StringBuilder(String.format(
        "Call timeout set to '%d' ns, due to context deadline.", effectiveTimeout));
    if (callDeadline == null) {
      builder.append(" Explicit call timeout was not set.");
    } else {
      long callTimeout = callDeadline.timeRemaining(TimeUnit.NANOSECONDS);
      builder.append(String.format(" Explicit call timeout was '%d' ns.", callTimeout));
    }

    log.fine(builder.toString());
  }

  private void removeContextListenerAndCancelDeadlineFuture() {
    context.removeListener(cancellationListener);
    ScheduledFuture<?> f = deadlineCancellationFuture;
    if (f != null) {
      f.cancel(false);
    }
  }

  private class DeadlineTimer implements Runnable {
    private final long remainingNanos;

    DeadlineTimer(long remainingNanos) {
      this.remainingNanos = remainingNanos;
    }

    @Override
    public void run() {
      // DelayedStream.cancel() is safe to call from a thread that is different from where the
      // stream is created.
      stream.cancel(DEADLINE_EXCEEDED.augmentDescription(
          String.format("deadline exceeded after %dns", remainingNanos)));
    }
  }

  private ScheduledFuture<?> startDeadlineTimer(Deadline deadline) {
    long remainingNanos = deadline.timeRemaining(TimeUnit.NANOSECONDS);
    return deadlineCancellationExecutor.schedule(
        new LogExceptionRunnable(
            new DeadlineTimer(remainingNanos)), remainingNanos, TimeUnit.NANOSECONDS);
  }

  @Nullable
  private Deadline effectiveDeadline() {
    // Call options and context are immutable, so we don't need to cache the deadline.
    return min(callOptions.getDeadline(), context.getDeadline());
  }

  @Nullable
  private static Deadline min(@Nullable Deadline deadline0, @Nullable Deadline deadline1) {
    if (deadline0 == null) {
      return deadline1;
    }
    if (deadline1 == null) {
      return deadline0;
    }
    return deadline0.minimum(deadline1);
  }

  @Override
  public void request(int numMessages) {
    checkState(stream != null, "Not started");
    checkArgument(numMessages >= 0, "Number requested must be non-negative");
    stream.request(numMessages);
  }

  @Override
  public void cancel(@Nullable String message, @Nullable Throwable cause) {
    if (message == null && cause == null) {
      cause = new CancellationException("Cancelled without a message or cause");
      log.log(Level.WARNING, "Cancelling without a message or cause is suboptimal", cause);
    }
    if (cancelCalled) {
      return;
    }
    cancelCalled = true;
    try {
      // Cancel is called in exception handling cases, so it may be the case that the
      // stream was never successfully created or start has never been called.
      if (stream != null) {
        Status status = Status.CANCELLED;
        if (message != null) {
          status = status.withDescription(message);
        } else {
          status = status.withDescription("Call cancelled without message");
        }
        if (cause != null) {
          status = status.withCause(cause);
        }
        stream.cancel(status);
      }
    } finally {
      removeContextListenerAndCancelDeadlineFuture();
    }
  }

  @Override
  public void halfClose() {
    checkState(stream != null, "Not started");
    checkState(!cancelCalled, "call was cancelled");
    checkState(!halfCloseCalled, "call already half-closed");
    halfCloseCalled = true;
    stream.halfClose();
  }

  @Override
  public void sendMessage(ReqT message) {
    checkState(stream != null, "Not started");
    checkState(!cancelCalled, "call was cancelled");
    checkState(!halfCloseCalled, "call was half-closed");
    try {
      if (stream instanceof RetriableStream) {
        @SuppressWarnings("unchecked")
        RetriableStream<ReqT> retriableStream = ((RetriableStream<ReqT>) stream);
        retriableStream.sendMessage(message);
      } else {
        stream.writeMessage(method.streamRequest(message));
      }
    } catch (RuntimeException e) {
      stream.cancel(Status.CANCELLED.withCause(e).withDescription("Failed to stream message"));
      return;
    } catch (Error e) {
      stream.cancel(Status.CANCELLED.withDescription("Client sendMessage() failed with Error"));
      throw e;
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

  @Override
  public Attributes getAttributes() {
    if (stream != null) {
      return stream.getAttributes();
    }
    return Attributes.EMPTY;
  }

  private void closeObserver(Listener<RespT> observer, Status status, Metadata trailers) {
    observer.onClose(status, trailers);
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("method", method).toString();
  }

  private class ClientStreamListenerImpl implements ClientStreamListener {
    private final Listener<RespT> observer;
    private boolean closed;

    public ClientStreamListenerImpl(Listener<RespT> observer) {
      this.observer = checkNotNull(observer, "observer");
    }

    @Override
    public void headersRead(final Metadata headers) {
      class HeadersRead extends ContextRunnable {
        HeadersRead() {
          super(context);
        }

        @Override
        public final void runInContext() {
          try {
            if (closed) {
              return;
            }
            observer.onHeaders(headers);
          } catch (Throwable t) {
            Status status =
                Status.CANCELLED.withCause(t).withDescription("Failed to read headers");
            stream.cancel(status);
            close(status, new Metadata());
          }
        }
      }

      callExecutor.execute(new HeadersRead());
    }

    @Override
    public void messagesAvailable(final MessageProducer producer) {
      class MessagesAvailable extends ContextRunnable {
        MessagesAvailable() {
          super(context);
        }

        @Override
        public final void runInContext() {
          if (closed) {
            GrpcUtil.closeQuietly(producer);
            return;
          }

          InputStream message;
          try {
            while ((message = producer.next()) != null) {
              try {
                observer.onMessage(method.parseResponse(message));
              } catch (Throwable t) {
                GrpcUtil.closeQuietly(message);
                throw t;
              }
              message.close();
            }
          } catch (Throwable t) {
            GrpcUtil.closeQuietly(producer);
            Status status =
                Status.CANCELLED.withCause(t).withDescription("Failed to read message.");
            stream.cancel(status);
            close(status, new Metadata());
          }
        }
      }

      callExecutor.execute(new MessagesAvailable());
    }

    /**
     * Must be called from application thread.
     */
    private void close(Status status, Metadata trailers) {
      closed = true;
      cancelListenersShouldBeRemoved = true;
      try {
        closeObserver(observer, status, trailers);
      } finally {
        removeContextListenerAndCancelDeadlineFuture();
        channelCallsTracer.reportCallEnded(status.isOk());
      }
    }

    @Override
    public void closed(Status status, Metadata trailers) {
      closed(status, RpcProgress.PROCESSED, trailers);
    }

    @Override
    public void closed(Status status, RpcProgress rpcProgress, Metadata trailers) {
      Deadline deadline = effectiveDeadline();
      if (status.getCode() == Status.Code.CANCELLED && deadline != null) {
        // When the server's deadline expires, it can only reset the stream with CANCEL and no
        // description. Since our timer may be delayed in firing, we double-check the deadline and
        // turn the failure into the likely more helpful DEADLINE_EXCEEDED status.
        if (deadline.isExpired()) {
          status = DEADLINE_EXCEEDED;
          // Replace trailers to prevent mixing sources of status and trailers.
          trailers = new Metadata();
        }
      }
      final Status savedStatus = status;
      final Metadata savedTrailers = trailers;
      class StreamClosed extends ContextRunnable {
        StreamClosed() {
          super(context);
        }

        @Override
        public final void runInContext() {
          if (closed) {
            // We intentionally don't keep the status or metadata from the server.
            return;
          }
          close(savedStatus, savedTrailers);
        }
      }

      callExecutor.execute(new StreamClosed());
    }

    @Override
    public void onReady() {
      class StreamOnReady extends ContextRunnable {
        StreamOnReady() {
          super(context);
        }

        @Override
        public final void runInContext() {
          try {
            observer.onReady();
          } catch (Throwable t) {
            Status status =
                Status.CANCELLED.withCause(t).withDescription("Failed to call onReady.");
            stream.cancel(status);
            close(status, new Metadata());
          }
        }
      }

      callExecutor.execute(new StreamOnReady());
    }
  }
}
