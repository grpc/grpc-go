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

/**
 * An instance of a call to to a remote method. A call will send zero or more
 * request messages to the server and receive zero or more response messages back.
 *
 * <p>Instances are created
 * by a {@link Channel} and used by stubs to invoke their remote behavior.
 *
 * <p>More advanced usages may consume this interface directly as opposed to using a stub. Common
 * reasons for doing so would be the need to interact with flow-control or when acting as a generic
 * proxy for arbitrary operations.
 *
 * <p>{@link #start start()} must be called prior to calling any other methods. {@link #cancel} must
 * not be followed by any other methods, but can be called more than once, while only the first one
 * has effect.
 *
 * <p>No generic method for determining message receipt or providing acknowledgement is provided.
 * Applications are expected to utilize normal payload messages for such signals, as a response
 * naturally acknowledges its request.
 *
 * <p>Methods are guaranteed to be non-blocking. Implementations are not required to be thread-safe.
 *
 * <p>There is no interaction between the states on the {@link Listener Listener} and {@link
 * ClientCall}, i.e., if {@link Listener#onClose Listener.onClose()} is called, it has no bearing on
 * the permitted operations on {@code ClientCall} (but it may impact whether they do anything).
 *
 * <p>There is a race between {@link #cancel} and the completion/failure of the RPC in other ways.
 * If {@link #cancel} won the race, {@link Listener#onClose Listener.onClose()} is called with
 * {@link Status#CANCELLED CANCELLED}. Otherwise, {@link Listener#onClose Listener.onClose()} is
 * called with whatever status the RPC was finished. We ensure that at most one is called.
 *
 * @param <ReqT> type of message sent one or more times to the server.
 * @param <RespT> type of message received one or more times from the server.
 */
public abstract class ClientCall<ReqT, RespT> {
  /**
   * Callbacks for receiving metadata, response messages and completion status from the server.
   *
   * <p>Implementations are free to block for extended periods of time. Implementations are not
   * required to be thread-safe.
   */
  public abstract static class Listener<T> {

    /**
     * The response headers have been received. Headers always precede messages.
     * This method is always called, if no headers were received then an empty {@link Metadata}
     * is passed.
     *
     * @param headers containing metadata sent by the server at the start of the response.
     */
    public void onHeaders(Metadata headers) {}

    /**
     * A response message has been received. May be called zero or more times depending on whether
     * the call response is empty, a single message or a stream of messages.
     *
     * @param message returned by the server
     */
    public void onMessage(T message) {}

    /**
     * The ClientCall has been closed. Any additional calls to the {@code ClientCall} will not be
     * processed by the server. No further receiving will occur and no further notifications will be
     * made.
     *
     * <p>If {@code status} returns false for {@link Status#isOk()}, then the call failed.
     * An additional block of trailer metadata may be received at the end of the call from the
     * server. An empty {@link Metadata} object is passed if no trailers are received.
     *
     * @param status the result of the remote call.
     * @param trailers metadata provided at call completion.
     */
    public void onClose(Status status, Metadata trailers) {}

    /**
     * This indicates that the ClientCall is now capable of sending additional messages (via
     * {@link #sendMessage}) without requiring excessive buffering internally. This event is
     * just a suggestion and the application is free to ignore it, however doing so may
     * result in excessive buffering within the ClientCall.
     */
    public void onReady() {}
  }

  /**
   * Start a call, using {@code responseListener} for processing response messages.
   *
   * <p>It must be called prior to any other method on this class.
   *
   * @param responseListener receives response messages
   * @param headers which can contain extra call metadata, e.g. authentication credentials.
   * @throws IllegalStateException if a method (including {@code start()}) on this class has been
   *                               called.
   */
  public abstract void start(Listener<RespT> responseListener, Metadata headers);

  /**
   * Requests up to the given number of messages from the call to be delivered to
   * {@link Listener#onMessage(Object)}. No additional messages will be delivered.
   *
   * <p>Message delivery is guaranteed to be sequential in the order received. In addition, the
   * listener methods will not be accessed concurrently. While it is not guaranteed that the same
   * thread will always be used, it is guaranteed that only a single thread will access the listener
   * at a time.
   *
   * <p>If it is desired to bypass inbound flow control, a very large number of messages can be
   * specified (e.g. {@link Integer#MAX_VALUE}).
   *
   * <p>If called multiple times, the number of messages able to delivered will be the sum of the
   * calls.
   *
   * <p>This method is safe to call from multiple threads without external synchronizaton.
   *
   * @param numMessages the requested number of messages to be delivered to the listener. Must be
   *                    non-negative.
   */
  public abstract void request(int numMessages);

  /**
   * Prevent any further processing for this {@code ClientCall}. No further messages may be sent or
   * will be received. The server is informed of cancellations, but may not stop processing the
   * call. Cancellation is permitted if previously {@link #halfClose}d. Cancelling an already {@code
   * cancel()}ed {@code ClientCall} has no effect.
   *
   *
   * <p>No other methods on this class can be called after this method has been called.
   */
  public abstract void cancel();

  /**
   * Close the call for request message sending. Incoming response messages are unaffected.
   *
   * @throws IllegalStateException if call is already {@code halfClose()}d or {@link #cancel}ed
   */
  public abstract void halfClose();

  /**
   * Send a request message to the server. May be called zero or more times depending on how many
   * messages the server is willing to accept for the operation.
   *
   * @param message message to be sent to the server.
   * @throws IllegalStateException if call is {@link #halfClose}d or explicitly {@link #cancel}ed
   */
  public abstract void sendMessage(ReqT message);

  /**
   * If {@code true}, indicates that the call is capable of sending additional messages
   * without requiring excessive buffering internally. This event is
   * just a suggestion and the application is free to ignore it, however doing so may
   * result in excessive buffering within the call.
   *
   * <p>This implementation always returns {@code true}.
   */
  public boolean isReady() {
    return true;
  }
}
