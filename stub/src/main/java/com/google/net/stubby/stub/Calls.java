package com.google.net.stubby.stub;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.ExecutionError;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import com.google.common.util.concurrent.UncheckedExecutionException;
import com.google.net.stubby.Call;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Transport;

import java.io.InputStream;
import java.util.Iterator;
import java.util.NoSuchElementException;
import java.util.concurrent.CancellationException;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Future;
import java.util.concurrent.LinkedBlockingQueue;


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
   * Returns the result of calling {@link Future#get()} interruptably on a task known not to throw a
   * checked exception.
   *
   * <p>If interrupted, the interrupt is restored before throwing a {@code RuntimeException}.
   *
   * @throws RuntimeException if {@code get} is interrupted
   * @throws CancellationException if {@code get} throws a {@code CancellationException}
   * @throws UncheckedExecutionException if {@code get} throws an {@code ExecutionException} with an
   *     {@code Exception} as its cause
   * @throws ExecutionError if {@code get} throws an {@code ExecutionException} with an {@code
   *     Error} as its cause
   */
  private static <V> V getUnchecked(Future<V> future) {
    try {
      return future.get();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new RuntimeException(e);
    } catch (ExecutionException e) {
      if (e.getCause() == null) {
        // Strange...
        throw new UncheckedExecutionException(e);
      } else {
        if (e.getCause() instanceof Error) {
          throw new ExecutionError((Error) e.getCause());
        } else {
          throw new UncheckedExecutionException(e.getCause());
        }
      }
    }
  }

  /**
   * Execute a unary call and block on the response.
   * @return the single response message.
   */
  public static <ReqT, RespT> RespT blockingUnaryCall(Call<ReqT, RespT> call, ReqT param) {
    try {
      return getUnchecked(unaryFutureCall(call, param));
    } catch (Throwable t) {
      call.cancel();
      throw Throwables.propagate(t);
    }
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
    BlockingResponseStream<RespT> result = new BlockingResponseStream<RespT>();
    asyncServerStreamingCall(call, param, result.listener());
    return result;
  }

  /**
   * Execute a server-streaming call with a response {@link StreamObserver}.
   */
  public static <ReqT, RespT> void asyncServerStreamingCall(
      Call<ReqT, RespT> call,
      ReqT param,
      StreamObserver<RespT> responseObserver) {
    asyncServerStreamingCall(call, param,
        new StreamObserverToCallListenerAdapter<RespT>(responseObserver));
  }

  private static <ReqT, RespT> void asyncServerStreamingCall(
      Call<ReqT, RespT> call,
      ReqT param,
      Call.Listener<RespT> responseListener) {
    call.start(responseListener);
    try {
      call.sendPayload(param);
      call.halfClose();
    } catch (Throwable t) {
      call.cancel();
      throw Throwables.propagate(t);
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
        call.sendPayload(clientStream.next());
      }
      call.halfClose();
    } catch (Throwable t) {
      call.cancel();
      throw Throwables.propagate(t);
    }
    try {
      return getUnchecked(responseFuture);
    } catch (Throwable t) {
      call.cancel();
      throw Throwables.propagate(t);
    }
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
    call.start(new StreamObserverToCallListenerAdapter<RespT>(responseObserver));
    return new CallToStreamObserverAdapter<ReqT>(call);
  }

  private static class CallToStreamObserverAdapter<T> implements StreamObserver<T> {
    private final Call<T, ?> call;

    public CallToStreamObserverAdapter(Call<T, ?> call) {
      this.call = call;
    }

    @Override
    public void onValue(T value) {
      call.sendPayload(value);
    }

    @Override
    public void onError(Throwable t) {
      // TODO(user): log?
      call.cancel();
    }

    @Override
    public void onCompleted() {
      call.halfClose();
    }
  }

  private static class StreamObserverToCallListenerAdapter<T> extends Call.Listener<T> {
    private final StreamObserver<T> observer;

    public StreamObserverToCallListenerAdapter(StreamObserver<T> observer) {
      this.observer = observer;
    }

    @Override
    public ListenableFuture<Void> onContext(String name, InputStream value) {
      // StreamObservers don't receive contexts.
      return null;
    }

    @Override
    public ListenableFuture<Void> onPayload(T payload) {
      observer.onValue(payload);
      return null;
    }

    @Override
    public void onClose(Status status) {
      if (status.isOk()) {
        observer.onCompleted();
      } else {
        observer.onError(status.asRuntimeException());
      }
    }
  }

  /**
   * Complete a SettableFuture using {@link StreamObserver} events.
   */
  private static class UnaryStreamToFuture<RespT> extends Call.Listener<RespT> {
    private final SettableFuture<RespT> responseFuture;
    private RespT value;

    public UnaryStreamToFuture(SettableFuture<RespT> responseFuture) {
      this.responseFuture = responseFuture;
    }

    @Override
    public ListenableFuture<Void> onContext(String name, InputStream value) {
      // Don't care about contexts.
      return null;
    }

    @Override
    public ListenableFuture<Void> onPayload(RespT value) {
      if (this.value != null) {
        throw new Status(Transport.Code.INTERNAL, "More than one value received for unary call")
            .asRuntimeException();
      }
      this.value = value;
      return null;
    }

    @Override
    public void onClose(Status status) {
      if (status.isOk()) {
        if (value == null) {
          // No value received so mark the future as an error
          responseFuture.setException(
              new Status(Transport.Code.INTERNAL, "No value received for unary call")
                  .asRuntimeException().fillInStackTrace());
        }
        responseFuture.set(value);
      } else {
        responseFuture.setException(status.asRuntimeException());
      }
    }
  }

  /**
   * Convert events on a {@link Call.Listener} into a blocking {@link Iterator}.
   *
   * <p>The class is not thread-safe, but it does permit Call.Listener calls in a separate thread
   * from Iterator calls.
   */
  // TODO(user): determine how to allow Call.cancel() in case of application error.
  private static class BlockingResponseStream<T> implements Iterator<T> {
    private final LinkedBlockingQueue<Object> buffer = new LinkedBlockingQueue<Object>();
    private final Call.Listener<T> listener = new QueuingListener();
    // Only accessed when iterating.
    private Object last;

    Call.Listener<T> listener() {
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
      if (last instanceof Throwable) {
        throw Throwables.propagate((Throwable) last);
      }
      return last != this;
    }

    @Override
    public T next() {
      if (!hasNext()) {
        throw new NoSuchElementException();
      }
      @SuppressWarnings("unchecked")
      Payload<T> tmp = (Payload<T>) last;
      last = null;
      tmp.processed.set(null);
      return tmp.value;
    }

    @Override
    public void remove() {
      throw new UnsupportedOperationException();
    }

    private class QueuingListener extends Call.Listener<T> {
      private boolean done = false;

      @Override
      public ListenableFuture<Void> onContext(String name, InputStream value) {
        // Don't care about contexts.
        return null;
      }

      @Override
      public ListenableFuture<Void> onPayload(T value) {
        Preconditions.checkState(!done, "Call already closed");
        SettableFuture<Void> future = SettableFuture.create();
        buffer.add(new Payload<T>(value, future));
        return future;
      }

      @Override
      public void onClose(Status status) {
        Preconditions.checkState(!done, "Call already closed");
        if (status.isOk()) {
          buffer.add(this);
        } else {
          buffer.add(status.asRuntimeException());
        }
        done = true;
      }
    }
  }

  private static class Payload<T> {
    public final T value;
    public final SettableFuture<Void> processed;

    public Payload(T value, SettableFuture<Void> processed) {
      this.value = value;
      this.processed = processed;
    }
  }
}
