/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import java.io.IOException;
import javax.annotation.concurrent.ThreadSafe;

/**
 * An object that accepts new incoming connections. This would commonly encapsulate a bound socket
 * that {@code accept()}s new connections.
 */
@ThreadSafe
public interface InternalServer {
  /**
   * Starts transport. Implementations must not call {@code listener} until after {@code start()}
   * returns. The method only returns after it has done the equivalent of bind()ing, so it will be
   * able to service any connections created after returning.
   *
   * @param listener non-{@code null} listener of server events
   * @throws IOException if unable to bind
   */
  void start(ServerListener listener) throws IOException;

  /**
   * Initiates an orderly shutdown of the server. Existing transports continue, but new transports
   * will not be created (once {@link ServerListener#serverShutdown()} callback is called). This
   * method may only be called once.
   */
  void shutdown();

  /**
   * Returns what underlying port the server is listening on, or -1 if the port number is not
   * available or does not make sense.
   */
  int getPort();
}
