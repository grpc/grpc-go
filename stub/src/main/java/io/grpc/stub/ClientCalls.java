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

package io.grpc.stub;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.AbstractFuture;
import com.google.common.util.concurrent.ListenableFuture;

import io.grpc.ClientCall;
import io.grpc.Metadata;
import io.grpc.Status;

import java.util.Iterator;
import java.util.NoSuchElementException;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Future;

import javax.annotation.Nullable;

/**
 * Utility functions for processing different call idioms. We have one-to-one correspondence
 * between utilities in this class and the potential signatures in a generated stub class so
 * that the runtime can vary behavior without requiring regeneration of the stub.
 */
public class ClientCalls {
  // Prevent instantiation
  private ClientCalls() {}

  /**
   * Executes a unary call with a response {@link StreamObserver}.
   */
  public static <ReqT, RespT> void asyncUnaryCall(
      ClientCall<ReqT, RespT> call,
      ReqT param,
      StreamObserver<RespT> observer) {
    asyncUnaryRequestCall(call, param, observer, false);
  }

  /**
   * Executes a server-streaming call with a response {@link StreamObserver}.
   */
  public static <ReqT, RespT> void asyncServerStreamingCall(
      ClientCall<ReqT, RespT> call,
      ReqT param,
      StreamObserver<RespT> responseObserver) {
    asyncUnaryRequestCall(call, param, responseObserver, true);
  }

  /**
   * Executes a client-streaming call returning a {@link StreamObserver} for the request messages.
   * @return request stream observer.
   */
  public static <ReqT, RespT> StreamObserver<ReqT> asyncClientStreamingCall(
      ClientCall<ReqT, RespT> call,
      StreamObserver<RespT> responseObserver) {
    return asyncStreamingRequestCall(call, responseObserver, false);
  }

  /**
   * Executes a bidi-streaming call.
   * @return request stream observer.
   */
  public static <ReqT, RespT> StreamObserver<ReqT> asyncBidiStreamingCall(
      ClientCall<ReqT, RespT> call, StreamObserver<RespT> responseObserver) {
    return asyncStreamingRequestCall(call, responseObserver, true);
  }

  /**
   * Executes a unary call and blocks on the response.
   * @return the single response message.
   */
  public static <ReqT, RespT> RespT blockingUnaryCall(ClientCall<ReqT, RespT> call, ReqT param) {
    try {
      return getUnchecked(futureUnaryCall(call, param));
    } catch (Throwable t) {
      call.cancel();
      throw Throwables.propagate(t);
    }
  }

  /**
   * Executes a server-streaming call returning a blocking {@link Iterator} over the
   * response stream.
   * @return an iterator over the response stream.
   */
  // TODO(louiscryan): Not clear if we want to use this idiom for 'simple' stubs.
  public static <ReqT, RespT> Iterator<RespT> blockingServerStreamingCall(
      ClientCall<ReqT, RespT> call, ReqT param) {
    BlockingResponseStream<RespT> result = new BlockingResponseStream<RespT>(call);
    asyncUnaryRequestCall(call, param, result.listener(), true);
    return result;
  }

  /**
   * Executes a unary call and returns a {@link ListenableFuture} to the response.
   *
   * @return a future for the single response message.
   */
  public static <ReqT, RespT> ListenableFuture<RespT> futureUnaryCall(
      ClientCall<ReqT, RespT> call,
      ReqT param) {
    GrpcFuture<RespT> responseFuture = new GrpcFuture<RespT>(call);
    asyncUnaryRequestCall(call, param, new UnaryStreamToFuture<RespT>(responseFuture), false);
    return responseFuture;
  }

  /**
   * Returns the result of calling {@link Future#get()} interruptably on a task known not to throw a
   * checked exception.
   *
   * <p>If interrupted, the interrupt is restored before throwing a {@code RuntimeException}.
   *
   * @throws RuntimeException if {@code get} is interrupted
   * @throws CancellationException if {@code get} throws a {@code CancellationException}
   * @throws StatusRuntimeException if {@code get} throws an {@code ExecutionException}
   */
  private static <V> V getUnchecked(Future<V> future) {
    try {
      return future.get();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new RuntimeException(e);
    } catch (ExecutionException e) {
      throw Status.fromThrowable(e).asRuntimeException();
    }
  }

  private static <ReqT, RespT> void asyncUnaryRequestCall(
      ClientCall<ReqT, RespT> call, ReqT param, StreamObserver<RespT> responseObserver,
      boolean streamingResponse) {
    asyncUnaryRequestCall(call, param,
        new StreamObserverToCallListenerAdapter<RespT>(call, responseObserver, streamingResponse),
        streamingResponse);
  }

  private static <ReqT, RespT> void asyncUnaryRequestCall(
      ClientCall<ReqT, RespT> call,
      ReqT param,
      ClientCall.Listener<RespT> responseListener,
      boolean streamingResponse) {
    startCall(call, responseListener, streamingResponse);
    try {
      call.sendMessage(param);
      call.halfClose();
    } catch (Throwable t) {
      call.cancel();
      throw Throwables.propagate(t);
    }
  }

  private static <ReqT, RespT> StreamObserver<ReqT> asyncStreamingRequestCall(
      ClientCall<ReqT, RespT> call, StreamObserver<RespT> responseObserver,
      boolean streamingResponse) {
    startCall(call, new StreamObserverToCallListenerAdapter<RespT>(
        call, responseObserver, streamingResponse), streamingResponse);
    return new CallToStreamObserverAdapter<ReqT>(call);
  }

  private static <ReqT, RespT> void startCall(ClientCall<ReqT, RespT> call,
      ClientCall.Listener<RespT> responseListener, boolean streamingResponse) {
    call.start(responseListener, new Metadata());
    if (streamingResponse) {
      call.request(1);
    } else {
      // Initially ask for two responses from flow-control so that if a misbehaving server sends
      // more than one responses, we can catch it and fail it in the listener.
      call.request(2);
    }
  }

  private static class CallToStreamObserverAdapter<T> implements StreamObserver<T> {
    private final ClientCall<T, ?> call;

    public CallToStreamObserverAdapter(ClientCall<T, ?> call) {
      this.call = call;
    }

    @Override
    public void onNext(T value) {
      call.sendMessage(value);
    }

    @Override
    public void onError(Throwable t) {
      // TODO(ejona86): log?
      call.cancel();
    }

    @Override
    public void onCompleted() {
      call.halfClose();
    }
  }

  private static class StreamObserverToCallListenerAdapter<RespT>
      extends ClientCall.Listener<RespT> {
    private final ClientCall<?, RespT> call;
    private final StreamObserver<RespT> observer;
    private final boolean streamingResponse;
    private boolean firstResponseReceived;

    public StreamObserverToCallListenerAdapter(
        ClientCall<?, RespT> call, StreamObserver<RespT> observer, boolean streamingResponse) {
      this.call = call;
      this.observer = observer;
      this.streamingResponse = streamingResponse;
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

      if (streamingResponse) {
        // Request delivery of the next inbound message.
        call.request(1);
      }
    }

    @Override
    public void onClose(Status status, Metadata trailers) {
      if (status.isOk()) {
        observer.onCompleted();
      } else {
        observer.onError(status.asRuntimeException());
      }
    }
  }

  /**
   * Complete a GrpcFuture using {@link StreamObserver} events.
   */
  private static class UnaryStreamToFuture<RespT> extends ClientCall.Listener<RespT> {
    private final GrpcFuture<RespT> responseFuture;
    private RespT value;

    public UnaryStreamToFuture(GrpcFuture<RespT> responseFuture) {
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
                  .asRuntimeException());
        }
        responseFuture.set(value);
      } else {
        responseFuture.setException(status.asRuntimeException());
      }
    }
  }

  private static class GrpcFuture<RespT> extends AbstractFuture<RespT> {
    private final ClientCall<?, RespT> call;

    GrpcFuture(ClientCall<?, RespT> call) {
      this.call = call;
    }

    @Override
    protected void interruptTask() {
      call.cancel();
    }

    @Override
    protected boolean set(@Nullable RespT resp) {
      return super.set(resp);
    }

    @Override
    protected boolean setException(Throwable throwable) {
      return super.setException(throwable);
    }
  }

  /**
   * Convert events on a {@link io.grpc.ClientCall.Listener} into a blocking
   * {@link Iterator}.
   *
   * <p>The class is not thread-safe, but it does permit {@link ClientCall.Listener} calls in a
   * separate thread from {@code Iterator} calls.
   */
  // TODO(ejona86): determine how to allow ClientCall.cancel() in case of application error.
  private static class BlockingResponseStream<T> implements Iterator<T> {
    // Due to flow control, only needs to hold up to 2 items: 1 for value, 1 for close.
    private final BlockingQueue<Object> buffer = new ArrayBlockingQueue<Object>(2);
    private final ClientCall.Listener<T> listener = new QueuingListener();
    private final ClientCall<?, T> call;
    // Only accessed when iterating.
    private Object last;

    private BlockingResponseStream(ClientCall<?, T> call) {
      this.call = call;
    }

    ClientCall.Listener<T> listener() {
      return listener;
    }

    @Override
    public boolean hasNext() {
      try {
        // Will block here indefinitely waiting for content. RPC timeouts defend against permanent
        // hangs here as the call will become closed.
        last = (last == null) ? buffer.take() : last;
      } catch (InterruptedException ie) {
        Thread.interrupted();
        throw new RuntimeException(ie);
      }
      if (last instanceof Status) {
        throw ((Status) last).asRuntimeException();
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

    private class QueuingListener extends ClientCall.Listener<T> {
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
          buffer.add(status);
        }
        done = true;
      }
    }
  }
}
