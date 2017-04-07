/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.internal;

import io.grpc.CallOptions;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import java.util.concurrent.Executor;
import javax.annotation.concurrent.ThreadSafe;

/**
 * The client-side transport typically encapsulating a single connection to a remote
 * server. However, streams created before the client has discovered any server address may
 * eventually be issued on different connections.  All methods on the transport and its callbacks
 * are expected to execute quickly.
 */
@ThreadSafe
public interface ClientTransport {

  /**
   * Creates a new stream for sending messages to a remote end-point.
   *
   * <p>This method returns immediately and does not wait for any validation of the request. If
   * creation fails for any reason, {@link ClientStreamListener#closed} will be called to provide
   * the error information. Any sent messages for this stream will be buffered until creation has
   * completed (either successfully or unsuccessfully).
   *
   * <p>This method is called under the {@link io.grpc.Context} of the {@link io.grpc.ClientCall}.
   *
   * @param method the descriptor of the remote method to be called for this stream.
   * @param headers to send at the beginning of the call
   * @param callOptions runtime options of the call
   * @return the newly created stream.
   */
  // TODO(nmittler): Consider also throwing for stopping.
  ClientStream newStream(MethodDescriptor<?, ?> method, Metadata headers, CallOptions callOptions);

  // TODO(zdapeng): Remove two-argument version in favor of four-argument overload.
  ClientStream newStream(MethodDescriptor<?, ?> method, Metadata headers);

  /**
   * Pings a remote endpoint. When an acknowledgement is received, the given callback will be
   * invoked using the given executor.
   *
   * <p>Pings are not necessarily sent to the same endpont, thus a successful ping only means at
   * least one endpoint responded, but doesn't imply the availability of other endpoints (if there
   * is any).
   *
   * <p>This is an optional method. Transports that do not have any mechanism by which to ping the
   * remote endpoint may throw {@link UnsupportedOperationException}.
   */
  void ping(PingCallback callback, Executor executor);

  /**
   * A callback that is invoked when the acknowledgement to a {@link #ping} is received. Exactly one
   * of the two methods should be called per {@link #ping}.
   */
  interface PingCallback {

    /**
     * Invoked when a ping is acknowledged. The given argument is the round-trip time of the ping,
     * in nanoseconds.
     *
     * @param roundTripTimeNanos the round-trip duration between the ping being sent and the
     *     acknowledgement received
     */
    void onSuccess(long roundTripTimeNanos);

    /**
     * Invoked when a ping fails. The given argument is the cause of the failure.
     *
     * @param cause the cause of the ping failure
     */
    void onFailure(Throwable cause);
  }
}
