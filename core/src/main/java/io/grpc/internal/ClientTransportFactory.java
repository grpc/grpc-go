/*
 * Copyright 2014, gRPC Authors All rights reserved.
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

import java.io.Closeable;
import java.net.SocketAddress;
import javax.annotation.Nullable;

/** Pre-configured factory for creating {@link ConnectionClientTransport} instances. */
public interface ClientTransportFactory extends Closeable {
  /**
   * Creates an unstarted transport for exclusive use.
   *
   * @param serverAddress the address that the transport is connected to
   * @param authority the HTTP/2 authority of the server
   */
  ConnectionClientTransport newClientTransport(SocketAddress serverAddress, String authority,
      @Nullable String userAgent);

  /**
   * Releases any resources.
   *
   * <p>After this method has been called, it's no longer valid to call
   * {@link #newClientTransport}. No guarantees about thread-safety are made.
   */
  @Override
  void close();
}
