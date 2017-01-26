/*
 * Copyright 2015, Google Inc. All rights reserved.
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
