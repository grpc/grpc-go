/*
 * Copyright 2015, Google Inc. All rights reserved.
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

package io.grpc;

import com.google.common.base.Preconditions;
import java.util.concurrent.TimeoutException;

/**
 * Utility methods for working with {@link Context}s in GRPC.
 */
public final class Contexts {

  private Contexts() {
  }

  /**
   * Make the provided {@link Context} {@link Context#current()} for the creation of a listener
   * to a received call and for all events received by that listener.
   *
   * <p>This utility is expected to be used by {@link ServerInterceptor} implementations that need
   * to augment the {@link Context} in which the application does work when receiving events from
   * the client.
   *
   * @param context to make {@link Context#current()}.
   * @param call used to send responses to client.
   * @param headers received from client.
   * @param next handler used to create the listener to be wrapped.
   * @return listener that will receive events in the scope of the provided context.
   */
  public static <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
        Context context,
        ServerCall<ReqT, RespT> call,
        Metadata headers,
        ServerCallHandler<ReqT, RespT> next) {
    Context previous = context.attach();
    try {
      return new ContextualizedServerCallListener<ReqT>(
          next.startCall(call, headers),
          context);
    } finally {
      context.detach(previous);
    }
  }

  /**
   * Implementation of {@link io.grpc.ForwardingServerCallListener} that attaches a context before
   * dispatching calls to the delegate and detaches them after the call completes.
   */
  private static class ContextualizedServerCallListener<ReqT> extends
      ForwardingServerCallListener.SimpleForwardingServerCallListener<ReqT> {
    private final Context context;

    public ContextualizedServerCallListener(ServerCall.Listener<ReqT> delegate, Context context) {
      super(delegate);
      this.context = context;
    }

    @Override
    public void onMessage(ReqT message) {
      Context previous = context.attach();
      try {
        super.onMessage(message);
      } finally {
        context.detach(previous);
      }
    }

    @Override
    public void onHalfClose() {
      Context previous = context.attach();
      try {
        super.onHalfClose();
      } finally {
        context.detach(previous);
      }
    }

    @Override
    public void onCancel() {
      Context previous = context.attach();
      try {
        super.onCancel();
      } finally {
        context.detach(previous);
      }
    }

    @Override
    public void onComplete() {
      Context previous = context.attach();
      try {
        super.onComplete();
      } finally {
        context.detach(previous);
      }
    }

    @Override
    public void onReady() {
      Context previous = context.attach();
      try {
        super.onReady();
      } finally {
        context.detach(previous);
      }
    }
  }

  /**
   * Returns the {@link Status} of a cancelled context or {@code null} if the context
   * is not cancelled.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1975")
  public static Status statusFromCancelled(Context context) {
    Preconditions.checkNotNull(context, "context must not be null");
    if (!context.isCancelled()) {
      return null;
    }

    Throwable cancellationCause = context.cancellationCause();
    if (cancellationCause == null) {
      return Status.CANCELLED;
    }
    if (cancellationCause instanceof TimeoutException) {
      return Status.DEADLINE_EXCEEDED
          .withDescription(cancellationCause.getMessage())
          .withCause(cancellationCause);
    }
    Status status = Status.fromThrowable(cancellationCause);
    if (Status.Code.UNKNOWN.equals(status.getCode())
        && status.getCause() == cancellationCause) {
      // If fromThrowable could not determine a status, then
      // just return CANCELLED.
      return Status.CANCELLED.withCause(cancellationCause);
    }
    return status.withCause(cancellationCause);
  }
}
