/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

package io.grpc.inprocess;

import io.grpc.internal.InternalServer;
import io.grpc.internal.ServerListener;
import io.grpc.internal.ServerTransportListener;
import java.io.IOException;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import javax.annotation.concurrent.ThreadSafe;

@ThreadSafe
class InProcessServer implements InternalServer {
  private static final ConcurrentMap<String, InProcessServer> registry
      = new ConcurrentHashMap<String, InProcessServer>();

  static InProcessServer findServer(String name) {
    return registry.get(name);
  }

  private final String name;
  private ServerListener listener;
  private boolean shutdown;

  InProcessServer(String name) {
    this.name = name;
  }

  @Override
  public void start(ServerListener serverListener) throws IOException {
    this.listener = serverListener;
    // Must be last, as channels can start connecting after this point.
    if (registry.putIfAbsent(name, this) != null) {
      throw new IOException("name already registered: " + name);
    }
  }

  @Override
  public int getPort() {
    return -1;
  }

  @Override
  public void shutdown() {
    if (!registry.remove(name, this)) {
      throw new AssertionError();
    }
    synchronized (this) {
      shutdown = true;
      listener.serverShutdown();
    }
  }

  synchronized ServerTransportListener register(InProcessTransport transport) {
    if (shutdown) {
      return null;
    }
    return listener.transportCreated(transport);
  }
}
