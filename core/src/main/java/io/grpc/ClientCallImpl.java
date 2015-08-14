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

package io.grpc;

import static io.grpc.ChannelImpl.TIMEOUT_KEY;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;

import io.grpc.MessageEncoding.Compressor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.internal.ClientStream;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.ClientTransport;
import io.grpc.internal.HttpUtil;
import io.grpc.internal.SerializingExecutor;

import java.io.InputStream;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

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
  private ClientTransportProvider clientTransportProvider;
  private String userAgent;
  private ScheduledExecutorService deadlineCancellationExecutor;

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
     * @return a client transport, or null if no more transports can be created.
     */
    ClientTransport get();
  }

  ClientCallImpl<ReqT, RespT> setUserAgent(String userAgent) {
    this.userAgent = userAgent;
    return this;
  }

  @Override
  public void start(Listener<RespT> observer, Metadata.Headers headers) {
    Preconditions.checkState(stream == null, "Already started");
    Long deadlineNanoTime = callOptions.getDeadlineNanoTime();
    ClientStreamListener listener = new ClientStreamListenerImpl(observer, deadlineNanoTime);
    ClientTransport transport;
    try {
      transport = clientTransportProvider.get();
    } catch (RuntimeException ex) {
      closeCallPrematurely(listener, Status.fromThrowable(ex));
      return;
    }
    if (transport == null) {
      closeCallPrematurely(listener, Status.UNAVAILABLE.withDescription("Channel is shutdown"));
      return;
    }

    // Fill out timeout on the headers
    headers.removeAll(TIMEOUT_KEY);
    // Convert the deadline to timeout. Timeout is more favorable than deadline on the wire
    // because timeout tolerates the clock difference between machines.
    long timeoutMicros = 0;
    if (deadlineNanoTime != null) {
      timeoutMicros = TimeUnit.NANOSECONDS.toMicros(deadlineNanoTime - System.nanoTime());
      if (timeoutMicros <= 0) {
        closeCallPrematurely(listener, Status.DEADLINE_EXCEEDED);
        return;
      }
      headers.put(TIMEOUT_KEY, timeoutMicros);
    }

    // Fill out the User-Agent header.
    headers.removeAll(HttpUtil.USER_AGENT_KEY);
    if (userAgent != null) {
      headers.put(HttpUtil.USER_AGENT_KEY, userAgent);
    }

    headers.removeAll(ChannelImpl.MESSAGE_ENCODING_KEY);
    Compressor compressor = callOptions.getCompressor();
    if (compressor != null && compressor != MessageEncoding.NONE) {
      headers.put(ChannelImpl.MESSAGE_ENCODING_KEY, compressor.getMessageEncoding());
    }

    try {
      stream = transport.newStream(method, headers, listener);
    } catch (IllegalStateException ex) {
      // We can race with the transport and end up trying to use a terminated transport.
      // TODO(ejona86): Improve the API to remove the possibility of the race.
      closeCallPrematurely(listener, Status.fromThrowable(ex));
    }

    if (stream != null && compressor != null) {
      stream.setCompressor(compressor);
    }

    // Start the deadline timer after stream creation because it will close the stream
    if (deadlineNanoTime != null) {
      deadlineCancellationFuture = startDeadlineTimer(timeoutMicros);
    }
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

  /**
   * Close the call before the stream is created.
   */
  private void closeCallPrematurely(ClientStreamListener listener, Status status) {
    Preconditions.checkState(stream == null, "Stream already created");
    stream = new NoopClientStream();
    listener.closed(status, new Metadata());
  }

  private ScheduledFuture<?> startDeadlineTimer(long timeoutMicros) {
    return deadlineCancellationExecutor.schedule(new Runnable() {
      @Override
      public void run() {
        stream.cancel(Status.DEADLINE_EXCEEDED);
      }
    }, timeoutMicros, TimeUnit.MICROSECONDS);
  }

  private class ClientStreamListenerImpl implements ClientStreamListener {
    private final Listener<RespT> observer;
    private final Long deadlineNanoTime;
    private boolean closed;

    public ClientStreamListenerImpl(Listener<RespT> observer, Long deadlineNanoTime) {
      Preconditions.checkNotNull(observer);
      this.observer = observer;
      this.deadlineNanoTime = deadlineNanoTime;
    }

    @Override
    public void headersRead(final Metadata.Headers headers) {
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
      if (status.getCode() == Status.Code.CANCELLED && deadlineNanoTime != null) {
        // When the server's deadline expires, it can only reset the stream with CANCEL and no
        // description. Since our timer may be delayed in firing, we double-check the deadline and
        // turn the failure into the likely more helpful DEADLINE_EXCEEDED status. This is always
        // safe, but we avoid wasting resources getting the nanoTime() when unnecessary.
        if (deadlineNanoTime <= System.nanoTime()) {
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

  static class NoopClientStream implements ClientStream {
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
  }
}

