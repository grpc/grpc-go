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
 * Encapsulates a single call received from a remote client. Calls may not simply be unary
 * request-response even though this is the most common pattern. Calls may stream any number of
 * requests and responses. This API is generally intended for use by generated handlers,
 * but applications may use it directly if they need to.
 *
 * <p>Headers must be sent before any payloads, which must be sent before closing.
 *
 * <p>No generic method for determining message receipt or providing acknowledgement is provided.
 * Applications are expected to utilize normal payload messages for such signals, as a response
 * naturally acknowledges its request.
 *
 * <p>Methods are guaranteed to be non-blocking. Implementations are not required to be thread-safe.
 *
 * @param <ResponseT> parsed type of response message.
 */
public abstract class ServerCall<ResponseT> {
  /**
   * Callbacks for consuming incoming RPC messages.
   *
   * <p>Any contexts are guaranteed to arrive before any payloads, which are guaranteed before half
   * close, which is guaranteed before completion.
   *
   * <p>Implementations are free to block for extended periods of time. Implementations are not
   * required to be thread-safe.
   */
  // TODO(ejona86): We need to decide what to do in the case of server closing with non-cancellation
  // before client half closes. It may be that we treat such a case as an error. If we permit such
  // a case then we either get to generate a half close or purposefully omit it.
  public abstract static class Listener<RequestT> {
    /**
     * A request message has been received. For streaming calls, there may be zero or more request
     * messages.
     *
     * @param payload a received request message.
     */
    public abstract void onPayload(RequestT payload);

    /**
     * The client completed all message sending. However, the call may still be cancelled.
     */
    public abstract void onHalfClose();

    /**
     * The call was cancelled and the server is encouraged to abort processing to save resources,
     * since the client will not process any further messages. Cancellations can be caused by
     * timeouts, explicit cancellation by the client, network errors, etc.
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
    public abstract void onComplete();
  }

  /**
   * Requests up to the given number of messages from the call to be delivered to
   * {@link Listener#onPayload(Object)}. Once {@code numMessages} have been delivered
   * no further request messages will be delivered until more messages are requested by
   * calling this method again.
   *
   * <p>Servers use this mechanism to provide back-pressure to the client for flow-control.
   *
   * @param numMessages the requested number of messages to be delivered to the listener.
   */
  public abstract void request(int numMessages);

  /**
   * Send response header metadata prior to sending a response payload. This method may
   * only be called once and cannot be called after calls to {@code Stream#sendPayload}
   * or {@code #close}.
   *
   * @param headers metadata to send prior to any response body.
   * @throws IllegalStateException if {@code close} has been called or a payload has been sent.
   */
  public abstract void sendHeaders(Metadata.Headers headers);

  /**
   * Send a response message. Payload messages are the primary form of communication associated with
   * RPCs. Multiple response messages may exist for streaming calls.
   *
   * @param payload response message.
   * @throws IllegalStateException if call is {@link #close}d
   */
  public abstract void sendPayload(ResponseT payload);

  /**
   * Close the call with the provided status. No further sending or receiving will occur. If {@code
   * status} is not equal to {@link Status#OK}, then the call is said to have failed.
   *
   * <p>If {@code status} is not {@link Status#CANCELLED} and no errors or cancellations are known
   * to have occured, then a {@link Listener#onComplete} notification should be expected.
   * Otherwise {@link Listener#onCancel} has been or will be called.
   *
   * @throws IllegalStateException if call is already {@code close}d
   */
  public abstract void close(Status status, Metadata.Trailers trailers);

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
