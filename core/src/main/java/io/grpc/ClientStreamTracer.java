/*
 * Copyright 2017, Google Inc. All rights reserved.
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

import javax.annotation.concurrent.ThreadSafe;

/**
 * {@link StreamTracer} for the client-side.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2861")
@ThreadSafe
public abstract class ClientStreamTracer extends StreamTracer {
  /**
   * Headers has been sent to the socket.
   */
  public void outboundHeaders() {
  }

  /**
   * Headers has been received from the server.
   */
  public void inboundHeaders() {
  }

  /**
   * Factory class for {@link ClientStreamTracer}.
   */
  public abstract static class Factory {
    /**
     * Creates a {@link ClientStreamTracer} for a new client stream.
     *
     * @param headers the mutable headers of the stream. It can be safely mutated within this
     *        method.  It should not be saved because it is not safe for read or write after the
     *        method returns.
     */
    public abstract ClientStreamTracer newClientStreamTracer(Metadata headers);
  }
}
