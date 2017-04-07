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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static com.google.common.util.concurrent.MoreExecutors.directExecutor;
import static io.grpc.Contexts.statusFromCancelled;
import static io.grpc.Status.DEADLINE_EXCEEDED;
import static io.grpc.internal.GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.TIMEOUT_KEY;
import static java.lang.Math.max;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.Deadline;
import io.grpc.Decompressor;
import io.grpc.DecompressorRegistry;
import io.grpc.InternalDecompressorRegistry;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import java.io.InputStream;
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
final class ClientCallImpl<ReqT, RespT> extends ClientCall<ReqT, RespT>
    implements Context.CancellationListener {

  private static final Logger log = Logger.getLogger(ClientCallImpl.class.getName());

  private final MethodDescriptor<ReqT, RespT> method;
  private final Executor callExecutor;
  private final Context context;
  private volatile ScheduledFuture<?> deadlineCancellationFuture;
  private final boolean unaryRequest;
  private final CallOptions callOptions;
  private ClientStream stream;
  private volatile boolean cancelListenersShouldBeRemoved;
  private boolean cancelCalled;
  private boolean halfCloseCalled;
  private final ClientTransportProvider clientTransportProvider;
  private ScheduledExecutorService deadlineCancellationExecutor;
  private DecompressorRegistry decompressorRegistry = DecompressorRegistry.getDefaultInstance();
  private CompressorRegistry compressorRegistry = CompressorRegistry.getDefaultInstance();

  ClientCallImpl(
      MethodDescriptor<ReqT, RespT> method, Executor executor, CallOptions callOptions,
      ClientTransportProvider clientTransportProvider,
      ScheduledExecutorService deadlineCancellationExecutor) {
    this.method = method;
    // If we know that the executor is a direct executor, we don't need to wrap it with a
    // SerializingExecutor. This is purely for performance reasons.
    // See https://github.com/grpc/grpc-java/issues/368
    this.callExecutor = executor == directExecutor()
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
    stream.cancel(statusFromCancelled(context));
  }

  /**
   * Provider of {@link ClientTransport}s.
   */
  interface ClientTransportProvider {
    /**
     * Returns a transport for a new call.
     *
     * @param args object containing call arguments.
     */
    ClientTransport get(PickSubchannelArgs args);
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
      Metadata headers, DecompressorRegistry decompressorRegistry, Compressor compressor) {
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
  }

  @Override
  public void start(final Listener<RespT> observer, Metadata headers) {
    checkState(stream == null, "Already started");
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

    prepareHeaders(headers, decompressorRegistry, compressor);

    Deadline effectiveDeadline = effectiveDeadline();
    boolean deadlineExceeded = effectiveDeadline != null && effectiveDeadline.isExpired();
    if (!deadlineExceeded) {
      updateTimeoutHeaders(effectiveDeadline, callOptions.getDeadline(),
          context.getDeadline(), headers);
      ClientTransport transport = clientTransportProvider.get(
          new PickSubchannelArgsImpl(method, headers, callOptions));
      Context origContext = context.attach();
      try {
        stream = transport.newStream(method, headers, callOptions);
      } finally {
        context.detach(origContext);
      }
    } else {
      stream = new FailingClientStream(DEADLINE_EXCEEDED);
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
    stream.setCompressor(compressor);
    stream.start(new ClientStreamListenerImpl(observer));

    // Delay any sources of cancellation after start(), because most of the transports are broken if
    // they receive cancel before start. Issue #1343 has more details

    // Propagate later Context cancellation to the remote side.
    context.addListener(this, directExecutor());
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

  /**
   * Based on the deadline, calculate and set the timeout to the given headers.
   */
  private static void updateTimeoutHeaders(@Nullable Deadline effectiveDeadline,
      @Nullable Deadline callDeadline, @Nullable Deadline outerCallDeadline, Metadata headers) {
    headers.discardAll(TIMEOUT_KEY);

    if (effectiveDeadline == null) {
      return;
    }

    long effectiveTimeout = max(0, effectiveDeadline.timeRemaining(TimeUnit.NANOSECONDS));
    headers.put(TIMEOUT_KEY, effectiveTimeout);

    logIfContextNarrowedTimeout(effectiveTimeout, effectiveDeadline, outerCallDeadline,
        callDeadline);
  }

  private static void logIfContextNarrowedTimeout(long effectiveTimeout,
      Deadline effectiveDeadline, @Nullable Deadline outerCallDeadline,
      @Nullable Deadline callDeadline) {
    if (!log.isLoggable(Level.INFO) || outerCallDeadline != effectiveDeadline) {
      return;
    }

    StringBuilder builder = new StringBuilder();
    builder.append(String.format("Call timeout set to '%d' ns, due to context deadline.",
        effectiveTimeout));
    if (callDeadline == null) {
      builder.append(" Explicit call timeout was not set.");
    } else {
      long callTimeout = callDeadline.timeRemaining(TimeUnit.NANOSECONDS);
      builder.append(String.format(" Explicit call timeout was '%d' ns.", callTimeout));
    }

    log.info(builder.toString());
  }

  private void removeContextListenerAndCancelDeadlineFuture() {
    context.removeListener(this);
    ScheduledFuture<?> f = deadlineCancellationFuture;
    if (f != null) {
      f.cancel(false);
    }
  }

  private class DeadlineTimer implements Runnable {
    @Override
    public void run() {
      // DelayedStream.cancel() is safe to call from a thread that is different from where the
      // stream is created.
      stream.cancel(DEADLINE_EXCEEDED);
    }
  }

  private ScheduledFuture<?> startDeadlineTimer(Deadline deadline) {
    return deadlineCancellationExecutor.schedule(
        new LogExceptionRunnable(new DeadlineTimer()), deadline.timeRemaining(TimeUnit.NANOSECONDS),
        TimeUnit.NANOSECONDS);
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
    Preconditions.checkState(stream != null, "Not started");
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
    try {
      // TODO(notcarl): Find out if messageIs needs to be closed.
      InputStream messageIs = method.streamRequest(message);
      stream.writeMessage(messageIs);
    } catch (Throwable e) {
      stream.cancel(Status.CANCELLED.withCause(e).withDescription("Failed to stream message"));
      return;
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

  private class ClientStreamListenerImpl implements ClientStreamListener {
    private final Listener<RespT> observer;
    private boolean closed;

    public ClientStreamListenerImpl(Listener<RespT> observer) {
      this.observer = Preconditions.checkNotNull(observer, "observer");
    }

    @Override
    public void headersRead(final Metadata headers) {
      Decompressor decompressor = Codec.Identity.NONE;
      if (headers.containsKey(MESSAGE_ENCODING_KEY)) {
        String encoding = headers.get(MESSAGE_ENCODING_KEY);
        decompressor = decompressorRegistry.lookupDecompressor(encoding);
        if (decompressor == null) {
          stream.cancel(Status.INTERNAL.withDescription(
              String.format("Can't find decompressor for %s", encoding)));
          return;
        }
      }
      stream.setDecompressor(decompressor);

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
    public void messageRead(final InputStream message) {
      class MessageRead extends ContextRunnable {
        MessageRead() {
          super(context);
        }

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
            Status status =
                Status.CANCELLED.withCause(t).withDescription("Failed to read message.");
            stream.cancel(status);
            close(status, new Metadata());
          }
        }
      }

      callExecutor.execute(new MessageRead());
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
      }
    }

    @Override
    public void closed(Status status, Metadata trailers) {
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
