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

package io.grpc.internal;

import io.grpc.Metadata;
import io.grpc.Status;

/** An observer of client-side stream events. */
public interface ClientStreamListener extends StreamListener {
  /**
   * Called upon receiving all header information from the remote end-point. Note that transports
   * are not required to call this method if no header information is received, this would occur
   * when a stream immediately terminates with an error and only
   * {@link #closed(io.grpc.Status, Metadata)} is called.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param headers the fully buffered received headers.
   */
  void headersRead(Metadata headers);

  /**
   * Called when the stream is fully closed. {@link
   * io.grpc.Status.Code#OK} is the only status code that is guaranteed
   * to have been sent from the remote server. Any other status code may have been caused by
   * abnormal stream termination. This is guaranteed to always be the final call on a listener. No
   * further callbacks will be issued.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param status details about the remote closure
   * @param trailers trailing metadata
   */
  // TODO(zdapeng): remove this method in favor of the 3-arg one.
  void closed(Status status, Metadata trailers);

  /**
   * Called when the stream is fully closed. {@link
   * io.grpc.Status.Code#OK} is the only status code that is guaranteed
   * to have been sent from the remote server. Any other status code may have been caused by
   * abnormal stream termination. This is guaranteed to always be the final call on a listener. No
   * further callbacks will be issued.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param status details about the remote closure
   * @param rpcProgress RPC progress when client stream listener is closed
   * @param trailers trailing metadata
   */
  void closed(Status status, RpcProgress rpcProgress, Metadata trailers);

  /**
   * The progress of the RPC when client stream listener is closed.
   */
  enum RpcProgress {
    /**
     * The RPC is processed by the server normally.
     */
    PROCESSED,
    /**
     * The RPC is not processed by the server's application logic.
     */
    REFUSED,
    /**
     * The RPC is dropped (by load balancer).
     */
    DROPPED
  }
}
