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

package io.grpc.transport;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;

/**
 * The client-side transport encapsulating a single connection to a remote server. Allows creation
 * of new {@link Stream} instances for communication with the server.
 */
public interface ClientTransport {

  /**
   * Creates a new stream for sending messages to the remote end-point.
   *
   * <p>
   * This method returns immediately and does not wait for any validation of the request. If
   * creation fails for any reason, {@link ClientStreamListener#closed} will be called to provide
   * the error information. Any sent messages for this stream will be buffered until creation has
   * completed (either successfully or unsuccessfully).
   *
   * @param method the descriptor of the remote method to be called for this stream.
   * @param headers to send at the beginning of the call
   * @param listener the listener for the newly created stream.
   * @throws IllegalStateException if the service is already stopped.
   * @return the newly created stream.
   */
  // TODO(nmittler): Consider also throwing for stopping.
  ClientStream newStream(MethodDescriptor<?, ?> method,
                         Metadata.Headers headers,
                         ClientStreamListener listener);

  /**
   * Starts transport. Implementations must not call {@code listener} until after {@code start()} returns.
   *
   * @param listener non-{@code null} listener of transport events
   */
  void start(Listener listener);

  /**
   * Initiates an orderly shutdown of the transport. Existing streams continue, but new streams will
   * fail (once {@link Listener#transportShutdown()} callback called).
   */
  void shutdown();

  /**
   * Receives notifications for the transport life-cycle events.
   */
  interface Listener {
    /**
     * The transport is shutting down. No new streams will be processed, but existing streams may
     * continue. Shutdown could have been caused by an error or normal operation.
     */
    void transportShutdown();

    /**
     * The transport completed shutting down. All resources have been released.
     */
    void transportTerminated();
  }
}
