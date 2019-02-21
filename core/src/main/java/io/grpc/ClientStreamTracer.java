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

import io.grpc.Grpc;
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
   * Trailing metadata has been received from the server.
   *
   * @param trailers the mutable trailing metadata.  Modifications to it will be seen by
   *                 interceptors and the application.
   * @since 1.17.0
   */
  public void inboundTrailers(Metadata trailers) {
  }

  /**
   * Factory class for {@link ClientStreamTracer}.
   */
  public abstract static class Factory {
    /**
     * Creates a {@link ClientStreamTracer} for a new client stream.
     *
     * @param callOptions the effective CallOptions of the call
     * @param headers the mutable headers of the stream. It can be safely mutated within this
     *        method.  It should not be saved because it is not safe for read or write after the
     *        method returns.
     *
     * @deprecated use {@link #newClientStreamTracer(StreamInfo, Metadata)} instead
     */
    @Deprecated
    public ClientStreamTracer newClientStreamTracer(CallOptions callOptions, Metadata headers) {
      throw new UnsupportedOperationException("Not implemented");
    }

    /**
     * Creates a {@link ClientStreamTracer} for a new client stream.  This is called inside the
     * transport when it's creating the stream.
     *
     * @param info information about the stream
     * @param headers the mutable headers of the stream. It can be safely mutated within this
     *        method.  Changes made to it will be sent by the stream.  It should not be saved
     *        because it is not safe for read or write after the method returns.
     *
     * @since 1.20.0
     */
    @SuppressWarnings("deprecation")
    public ClientStreamTracer newClientStreamTracer(StreamInfo info, Metadata headers) {
      return newClientStreamTracer(info.getCallOptions(), headers);
    }
  }

  /**
   * Information about a stream.
   */
  public abstract static class StreamInfo {
    /**
     * Returns the attributes of the transport that this stream was created on.
     */
    @Grpc.TransportAttr
    public abstract Attributes getTransportAttrs();

    /**
     * Returns the effective CallOptions of the call.
     */
    public abstract CallOptions getCallOptions();
  }
}
