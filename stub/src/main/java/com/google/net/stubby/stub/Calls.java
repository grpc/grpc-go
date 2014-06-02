package com.google.net.stubby.stub;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Transport;

import java.util.Iterator;
import java.util.NoSuchElementException;
import java.util.concurrent.CancellationException;
import java.util.concurrent.LinkedBlockingQueue;

import javax.annotation.concurrent.ThreadSafe;

/**
 * Utility functions for processing different call idioms. We have one-to-one correspondence
 * between utilities in this class and the potential signatures in a generated stub class so
 * that the runtime can vary behavior without requiring regeneration of the stub.
 */
public class Calls {

  /**
   * Execute a unary call and return a {@link ListenableFuture} to the response.
   * @return a future for the single response message.
   */
  public static <ReqT, RespT> ListenableFuture<RespT> unaryFutureCall(
      Call<ReqT, RespT> call,
      ReqT param) {
    SettableFuture<RespT> responseFuture = SettableFuture.create();
    asyncServerStreamingCall(call, param, new UnaryStreamToFuture<RespT>(responseFuture));
    return responseFuture;
  }

  /**
   * Execute a unary call and block on the response.
   * @return the single response message.
   */
  public static <ReqT, RespT> RespT blockingUnaryCall(Call<ReqT, RespT> call, ReqT param) {
    return Futures.getUnchecked(unaryFutureCall(call, param));
  }

  /**
   * Execute a unary call with a response {@link StreamObserver}.
   */
  public static <ReqT, RespT> void asyncUnaryCall(
      Call<ReqT, RespT> call,
      ReqT param,
      StreamObserver<RespT> observer) {
    asyncServerStreamingCall(call, param, observer);
  }

  /**
   * Execute a server-streaming call returning a blocking {@link Iterator} over the
   * response stream.
   * @return an iterator over the response stream.
   */
  // TODO(user): Not clear if we want to use this idiom for 'simple' stubs.
  public static <ReqT, RespT> Iterator<RespT> blockingServerStreamingCall(
      Call<ReqT, RespT> call, ReqT param) {
    // This is an interesting scenario for flow control...
    // TODO(user): Capacity restriction is entirely arbitrary, need to parameterize.
    BlockingResponseStream<RespT> result = new BlockingResponseStream<>(4096);
    asyncServerStreamingCall(call, param, result);
    return result;
  }

  /**
   * Execute a server-streaming call with a response {@link StreamObserver}.
   */
  public static <ReqT, RespT> void asyncServerStreamingCall(
      Call<ReqT, RespT> call,
      ReqT param,
      StreamObserver<RespT> responseObserver) {
    call.start(responseObserver);
    try {
      call.onValue(param);
      call.onCompleted();
    } catch (Throwable t) {
      call.onError(t);
    }
  }

  /**
   * Execute a client-streaming call with a blocking {@link Iterator} of request messages.
   * @return the single response value.
   */
  public static <ReqT, RespT> RespT blockingClientStreamingCall(
      Call<ReqT, RespT> call,
      Iterator<ReqT> clientStream) {
    SettableFuture<RespT> responseFuture = SettableFuture.create();
    call.start(new UnaryStreamToFuture<RespT>(responseFuture));
    try {
      while (clientStream.hasNext()) {
        call.onValue(clientStream.next());
      }
      call.onCompleted();
    } catch (Throwable t) {
      // Notify runtime of the error which will cancel the call
      call.onError(t);
      throw Throwables.propagate(t);
    }
    return Futures.getUnchecked(responseFuture);
  }

  /**
   * Execute a client-streaming call returning a {@link StreamObserver} for the request messages.
   * @return request stream observer.
   */
  public static <ReqT, RespT> StreamObserver<ReqT> asyncClientStreamingCall(
      Call<ReqT, RespT> call,
      StreamObserver<RespT> responseObserver) {
    return duplexStreamingCall(call, responseObserver);
  }


  /**
   * Execute a duplex-streaming call.
   * @return request stream observer.
   */
  public static <ReqT, RespT> StreamObserver<ReqT> duplexStreamingCall(
      Call<ReqT, RespT> call, StreamObserver<RespT> responseObserver) {
    call.start(responseObserver);
    return call;
  }

  /**
   * Complete a SettableFuture using {@link StreamObserver} events.
   */
  private static class UnaryStreamToFuture<RespT> implements StreamObserver<RespT> {
    private final SettableFuture<RespT> responseFuture;
    private RespT value;

    public UnaryStreamToFuture(SettableFuture<RespT> responseFuture) {
      this.responseFuture = responseFuture;
    }

    @Override
    public void onValue(RespT value) {
      if (this.value != null) {
        throw new Status(Transport.Code.INTERNAL, "More than one value received for unary call")
            .asRuntimeException();
      }
      this.value = value;
    }

    @Override
    public void onError(Throwable t) {
      responseFuture.setException(t);
    }

    @Override
    public void onCompleted() {
      if (value == null) {
        // No value received so mark the future as an error
        responseFuture.setException(
            new Status(Transport.Code.INTERNAL, "No value received for unary call")
                .asRuntimeException().fillInStackTrace());
      }
      responseFuture.set(value);
    }
  }

  /**
   * Convert events on a {@link StreamObserver} into a blocking {@link Iterator}
   */
  @ThreadSafe
  private static class BlockingResponseStream<T> implements Iterator<T>, StreamObserver<T> {

    private final LinkedBlockingQueue<Object> buffer;
    private final int maxBufferSize;
    private Object last;
    private boolean done = false;

    /**
     * Construct a buffering iterator so that blocking clients can consume the response stream.
     * @param maxBufferSize limit on number of messages in the buffer before the stream is
     * terminated.
     */
    private BlockingResponseStream(int maxBufferSize) {
      buffer = new LinkedBlockingQueue<Object>();
      this.maxBufferSize = maxBufferSize;
    }

    @Override
    public synchronized void onValue(T value) {
      Preconditions.checkState(!done, "Call to onValue afer onError/onCompleted");
      if (buffer.size() >= maxBufferSize) {
        // Throw the exception as observables are required to propagate it to onError
        throw new CancellationException("Buffer size exceeded");
      } else {
        buffer.offer(value);
      }
    }

    @Override
    public synchronized void onError(Throwable t) {
      Preconditions.checkState(!done, "Call to onError after call to onError/onCompleted");
      buffer.offer(t);
      done = true;
    }

    @Override
    public synchronized void onCompleted() {
      Preconditions.checkState(!done, "Call to onCompleted after call to onError/onCompleted");
      buffer.offer(this);
      done = true;
    }

    @Override
    public synchronized boolean hasNext() {
      // Will block here indefinitely waiting for content, RPC timeouts defend against
      // permanent hangs here as onError will be reported.
      try {
        last = (last == null) ? buffer.take() : last;
        if (last instanceof Throwable) {
          throw Throwables.propagate((Throwable) last);
        }
        return last != this;
      } catch (InterruptedException ie) {
        Thread.interrupted();
        throw Status.fromThrowable(ie).asRuntimeException();
      }
    }

    @Override
    public synchronized T next() {
      if (!hasNext()) {
        throw new NoSuchElementException();
      }
      T tmp = (T) last;
      last = null;
      return tmp;
    }

    @Override
    public void remove() {
      throw new UnsupportedOperationException();
    }
  }
}
