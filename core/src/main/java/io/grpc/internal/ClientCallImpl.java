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
import static io.grpc.internal.GrpcUtil.ACCEPT_ENCODING_JOINER;
import static io.grpc.internal.GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.TIMEOUT_KEY;
import static io.grpc.internal.GrpcUtil.USER_AGENT_KEY;
import static java.util.concurrent.TimeUnit.NANOSECONDS;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.Deadline;
import io.grpc.Decompressor;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;

import java.io.InputStream;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;

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
  private volatile boolean deadlineCancellationFutureShouldBeCancelled;
  private boolean cancelCalled;
  private boolean halfCloseCalled;
  private final ClientTransportProvider clientTransportProvider;
  private String userAgent;
  private ScheduledExecutorService deadlineCancellationExecutor;
  private Compressor compressor;
  private DecompressorRegistry decompressorRegistry = DecompressorRegistry.getDefaultInstance();
  private CompressorRegistry compressorRegistry = CompressorRegistry.getDefaultInstance();

  ClientCallImpl(MethodDescriptor<ReqT, RespT> method, Executor executor,
      CallOptions callOptions, ClientTransportProvider clientTransportProvider,
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
    stream.cancel(Status.CANCELLED.withCause(context.cancellationCause()));
  }

  /**
   * Provider of {@link ClientTransport}s.
   */
  interface ClientTransportProvider {
    /**
     * Returns a transport for a new call.
     */
    ClientTransport get(CallOptions callOptions);
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

  @VisibleForTesting
  static void prepareHeaders(Metadata headers, CallOptions callOptions, String userAgent,
      DecompressorRegistry decompressorRegistry, Compressor compressor) {
    // Fill out the User-Agent header.
    headers.removeAll(USER_AGENT_KEY);
    if (userAgent != null) {
      headers.put(USER_AGENT_KEY, userAgent);
    }

    headers.removeAll(MESSAGE_ENCODING_KEY);
    if (compressor != Codec.Identity.NONE) {
      headers.put(MESSAGE_ENCODING_KEY, compressor.getMessageEncoding());
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
    checkState(stream == null, "Already started");
    checkNotNull(observer, "observer");
    checkNotNull(headers, "headers");

    if (context.isCancelled()) {
      // Context is already cancelled so no need to create a real stream, just notify the observer
      // of cancellation via callback on the executor
      stream = NoopClientStream.INSTANCE;
      callExecutor.execute(new ContextRunnable(context) {
        @Override
        public void runInContext() {
          observer.onClose(Status.CANCELLED.withCause(context.cancellationCause()), new Metadata());
        }
      });
      return;
    }
    final String compressorName = callOptions.getCompressor();
    if (compressorName != null) {
      compressor = compressorRegistry.lookupCompressor(compressorName);
      if (compressor == null) {
        stream = NoopClientStream.INSTANCE;
        callExecutor.execute(new ContextRunnable(context) {
          @Override
          public void runInContext() {
            observer.onClose(
                Status.INTERNAL.withDescription(
                    String.format("Unable to find compressor by name %s", compressorName)),
                new Metadata());
          }
        });
        return;
      }
    } else {
      compressor = Codec.Identity.NONE;
    }

    prepareHeaders(headers, callOptions, userAgent, decompressorRegistry, compressor);

    if (updateTimeoutHeader(callOptions.getDeadline(), headers)) {
      ClientTransport transport = clientTransportProvider.get(callOptions);
      stream = transport.newStream(method, headers);
    } else {
      stream = new FailingClientStream(Status.DEADLINE_EXCEEDED);
    }

    if (callOptions.getAuthority() != null) {
      stream.setAuthority(callOptions.getAuthority());
    }
    stream.setCompressor(compressor);

    stream.start(new ClientStreamListenerImpl(observer));
    if (compressor != Codec.Identity.NONE) {
      stream.setMessageCompression(true);
    }
    // Delay any sources of cancellation after start(), because most of the transports are broken if
    // they receive cancel before start. Issue #1343 has more details

    // Start the deadline timer after stream creation because it will close the stream
    if (callOptions.getDeadline() != null) {
      long timeoutNanos = callOptions.getDeadline().timeRemaining(NANOSECONDS);
      deadlineCancellationFuture = startDeadlineTimer(timeoutNanos);
      if (deadlineCancellationFutureShouldBeCancelled) {
        // Race detected! ClientStreamListener.closed may have been called before
        // deadlineCancellationFuture was set, thereby preventing the future from being cancelled.
        // Go ahead and cancel again, just to be sure it was cancelled.
        deadlineCancellationFuture.cancel(false);
      }
    }
    // Propagate later Context cancellation to the remote side.
    this.context.addListener(this, directExecutor());
  }

  /**
   * Based on the deadline, calculate and set the timeout to the given headers.
   *
   * @return {@code false} if deadline already exceeded
   */
  static boolean updateTimeoutHeader(@Nullable Deadline deadline, Metadata headers) {
    // Fill out timeout on the headers
    // TODO(carl-mastrangelo): Find out if this should always remove the timeout,
    // even when returning false.
    headers.removeAll(TIMEOUT_KEY);

    if (deadline != null) {
      // Convert the deadline to timeout. Timeout is more favorable than deadline on the wire
      // because timeout tolerates the clock difference between machines.
      long timeoutNanos = deadline.timeRemaining(NANOSECONDS);
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
    checkArgument(numMessages >= 0, "Number requested must be non-negative");
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

  private ScheduledFuture<?> startDeadlineTimer(long timeoutNanos) {
    return deadlineCancellationExecutor.schedule(new Runnable() {
      @Override
      public void run() {
        stream.cancel(Status.DEADLINE_EXCEEDED);
      }
    }, timeoutNanos, NANOSECONDS);
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

      callExecutor.execute(new ContextRunnable(context) {
        @Override
        public final void runInContext() {
          try {
            if (closed) {
              return;
            }

            observer.onHeaders(headers);
          } catch (Throwable t) {
            stream.cancel(Status.CANCELLED.withCause(t).withDescription("Failed to read headers"));
            return;
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
            stream.cancel(Status.CANCELLED.withCause(t).withDescription("Failed to read message."));
            return;
          }
        }
      });
    }

    @Override
    public void closed(Status status, Metadata trailers) {
      if (status.getCode() == Status.Code.CANCELLED && callOptions.getDeadline() != null) {
        // When the server's deadline expires, it can only reset the stream with CANCEL and no
        // description. Since our timer may be delayed in firing, we double-check the deadline and
        // turn the failure into the likely more helpful DEADLINE_EXCEEDED status.
        long timeoutNanos = callOptions.getDeadline().timeRemaining(NANOSECONDS);
        if (timeoutNanos <= 0) {
          status = Status.DEADLINE_EXCEEDED;
          // Replace trailers to prevent mixing sources of status and trailers.
          trailers = new Metadata();
        }
      }
      final Status savedStatus = status;
      final Metadata savedTrailers = trailers;
      callExecutor.execute(new ContextRunnable(context) {
        @Override
        public final void runInContext() {
          try {
            closed = true;
            deadlineCancellationFutureShouldBeCancelled = true;
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
}
