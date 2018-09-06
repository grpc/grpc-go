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

package io.grpc.internal;

import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalInstrumented;
import java.io.IOException;
import java.util.List;
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

  /**
   * Returns the listen sockets of this server. May return an empty list but never returns null.
   */
  List<InternalInstrumented<SocketStats>> getListenSockets();
}
