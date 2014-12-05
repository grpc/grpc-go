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

package com.google.net.stubby;

import com.google.common.util.concurrent.ListenableFuture;

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
  public abstract void sendPayload(RequestT payload);
}
