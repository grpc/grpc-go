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

package io.grpc.stub;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.MoreObjects;
import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractFuture;
import com.google.common.util.concurrent.ListenableFuture;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.StatusRuntimeException;
import java.util.Iterator;
import java.util.NoSuchElementException;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Executor;
import java.util.concurrent.Future;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Utility functions for processing different call idioms. We have one-to-one correspondence
 * between utilities in this class and the potential signatures in a generated stub class so
 * that the runtime can vary behavior without requiring regeneration of the stub.
 */
public final class ClientCalls {

  private static final Logger logger = Logger.getLogger(ClientCalls.class.getName());

  // Prevent instantiation
  private ClientCalls() {}

  /**
   * Executes a unary call with a response {@link StreamObserver}.  The {@code call} should not be
   * already started.  After calling this method, {@code call} should no longer be used.
   */
  public static <ReqT, RespT> void asyncUnaryCall(
      ClientCall<ReqT, RespT> call, ReqT req, StreamObserver<RespT> responseObserver) {
    asyncUnaryRequestCall(call, req, responseObserver, false);
  }

  /**
   * Executes a server-streaming call with a response {@link StreamObserver}.  The {@code call}
   * should not be already started.  After calling this method, {@code call} should no longer be
   * used.
   */
  public static <ReqT, RespT> void asyncServerStreamingCall(
      ClientCall<ReqT, RespT> call, ReqT req, StreamObserver<RespT> responseObserver) {
    asyncUnaryRequestCall(call, req, responseObserver, true);
  }

  /**
   * Executes a client-streaming call returning a {@link StreamObserver} for the request messages.
   * The {@code call} should not be already started.  After calling this method, {@code call}
   * should no longer be used.
   *
   * @return request stream observer.
   */
  public static <ReqT, RespT> StreamObserver<ReqT> asyncClientStreamingCall(
      ClientCall<ReqT, RespT> call,
      StreamObserver<RespT> responseObserver) {
    return asyncStreamingRequestCall(call, responseObserver, false);
  }

  /**
   * Executes a bidirectional-streaming call.  The {@code call} should not be already started.
   * After calling this method, {@code call} should no longer be used.
   *
   * @return request stream observer.
   */
  public static <ReqT, RespT> StreamObserver<ReqT> asyncBidiStreamingCall(
      ClientCall<ReqT, RespT> call, StreamObserver<RespT> responseObserver) {
    return asyncStreamingRequestCall(call, responseObserver, true);
  }

  /**
   * Executes a unary call and blocks on the response.  The {@code call} should not be already
   * started.  After calling this method, {@code call} should no longer be used.
   *
   * @return the single response message.
   */
  public static <ReqT, RespT> RespT blockingUnaryCall(ClientCall<ReqT, RespT> call, ReqT req) {
    try {
      return getUnchecked(futureUnaryCall(call, req));
    } catch (RuntimeException e) {
      throw cancelThrow(call, e);
    } catch (Error e) {
      throw cancelThrow(call, e);
    }
  }

  /**
   * Executes a unary call and blocks on the response.  The {@code call} should not be already
   * started.  After calling this method, {@code call} should no longer be used.
   *
   * @return the single response message.
   */
  public static <ReqT, RespT> RespT blockingUnaryCall(
      Channel channel, MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, ReqT req) {
    ThreadlessExecutor executor = new ThreadlessExecutor();
    ClientCall<ReqT, RespT> call = channel.newCall(method, callOptions.withExecutor(executor));
    try {
      ListenableFuture<RespT> responseFuture = futureUnaryCall(call, req);
      while (!responseFuture.isDone()) {
        try {
          executor.waitAndDrain();
        } catch (InterruptedException e) {
          Thread.currentThread().interrupt();
          throw Status.CANCELLED
              .withDescription("Call was interrupted")
              .withCause(e)
              .asRuntimeException();
        }
      }
      return getUnchecked(responseFuture);
    } catch (RuntimeException e) {
      throw cancelThrow(call, e);
    } catch (Error e) {
      throw cancelThrow(call, e);
    }
  }

  /**
   * Executes a server-streaming call returning a blocking {@link Iterator} over the
   * response stream.  The {@code call} should not be already started.  After calling this method,
   * {@code call} should no longer be used.
   *
   * @return an iterator over the response stream.
   */
  // TODO(louiscryan): Not clear if we want to use this idiom for 'simple' stubs.
  public static <ReqT, RespT> Iterator<RespT> blockingServerStreamingCall(
      ClientCall<ReqT, RespT> call, ReqT req) {
    BlockingResponseStream<RespT> result = new BlockingResponseStream<RespT>(call);
    asyncUnaryRequestCall(call, req, result.listener(), true);
    return result;
  }

  /**
   * Executes a server-streaming call returning a blocking {@link Iterator} over the
   * response stream.  The {@code call} should not be already started.  After calling this method,
   * {@code call} should no longer be used.
   *
   * @return an iterator over the response stream.
   */
  // TODO(louiscryan): Not clear if we want to use this idiom for 'simple' stubs.
  public static <ReqT, RespT> Iterator<RespT> blockingServerStreamingCall(
      Channel channel, MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, ReqT req) {
    ThreadlessExecutor executor = new ThreadlessExecutor();
    ClientCall<ReqT, RespT> call = channel.newCall(method, callOptions.withExecutor(executor));
    BlockingResponseStream<RespT> result = new BlockingResponseStream<RespT>(call, executor);
    asyncUnaryRequestCall(call, req, result.listener(), true);
    return result;
  }

  /**
   * Executes a unary call and returns a {@link ListenableFuture} to the response.  The
   * {@code call} should not be already started.  After calling this method, {@code call} should no
   * longer be used.
   *
   * @return a future for the single response message.
   */
  public static <ReqT, RespT> ListenableFuture<RespT> futureUnaryCall(
      ClientCall<ReqT, RespT> call, ReqT req) {
    GrpcFuture<RespT> responseFuture = new GrpcFuture<RespT>(call);
    asyncUnaryRequestCall(call, req, new UnaryStreamToFuture<RespT>(responseFuture), false);
    return responseFuture;
  }

  /**
   * Returns the result of calling {@link Future#get()} interruptibly on a task known not to throw a
   * checked exception.
   *
   * <p>If interrupted, the interrupt is restored before throwing an exception..
   *
   * @throws java.util.concurrent.CancellationException
   *     if {@code get} throws a {@code CancellationException}.
   * @throws io.grpc.StatusRuntimeException if {@code get} throws an {@link ExecutionException}
   *     or an {@link InterruptedException}.
   */
  private static <V> V getUnchecked(Future<V> future) {
    try {
      return future.get();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw Status.CANCELLED
          .withDescription("Call was interrupted")
          .withCause(e)
          .asRuntimeException();
    } catch (ExecutionException e) {
      throw toStatusRuntimeException(e.getCause());
    }
  }

  /**
   * Wraps the given {@link Throwable} in a {@link StatusRuntimeException}. If it contains an
   * embedded {@link StatusException} or {@link StatusRuntimeException}, the returned exception will
   * contain the embedded trailers and status, with the given exception as the cause. Otherwise, an
   * exception will be generated from an {@link Status#UNKNOWN} status.
   */
  private static StatusRuntimeException toStatusRuntimeException(Throwable t) {
    Throwable cause = checkNotNull(t, "t");
    while (cause != null) {
      // If we have an embedded status, use it and replace the cause
      if (cause instanceof StatusException) {
        StatusException se = (StatusException) cause;
        return new StatusRuntimeException(se.getStatus(), se.getTrailers());
      } else if (cause instanceof StatusRuntimeException) {
        StatusRuntimeException se = (StatusRuntimeException) cause;
        return new StatusRuntimeException(se.getStatus(), se.getTrailers());
      }
      cause = cause.getCause();
    }
    return Status.UNKNOWN.withDescription("unexpected exception").withCause(t)
        .asRuntimeException();
  }

  /**
   * Cancels a call, and throws the exception.
   *
   * @param t must be a RuntimeException or Error
   */
  private static RuntimeException cancelThrow(ClientCall<?, ?> call, Throwable t) {
    try {
      call.cancel(null, t);
    } catch (Throwable e) {
      assert e instanceof RuntimeException || e instanceof Error;
      logger.log(Level.SEVERE, "RuntimeException encountered while closing call", e);
    }
    if (t instanceof RuntimeException) {
      throw (RuntimeException) t;
    } else if (t instanceof Error) {
      throw (Error) t;
    }
    // should be impossible
    throw new AssertionError(t);
  }

  private static <ReqT, RespT> void asyncUnaryRequestCall(
      ClientCall<ReqT, RespT> call, ReqT req, StreamObserver<RespT> responseObserver,
      boolean streamingResponse) {
    asyncUnaryRequestCall(
        call,
        req,
        new StreamObserverToCallListenerAdapter<ReqT, RespT>(
            responseObserver,
            new CallToStreamObserverAdapter<ReqT>(call),
            streamingResponse),
        streamingResponse);
  }

  private static <ReqT, RespT> void asyncUnaryRequestCall(
      ClientCall<ReqT, RespT> call,
      ReqT req,
      ClientCall.Listener<RespT> responseListener,
      boolean streamingResponse) {
    startCall(call, responseListener, streamingResponse);
    try {
      call.sendMessage(req);
      call.halfClose();
    } catch (RuntimeException e) {
      throw cancelThrow(call, e);
    } catch (Error e) {
      throw cancelThrow(call, e);
    }
  }

  private static <ReqT, RespT> StreamObserver<ReqT> asyncStreamingRequestCall(
      ClientCall<ReqT, RespT> call,
      StreamObserver<RespT> responseObserver,
      boolean streamingResponse) {
    CallToStreamObserverAdapter<ReqT> adapter = new CallToStreamObserverAdapter<ReqT>(call);
    startCall(
        call,
        new StreamObserverToCallListenerAdapter<ReqT, RespT>(
            responseObserver, adapter, streamingResponse),
        streamingResponse);
    return adapter;
  }

  private static <ReqT, RespT> void startCall(
      ClientCall<ReqT, RespT> call,
      ClientCall.Listener<RespT> responseListener,
      boolean streamingResponse) {
    call.start(responseListener, new Metadata());
    if (streamingResponse) {
      call.request(1);
    } else {
      // Initially ask for two responses from flow-control so that if a misbehaving server sends
      // more than one responses, we can catch it and fail it in the listener.
      call.request(2);
    }
  }

  private static final class CallToStreamObserverAdapter<T> extends ClientCallStreamObserver<T> {
    private boolean frozen;
    private final ClientCall<T, ?> call;
    private Runnable onReadyHandler;
    private boolean autoFlowControlEnabled = true;

    // Non private to avoid synthetic class
    CallToStreamObserverAdapter(ClientCall<T, ?> call) {
      this.call = call;
    }

    private void freeze() {
      this.frozen = true;
    }

    @Override
    public void onNext(T value) {
      call.sendMessage(value);
    }

    @Override
    public void onError(Throwable t) {
      call.cancel("Cancelled by client with StreamObserver.onError()", t);
    }

    @Override
    public void onCompleted() {
      call.halfClose();
    }

    @Override
    public boolean isReady() {
      return call.isReady();
    }

    @Override
    public void setOnReadyHandler(Runnable onReadyHandler) {
      if (frozen) {
        throw new IllegalStateException("Cannot alter onReadyHandler after call started");
      }
      this.onReadyHandler = onReadyHandler;
    }

    @Override
    public void disableAutoInboundFlowControl() {
      if (frozen) {
        throw new IllegalStateException("Cannot disable auto flow control call started");
      }
      autoFlowControlEnabled = false;
    }

    @Override
    public void request(int count) {
      call.request(count);
    }

    @Override
    public void setMessageCompression(boolean enable) {
      call.setMessageCompression(enable);
    }

    @Override
    public void cancel(@Nullable String message, @Nullable Throwable cause) {
      call.cancel(message, cause);
    }
  }

  private static final class StreamObserverToCallListenerAdapter<ReqT, RespT>
      extends ClientCall.Listener<RespT> {
    private final StreamObserver<RespT> observer;
    private final CallToStreamObserverAdapter<ReqT> adapter;
    private final boolean streamingResponse;
    private boolean firstResponseReceived;

    // Non private to avoid synthetic class
    StreamObserverToCallListenerAdapter(
        StreamObserver<RespT> observer,
        CallToStreamObserverAdapter<ReqT> adapter,
        boolean streamingResponse) {
      this.observer = observer;
      this.streamingResponse = streamingResponse;
      this.adapter = adapter;
      if (observer instanceof ClientResponseObserver) {
        @SuppressWarnings("unchecked")
        ClientResponseObserver<ReqT, RespT> clientResponseObserver =
            (ClientResponseObserver<ReqT, RespT>) observer;
        clientResponseObserver.beforeStart(adapter);
      }
      adapter.freeze();
    }

    @Override
    public void onHeaders(Metadata headers) {
    }

    @Override
    public void onMessage(RespT message) {
      if (firstResponseReceived && !streamingResponse) {
        throw Status.INTERNAL
            .withDescription("More than one responses received for unary or client-streaming call")
            .asRuntimeException();
      }
      firstResponseReceived = true;
      observer.onNext(message);

      if (streamingResponse && adapter.autoFlowControlEnabled) {
        // Request delivery of the next inbound message.
        adapter.request(1);
      }
    }

    @Override
    public void onClose(Status status, Metadata trailers) {
      if (status.isOk()) {
        observer.onCompleted();
      } else {
        observer.onError(status.asRuntimeException(trailers));
      }
    }

    @Override
    public void onReady() {
      if (adapter.onReadyHandler != null) {
        adapter.onReadyHandler.run();
      }
    }
  }

  /**
   * Completes a {@link GrpcFuture} using {@link StreamObserver} events.
   */
  private static final class UnaryStreamToFuture<RespT> extends ClientCall.Listener<RespT> {
    private final GrpcFuture<RespT> responseFuture;
    private RespT value;

    // Non private to avoid synthetic class
    UnaryStreamToFuture(GrpcFuture<RespT> responseFuture) {
      this.responseFuture = responseFuture;
    }

    @Override
    public void onHeaders(Metadata headers) {
    }

    @Override
    public void onMessage(RespT value) {
      if (this.value != null) {
        throw Status.INTERNAL.withDescription("More than one value received for unary call")
            .asRuntimeException();
      }
      this.value = value;
    }

    @Override
    public void onClose(Status status, Metadata trailers) {
      if (status.isOk()) {
        if (value == null) {
          // No value received so mark the future as an error
          responseFuture.setException(
              Status.INTERNAL.withDescription("No value received for unary call")
                  .asRuntimeException(trailers));
        }
        responseFuture.set(value);
      } else {
        responseFuture.setException(status.asRuntimeException(trailers));
      }
    }
  }

  private static final class GrpcFuture<RespT> extends AbstractFuture<RespT> {
    private final ClientCall<?, RespT> call;

    // Non private to avoid synthetic class
    GrpcFuture(ClientCall<?, RespT> call) {
      this.call = call;
    }

    @Override
    protected void interruptTask() {
      call.cancel("GrpcFuture was cancelled", null);
    }

    @Override
    protected boolean set(@Nullable RespT resp) {
      return super.set(resp);
    }

    @Override
    protected boolean setException(Throwable throwable) {
      return super.setException(throwable);
    }

    @SuppressWarnings("MissingOverride") // Add @Override once Java 6 support is dropped
    protected String pendingToString() {
      return MoreObjects.toStringHelper(this).add("clientCall", call).toString();
    }
  }

  /**
   * Convert events on a {@link io.grpc.ClientCall.Listener} into a blocking {@link Iterator}.
   *
   * <p>The class is not thread-safe, but it does permit {@link ClientCall.Listener} calls in a
   * separate thread from {@link Iterator} calls.
   */
  // TODO(ejona86): determine how to allow ClientCall.cancel() in case of application error.
  private static final class BlockingResponseStream<T> implements Iterator<T> {
    // Due to flow control, only needs to hold up to 2 items: 1 for value, 1 for close.
    private final BlockingQueue<Object> buffer = new ArrayBlockingQueue<Object>(2);
    private final ClientCall.Listener<T> listener = new QueuingListener();
    private final ClientCall<?, T> call;
    /** May be null. */
    private final ThreadlessExecutor threadless;
    // Only accessed when iterating.
    private Object last;

    // Non private to avoid synthetic class
    BlockingResponseStream(ClientCall<?, T> call) {
      this(call, null);
    }

    // Non private to avoid synthetic class
    BlockingResponseStream(ClientCall<?, T> call, ThreadlessExecutor threadless) {
      this.call = call;
      this.threadless = threadless;
    }

    ClientCall.Listener<T> listener() {
      return listener;
    }

    private Object waitForNext() throws InterruptedException {
      if (threadless == null) {
        return buffer.take();
      } else {
        Object next = buffer.poll();
        while (next == null) {
          threadless.waitAndDrain();
          next = buffer.poll();
        }
        return next;
      }
    }

    @Override
    public boolean hasNext() {
      if (last == null) {
        try {
          // Will block here indefinitely waiting for content. RPC timeouts defend against permanent
          // hangs here as the call will become closed.
          last = waitForNext();
        } catch (InterruptedException ie) {
          Thread.currentThread().interrupt();
          throw Status.CANCELLED.withDescription("interrupted").withCause(ie).asRuntimeException();
        }
      }
      if (last instanceof StatusRuntimeException) {
        // Rethrow the exception with a new stacktrace.
        StatusRuntimeException e = (StatusRuntimeException) last;
        throw e.getStatus().asRuntimeException(e.getTrailers());
      }
      return last != this;
    }

    @Override
    public T next() {
      if (!hasNext()) {
        throw new NoSuchElementException();
      }
      try {
        call.request(1);
        @SuppressWarnings("unchecked")
        T tmp = (T) last;
        return tmp;
      } finally {
        last = null;
      }
    }

    @Override
    public void remove() {
      throw new UnsupportedOperationException();
    }

    private final class QueuingListener extends ClientCall.Listener<T> {
      // Non private to avoid synthetic class
      QueuingListener() {}

      private boolean done = false;

      @Override
      public void onHeaders(Metadata headers) {
      }

      @Override
      public void onMessage(T value) {
        Preconditions.checkState(!done, "ClientCall already closed");
        buffer.add(value);
      }

      @Override
      public void onClose(Status status, Metadata trailers) {
        Preconditions.checkState(!done, "ClientCall already closed");
        if (status.isOk()) {
          buffer.add(BlockingResponseStream.this);
        } else {
          buffer.add(status.asRuntimeException(trailers));
        }
        done = true;
      }
    }
  }

  private static final class ThreadlessExecutor implements Executor {
    private static final Logger log = Logger.getLogger(ThreadlessExecutor.class.getName());

    private final BlockingQueue<Runnable> queue = new LinkedBlockingQueue<Runnable>();

    // Non private to avoid synthetic class
    ThreadlessExecutor() {}

    /**
     * Waits until there is a Runnable, then executes it and all queued Runnables after it.
     */
    public void waitAndDrain() throws InterruptedException {
      Runnable runnable = queue.take();
      while (runnable != null) {
        try {
          runnable.run();
        } catch (Throwable t) {
          log.log(Level.WARNING, "Runnable threw exception", t);
        }
        runnable = queue.poll();
      }
    }

    @Override
    public void execute(Runnable runnable) {
      queue.add(runnable);
    }
  }
}
