package com.google.net.stubby.stub;

/**
 * Receives notifications from an observable stream of messages.
 *
 * <p>Implementations are expected to be thread-compatible. Separate StreamObservers do not need to
 * be sychronized together; incoming and outgoing directions are independent.
 */
// TODO(user): Consider whether we need to interact with flow-control at this layer. E.g.
// public ListenableFuture<Void> onValue(V value). Do we layer it in here or as an additional
// interface? Interaction with flow control can be done by blocking here.
public interface StreamObserver<V>  {
  /**
   * Receive a value from the stream.
   *
   * <p>Can be called many times but is never called after onError or onCompleted are called.
   *
   * <p>If an exception is thrown by an implementation the caller is expected to terminate the
   * stream by calling {@linkplain #onError(Throwable)} with the caught exception prior to
   * propagating it.
   */
  public void onValue(V value);

  /**
   * Receive a terminating error from the stream.
   *
   * <p>May only be called once and is never called after onCompleted. In particular if an exception
   * is thrown by an implementation of onError no further calls to any method are allowed.
   */
  public void onError(Throwable t);

  /**
   * Notifies successful stream completion.
   *
   * <p>May only be called once and is never called after onError. In particular if an exception is
   * thrown by an implementation of onCompleted no further calls to any method are allowed.
   */
  public void onCompleted();
}
