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

package io.grpc;

/**
 * The connectivity states.
 *
 * @see <a href="https://github.com/grpc/grpc/blob/master/doc/connectivity-semantics-and-api.md">
 * more information</a>
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4359")
public enum ConnectivityState {
  /**
   * The channel is trying to establish a connection and is waiting to make progress on one of the
   * steps involved in name resolution, TCP connection establishment or TLS handshake. This may be
   * used as the initial state for channels upon creation.
   */
  CONNECTING,

  /**
   * The channel has successfully established a connection all the way through TLS handshake (or
   * equivalent) and all subsequent attempt to communicate have succeeded (or are pending without
   * any known failure ).
   */
  READY,

  /**
   * There has been some transient failure (such as a TCP 3-way handshake timing out or a socket
   * error). Channels in this state will eventually switch to the CONNECTING state and try to
   * establish a connection again. Since retries are done with exponential backoff, channels that
   * fail to connect will start out spending very little time in this state but as the attempts
   * fail repeatedly, the channel will spend increasingly large amounts of time in this state. For
   * many non-fatal failures (e.g., TCP connection attempts timing out because the server is not
   * yet available), the channel may spend increasingly large amounts of time in this state.
   */
  TRANSIENT_FAILURE,

  /**
   * This is the state where the channel is not even trying to create a connection because of a
   * lack of new or pending RPCs. New RPCs MAY be created in this state. Any attempt to start an
   * RPC on the channel will push the channel out of this state to connecting. When there has been
   * no RPC activity on a channel for a configurable IDLE_TIMEOUT, i.e., no new or pending (active)
   * RPCs for this period, channels that are READY or CONNECTING switch to IDLE. Additionaly,
   * channels that receive a GOAWAY when there are no active or pending RPCs should also switch to
   * IDLE to avoid connection overload at servers that are attempting to shed connections.
   */
  IDLE,

  /**
   * This channel has started shutting down. Any new RPCs should fail immediately. Pending RPCs
   * may continue running till the application cancels them. Channels may enter this state either
   * because the application explicitly requested a shutdown or if a non-recoverable error has
   * happened during attempts to connect communicate . (As of 6/12/2015, there are no known errors
   * (while connecting or communicating) that are classified as non-recoverable) Channels that
   * enter this state never leave this state.
   */
  SHUTDOWN
}
