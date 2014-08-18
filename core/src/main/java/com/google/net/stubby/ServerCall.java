package com.google.net.stubby;

import com.google.common.util.concurrent.ListenableFuture;

import java.io.InputStream;
import javax.annotation.Nullable;

/**
 * Low-level method for communicating with a remote client during a single RPC. Unlike normal RPCs,
 * calls may stream any number of requests and responses, although a single request and single
 * response is most common. This API is generally intended for use generated handlers, but advanced
 * applications may have need for it.
 *
 * <p>Any contexts must be sent before any payloads, which must be sent before closing.
 *
 * <p>No generic method for determining message receipt or providing acknowlegement is provided.
 * Applications are expected to utilize normal payload messages for such signals, as a response
 * natually acknowledges its request.
 *
 * <p>Methods are guaranteed to be non-blocking. Implementations are not required to be thread-safe.
 */
public abstract class ServerCall<RequestT, ResponseT> {
  /**
   * Callbacks for consuming incoming RPC messages.
   *
   * <p>Any contexts are guaranteed to arrive before any payloads, which are guaranteed before half
   * close, which is guaranteed before completion.
   *
   * <p>Implementations are free to block for extended periods of time. Implementations are not
   * required to be thread-safe.
   */
  // TODO(user): We need to decide what to do in the case of server closing with non-cancellation
  // before client half closes. It may be that we treat such a case as an error. If we permit such
  // a case then we either get to generate a half close or purposefully omit it.
  public abstract static class Listener<T> {
    /**
     * A request context has been received. Any context messages will precede payload messages.
     *
     * <p>The {@code value} {@link InputStream} will be closed when the returned future completes.
     * If no future is returned, the value will be closed immediately after returning from this
     * method.
     */
    @Nullable
    public abstract ListenableFuture<Void> onContext(String name, InputStream value);

    /**
     * A request payload has been receiveed. For streaming calls, there may be zero payload
     * messages.
     */
    @Nullable
    public abstract ListenableFuture<Void> onPayload(T payload);

    /**
     * The client completed all message sending. However, the call may still be cancelled.
     */
    public abstract void onHalfClose();

    /**
     * The call was cancelled and the server is encouraged to abort processing to save resources,
     * since the client will not process any further messages. Cancellations can be caused by
     * timeouts, explicit cancel by client, network errors, and similar.
     *
     * <p>There will be no further callbacks for the call.
     */
    public abstract void onCancel();

    /**
     * The call is considered complete and {@link #onCancel} is guaranteed not to be called.
     * However, the client is not guaranteed to have received all messages.
     *
     * <p>There will be no further callbacks for the call.
     */
    public abstract void onCompleted();
  }

  /**
   * Close the call with the provided status. No further sending or receiving will occur. If {@code
   * status} is not equal to {@link Status#OK}, then the call is said to have failed.
   *
   * <p>If {@code status} is not {@link Status#CANCELLED} and no errors or cancellations are known
   * to have occured, then a {@link Listener#onCompleted} notification should be expected.
   * Otherwise {@link Listener#onCancel} has been or will be called.
   *
   * @throws IllegalStateException if call is already {@code close}d
   */
  public abstract void close(Status status);

  /**
   * Send a context message. Context messages are intended for side-channel information like
   * statistics and authentication.
   *
   * @param name key identifier of context
   * @param value context value bytes
   * @throws IllegalStateException if call is {@link #close}d, or after {@link #sendPayload}
   */
  public abstract void sendContext(String name, InputStream value);

  /**
   * Send a payload message. Payload messages are the primary form of communication associated with
   * RPCs. Multiple payload messages may exist for streaming calls.
   *
   * @param payload message
   * @throws IllegalStateException if call is {@link #close}d
   */
  public abstract void sendPayload(ResponseT payload);

  /**
   * Returns {@code true} when the call is cancelled and the server is encouraged to abort
   * processing to save resources, since the client will not be processing any further methods.
   * Cancellations can be caused by timeouts, explicit cancel by client, network errors, and
   * similar.
   *
   * <p>This method may safely be called concurrently from multiple threads.
   */
  public abstract boolean isCancelled();
}
