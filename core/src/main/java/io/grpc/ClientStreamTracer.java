/*
 * Copyright 2017 The gRPC Authors
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
     * @deprecated Override/call {@link #newClientStreamTracer(CallOptions, Metadata)} instead. 
     */
    @Deprecated
    public ClientStreamTracer newClientStreamTracer(Metadata headers) {
      throw new UnsupportedOperationException("This method will be deleted. Do not call.");
    }

    /**
     * Creates a {@link ClientStreamTracer} for a new client stream.
     *
     * @param callOptions the effective CallOptions of the call
     * @param headers the mutable headers of the stream. It can be safely mutated within this
     *        method.  It should not be saved because it is not safe for read or write after the
     *        method returns.
     */
    @SuppressWarnings("deprecation")
    public ClientStreamTracer newClientStreamTracer(CallOptions callOptions, Metadata headers) {
      return newClientStreamTracer(headers);
    }
  }
}
