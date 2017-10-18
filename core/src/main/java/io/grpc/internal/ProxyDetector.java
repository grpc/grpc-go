/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

import java.net.SocketAddress;
import javax.annotation.Nullable;

/**
 * A utility class to detect which proxy, if any, should be used for a given
 * {@link java.net.SocketAddress}.
 */
public interface ProxyDetector {
  ProxyDetector DEFAULT_INSTANCE = new ProxyDetectorImpl();

  /** A proxy detector that always claims no proxy is needed, for unit test convenience. */
  ProxyDetector NOOP_INSTANCE = new ProxyDetector() {
    @Nullable
    @Override
    public ProxyParameters proxyFor(SocketAddress targetServerAddress) {
      return null;
    }
  };

  /**
   * Given a target address, returns which proxy address should be used. If no proxy should be
   * used, then return value will be null.
   */
  @Nullable
  ProxyParameters proxyFor(SocketAddress targetServerAddress);
}
