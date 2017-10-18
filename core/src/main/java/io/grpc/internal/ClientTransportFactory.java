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
import java.util.concurrent.ScheduledExecutorService;
import javax.annotation.Nullable;

/** Pre-configured factory for creating {@link ConnectionClientTransport} instances. */
public interface ClientTransportFactory extends Closeable {
  /**
   * Creates an unstarted transport for exclusive use.
   *
   * @param serverAddress the address that the transport is connected to
   * @param authority the HTTP/2 authority of the server
   * @param proxy the proxy that should be used to connect to serverAddress
   */
  ConnectionClientTransport newClientTransport(SocketAddress serverAddress, String authority,
      @Nullable String userAgent, @Nullable ProxyParameters proxy);

  /**
   * Returns an executor for scheduling provided by the transport. The service should be configured
   * to allow cancelled scheduled runnables to be GCed.
   *
   * <p>The executor should not be used after the factory has been closed. The caller should ensure
   * any outstanding tasks are cancelled before the factory is closed. However, it is a
   * <a href="https://github.com/grpc/grpc-java/issues/1981">known issue</a> that ClientCallImpl may
   * use this executor after close, so implementations should not go out of their way to prevent
   * usage.
   */
  ScheduledExecutorService getScheduledExecutorService();

  /**
   * Releases any resources.
   *
   * <p>After this method has been called, it's no longer valid to call
   * {@link #newClientTransport}. No guarantees about thread-safety are made.
   */
  @Override
  void close();
}
