/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc;

import javax.annotation.Nullable;

/**
 * Encapsulates a single call received from a remote client. Calls may not simply be unary
 * request-response even though this is the most common pattern. Calls may stream any number of
 * requests and responses. This API is generally intended for use by generated handlers,
 * but applications may use it directly if they need to.
 *
 * <p>Headers must be sent before any messages, which must be sent before closing.
 *
 * <p>No generic method for determining message receipt or providing acknowledgement is provided.
 * Applications are expected to utilize normal messages for such signals, as a response
 * naturally acknowledges its request.
 *
 * <p>Methods are guaranteed to be non-blocking. Implementations are not required to be thread-safe.
 *
 * <p>DO NOT MOCK: Use InProcessTransport and make a fake server instead.
 *
 * @param <ReqT> parsed type of request message.
 * @param <RespT> parsed type of response message.
 */
public abstract class ServerCall<ReqT, RespT> {

  /**
   * Callbacks for consuming incoming RPC messages.
   *
   * <p>Any contexts are guaranteed to arrive before any messages, which are guaranteed before half
   * close, which is guaranteed before completion.
   *
   * <p>Implementations are free to block for extended periods of time. Implementations are not
   * required to be thread-safe.
   */
  // TODO(ejona86): We need to decide what to do in the case of server closing with non-cancellation
  // before client half closes. It may be that we treat such a case as an error. If we permit such
  // a case then we either get to generate a half close or purposefully omit it.
  public abstract static class Listener<ReqT> {
    /**
     * A request message has been received. For streaming calls, there may be zero or more request
     * messages.
     *
     * @param message a received request message.
     */
    public void onMessage(ReqT message) {}

    /**
     * The client completed all message sending. However, the call may still be cancelled.
     */
    public void onHalfClose() {}

    /**
     * The call was cancelled and the server is encouraged to abort processing to save resources,
     * since the client will not process any further messages. Cancellations can be caused by
     * timeouts, explicit cancellation by the client, network errors, etc.
     *
     * <p>There will be no further callbacks for the call.
     */
    public void onCancel() {}

    /**
     * The call is considered complete and {@link #onCancel} is guaranteed not to be called.
     * However, the client is not guaranteed to have received all messages.
     *
     * <p>There will be no further callbacks for the call.
     */
    public void onComplete() {}

    /**
     * This indicates that the call is now capable of sending additional messages (via
     * {@link #sendMessage}) without requiring excessive buffering internally. This event is
     * just a suggestion and the application is free to ignore it, however doing so may
     * result in excessive buffering within the call.
     */
    public void onReady() {}
  }

  /**
   * Requests up to the given number of messages from the call to be delivered to
   * {@link Listener#onMessage(Object)}. Once {@code numMessages} have been delivered
   * no further request messages will be delivered until more messages are requested by
   * calling this method again.
   *
   * <p>Servers use this mechanism to provide back-pressure to the client for flow-control.
   *
   * <p>This method is safe to call from multiple threads without external synchronizaton.
   *
   * @param numMessages the requested number of messages to be delivered to the listener.
   */
  public abstract void request(int numMessages);

  /**
   * Send response header metadata prior to sending a response message. This method may
   * only be called once and cannot be called after calls to {@link #sendMessage} or {@link #close}.
   *
   * <p>Since {@link Metadata} is not thread-safe, the caller must not access (read or write) {@code
   * headers} after this point.
   *
   * @param headers metadata to send prior to any response body.
   * @throws IllegalStateException if {@code close} has been called, a message has been sent, or
   *     headers have already been sent
   */
  public abstract void sendHeaders(Metadata headers);

  /**
   * Send a response message. Messages are the primary form of communication associated with
   * RPCs. Multiple response messages may exist for streaming calls.
   *
   * @param message response message.
   * @throws IllegalStateException if headers not sent or call is {@link #close}d
   */
  public abstract void sendMessage(RespT message);

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

  /**
   * Close the call with the provided status. No further sending or receiving will occur. If {@link
   * Status#isOk} is {@code false}, then the call is said to have failed.
   *
   * <p>If no errors or cancellations are known to have occurred, then a {@link Listener#onComplete}
   * notification should be expected, independent of {@code status}. Otherwise {@link
   * Listener#onCancel} has been or will be called.
   *
   * <p>Since {@link Metadata} is not thread-safe, the caller must not access (read or write) {@code
   * trailers} after this point.
   *
   * @throws IllegalStateException if call is already {@code close}d
   */
  public abstract void close(Status status, Metadata trailers);

  /**
   * Returns {@code true} when the call is cancelled and the server is encouraged to abort
   * processing to save resources, since the client will not be processing any further methods.
   * Cancellations can be caused by timeouts, explicit cancel by client, network errors, and
   * similar.
   *
   * <p>This method may safely be called concurrently from multiple threads.
   */
  public abstract boolean isCancelled();

  /**
   * Enables per-message compression, if an encoding type has been negotiated.  If no message
   * encoding has been negotiated, this is a no-op. By default per-message compression is enabled,
   * but may not have any effect if compression is not enabled on the call.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public void setMessageCompression(boolean enabled) {
    // noop
  }

  /**
   * Sets the compression algorithm for this call.  If the server does not support the compression
   * algorithm, the call will fail.  This method may only be called before {@link #sendHeaders}.
   * The compressor to use will be looked up in the {@link CompressorRegistry}.  Default gRPC
   * servers support the "gzip" compressor.
   *
   * @param compressor the name of the compressor to use.
   * @throws IllegalArgumentException if the compressor name can not be found.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public void setCompression(String compressor) {
    // noop
  }

  /**
   * Returns properties of a single call.
   *
   * <p>Attributes originate from the transport and can be altered by {@link ServerTransportFilter}.
   * {@link Grpc} defines commonly used attributes, but they are not guaranteed to be present.
   *
   * @return non-{@code null} Attributes container
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1779")
  public Attributes getAttributes() {
    return Attributes.EMPTY;
  }

  /**
   * Gets the authority this call is addressed to.
   *
   * @return the authority string. {@code null} if not available.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2924")
  @Nullable
  public String getAuthority() {
    return null;
  }

  /**
   * The {@link MethodDescriptor} for the call.
   */
  public abstract MethodDescriptor<ReqT, RespT> getMethodDescriptor();
}
