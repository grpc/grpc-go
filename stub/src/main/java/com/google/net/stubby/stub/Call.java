package com.google.net.stubby.stub;

/**
 * Encapsulates the producer and consumer aspects of a call. A Call is a deferred execution context
 * that does not dispatch data to the remote implementation until 'start' is called.
 *
 * <p>Call and its sub-classes are used by the stub code generators to produced typed stubs.
 */
// TODO(user): Implement context support
public interface Call<RequestT, ResponseT> extends StreamObserver<RequestT>  {
  /**
   * Start a call passing it the response {@link StreamObserver}.
   * @param responseObserver Which receive response messages.
   */
  public void start(StreamObserver<ResponseT> responseObserver);
}
