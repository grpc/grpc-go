/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.internal;

import io.grpc.CallOptions;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalInstrumented;
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
public interface ClientTransport extends InternalInstrumented<SocketStats> {

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
