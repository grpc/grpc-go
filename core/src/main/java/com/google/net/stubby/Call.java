package com.google.net.stubby;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;

import java.io.InputStream;

import javax.annotation.Nullable;

/**
 * Low-level methods for communicating with a remote server during a single RPC. Unlike normal RPCs,
 * calls may stream any number of requests and responses, although a single request and single
 * response is most common. This API is generally intended for use by stubs, but advanced
 * applications may have need for it.
 *
 * <p>{@link #start} is required to be the first of any methods called.
 *
 * <p>Any headers must be sent before any payloads, which must be sent before half closing.
 *
 * <p>No generic method for determining message receipt or providing acknowledgement is provided.
 * Applications are expected to utilize normal payload messages for such signals, as a response
 * natually acknowledges its request.
 *
 * <p>Methods are guaranteed to be non-blocking. Implementations are not required to be thread-safe.
 */
public abstract class Call<RequestT, ResponseT> {
  /**
   * Callbacks for consuming incoming RPC messages.
   *
   * <p>Response headers are guaranteed to arrive before any payloads, which are guaranteed
   * to arrive before close. An additional block of headers called 'trailers' can be delivered with
   * close.
   *
   * <p>Implementations are free to block for extended periods of time. Implementations are not
   * required to be thread-safe.
   */
  public abstract static class Listener<T> {

    /**
     * The response headers have been received. Headers always precede payloads.
     * This method is always called, if no headers were received then an empty {@link Metadata}
     * is passed.
     */
    public abstract ListenableFuture<Void> onHeaders(Metadata.Headers headers);

    /**
     * A response context has been received. Any context messages will precede payload messages.
     *
     * <p>The {@code value} {@link InputStream} will be closed when the returned future completes.
     * If no future is returned, the value will be closed immediately after returning from this
     * method.
     */
    @Nullable
    @Deprecated
    public abstract ListenableFuture<Void> onContext(String name, InputStream value);

    /**
     * A response payload has been received. For streaming calls, there may be zero payload
     * messages.
     */
    @Nullable
    public abstract ListenableFuture<Void> onPayload(T payload);

    /**
     * The Call has been closed. No further sending or receiving can occur. If {@code status} is
     * not equal to {@link Status#OK}, then the call failed. An additional block of headers may be
     * received at the end of the call from the server. An empty {@link Metadata} object is passed
     * if no trailers are received.
     */
    public abstract void onClose(Status status, Metadata.Trailers trailers);
  }

  /**
   * Start a call, using {@code responseListener} for processing response messages.
   *
   * @param responseListener receives response messages
   * @param headers which can contain extra information like authentication.
   * @throws IllegalStateException if call is already started
   */
  // TODO(user): Might be better to put into Channel#newCall, might reduce decoration burden
  public abstract void start(Listener<ResponseT> responseListener, Metadata.Headers headers);


  /**
   * Send a context message. Context messages are intended for side-channel information like
   * statistics and authentication.
   *
   * @param name key identifier of context
   * @param value context value bytes
   * @throws IllegalStateException if call is {@link #halfClose}d or explicitly {@link #cancel}ed,
   *     or after {@link #sendPayload}
   */
  @Deprecated
  public void sendContext(String name, InputStream value) {
    sendContext(name, value, null);
  }

  /**
   * Send a context message. Context messages are intended for side-channel information like
   * statistics and authentication.
   *
   * <p>If {@code accepted} is non-{@code null}, then the future will be completed when the flow
   * control window is able to fully permit the context message. If the Call fails before being
   * accepted, then the future will be cancelled. Callers concerned with unbounded buffering should
   * wait until the future completes before sending more messages.
   *
   * @param name key identifier of context
   * @param value context value bytes
   * @param accepted notification for adhering to flow control, or {@code null}
   * @throws IllegalStateException if call is {@link #halfClose}d or explicitly {@link #cancel}ed,
   *     or after {@link #sendPayload}
   */
  @Deprecated
  public abstract void sendContext(String name, InputStream value,
                                   @Nullable SettableFuture<Void> accepted);

  /**
   * Prevent any further processing for this Call. No further messages may be sent or will be
   * received. The server is informed of cancellations, but may not stop processing the call.
   * Cancellation is permitted even if previously {@code cancel()}ed or {@link #halfClose}d.
   */
  public abstract void cancel();

  /**
   * Close call for message sending. Incoming messages are unaffected.
   *
   * @throws IllegalStateException if call is already {@code halfClose()}d or {@link #cancel}ed
   */
  public abstract void halfClose();

  /**
   * Send a payload message. Payload messages are the primary form of communication associated with
   * RPCs. Multiple payload messages may exist for streaming calls.
   *
   * @param payload message
   * @throws IllegalStateException if call is {@link #halfClose}d or explicitly {@link #cancel}ed
   */
  public void sendPayload(RequestT payload) {
    sendPayload(payload, null);
  }

  /**
   * Send a payload message. Payload messages are the primary form of communication associated with
   * RPCs. Multiple payload messages may be sent for streaming calls.
   *
   * <p>If {@code accepted} is non-{@code null}, then the future will be completed when the flow
   * control window is able to fully permit the payload message. If the Call fails before being
   * accepted, then the future will be cancelled. Callers concerned with unbounded buffering should
   * wait until the future completes before sending more messages.
   *
   * @param payload message
   * @param accepted notification for adhering to flow control, or {@code null}
   * @throws IllegalStateException if call is {@link #halfClose}d or {@link #cancel}ed
   */
  public abstract void sendPayload(RequestT payload, @Nullable SettableFuture<Void> accepted);
}
