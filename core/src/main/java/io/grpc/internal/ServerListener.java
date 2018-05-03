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

/**
 * A listener to a server for transport creation events. The listener need not be thread-safe, so
 * notifications must be properly synchronized externally.
 */
public interface ServerListener {

  /**
   * Called upon the establishment of a new client connection.
   *
   * @param transport the new transport to be observed.
   * @return a listener for stream creation events on the transport.
   */
  ServerTransportListener transportCreated(ServerTransport transport);

  /**
   * The server is shutting down. No new transports will be processed, but existing transports may
   * continue. Shutdown is only caused by a call to {@link InternalServer#shutdown()}. All
   * resources have been released.
   */
  void serverShutdown();
}
