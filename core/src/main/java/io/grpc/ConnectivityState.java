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

package io.grpc;

/**
 * The connectivity states.
 *
 * @see <a href="https://github.com/grpc/grpc/blob/master/doc/connectivity-semantics-and-api.md">
 * more information</a>
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/28")
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
