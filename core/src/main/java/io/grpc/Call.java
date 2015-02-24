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
 * <p>{@link #start} must be called prior to calling any other methods.
 *
 * <p>No generic method for determining message receipt or providing acknowledgement is provided.
 * Applications are expected to utilize normal payload messages for such signals, as a response
 * naturally acknowledges its request.
 *
 * <p>Methods are guaranteed to be non-blocking. Implementations are not required to be thread-safe.
 *
 * @param <RequestT> type of message sent one or more times to the server.
 * @param <ResponseT> type of message received one or more times from the server.
 */
public abstract class Call<RequestT, ResponseT> {
  /**
   * Callbacks for receiving metadata, response messages and completion status from the server.
   *
   * <p>Implementations are free to block for extended periods of time. Implementations are not
   * required to be thread-safe.
   */
  public abstract static class Listener<T> {

    /**
     * The response headers have been received. Headers always precede payloads.
     * This method is always called, if no headers were received then an empty {@link Metadata}
     * is passed.
     *
     * @param headers containing metadata sent by the server at the start of the response.
     */
    public abstract void onHeaders(Metadata.Headers headers);

    /**
     * A response payload has been received. May be called zero or more times depending on whether
     * the call response is empty, a single message or a stream of messages.
     *
     * @param  payload returned by the server
     */
    public abstract void onPayload(T payload);

    /**
     * The Call has been closed. No further sending or receiving can occur. If {@code status} is
     * not equal to {@link Status#OK}, then the call failed. An additional block of trailer metadata
     * may be received at the end of the call from the server. An empty {@link Metadata} object is
     * passed if no trailers are received.
     *
     * @param status the result of the remote call.
     * @param trailers metadata provided at call completion.
     */
    public abstract void onClose(Status status, Metadata.Trailers trailers);
  }

  /**
   * Start a call, using {@code responseListener} for processing response messages.
   *
   * @param responseListener receives response messages
   * @param headers which can contain extra call metadata, e.g. authentication credentials.
   * @throws IllegalStateException if call is already started
   */
  public abstract void start(Listener<ResponseT> responseListener, Metadata.Headers headers);

  /**
   * Requests up to the given number of messages from the call to be delivered to
   * {@link Listener#onPayload(Object)}. No additional messages will be delivered.
   *
   * <p>Message delivery is guaranteed to be sequential in the order received. In addition, the
   * listener methods will not be accessed concurrently. While it is not guaranteed that the same
   * thread will always be used, it is guaranteed that only a single thread will access the listener
   * at a time.
   *
   * <p>If it is desired to bypass inbound flow control, a very large number of messages can be
   * specified (e.g. {@link Integer#MAX_VALUE}).
   *
   * @param numMessages the requested number of messages to be delivered to the listener.
   */
  public abstract void request(int numMessages);

  /**
   * Prevent any further processing for this Call. No further messages may be sent or will be
   * received. The server is informed of cancellations, but may not stop processing the call.
   * Cancellation is permitted even if previously {@code cancel()}ed or {@link #halfClose}d.
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
   * @param payload message to be sent to the server.
   * @throws IllegalStateException if call is {@link #halfClose}d or explicitly {@link #cancel}ed
   */
  public abstract void sendPayload(RequestT payload);
}
