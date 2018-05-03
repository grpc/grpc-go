/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.netty;

import io.grpc.Internal;
import io.netty.channel.ChannelHandler;
import io.netty.util.AsciiString;

/**
 * A class that provides a Netty handler to control protocol negotiation.
 */
@Internal
public interface ProtocolNegotiator {

  /**
   * The Netty handler to control the protocol negotiation.
   */
  interface Handler extends ChannelHandler {
    /**
     * The HTTP/2 scheme to be used when sending {@code HEADERS}.
     */
    AsciiString scheme();
  }

  /**
   * Creates a new handler to control the protocol negotiation. Once the negotiation has completed
   * successfully, the provided handler is installed. Must call {@code
   * grpcHandler.onHandleProtocolNegotiationCompleted()} at certain point if the negotiation has
   * completed successfully.
   */
  Handler newHandler(GrpcHttp2ConnectionHandler grpcHandler);
}
