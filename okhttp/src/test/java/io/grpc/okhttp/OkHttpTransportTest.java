/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.okhttp;

import io.grpc.ServerStreamTracer;
import io.grpc.internal.AccessProtectedHack;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.InternalServer;
import io.grpc.internal.ManagedClientTransport;
import io.grpc.internal.testing.AbstractTransportTest;
import io.grpc.netty.NettyServerBuilder;
import java.net.InetSocketAddress;
import java.util.List;
import org.junit.After;
import org.junit.Ignore;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for OkHttp transport. */
@RunWith(JUnit4.class)
public class OkHttpTransportTest extends AbstractTransportTest {
  private ClientTransportFactory clientFactory = OkHttpChannelBuilder
      // Although specified here, address is ignored because we never call build.
      .forAddress("::1", 0)
      .negotiationType(NegotiationType.PLAINTEXT)
      .buildTransportFactory();

  @After
  public void releaseClientFactory() {
    clientFactory.close();
  }

  @Override
  protected InternalServer newServer(List<ServerStreamTracer.Factory> streamTracerFactories) {
    return AccessProtectedHack.serverBuilderBuildTransportServer(
        NettyServerBuilder
          .forPort(0)
          .flowControlWindow(65 * 1024),
        streamTracerFactories);
  }

  @Override
  protected InternalServer newServer(
      InternalServer server, List<ServerStreamTracer.Factory> streamTracerFactories) {
    int port = server.getPort();
    return AccessProtectedHack.serverBuilderBuildTransportServer(
        NettyServerBuilder
            .forPort(port)
            .flowControlWindow(65 * 1024),
        streamTracerFactories);
  }

  @Override
  protected String testAuthority(InternalServer server) {
    return "[::1]:" + server.getPort();
  }

  @Override
  protected ManagedClientTransport newClientTransport(InternalServer server) {
    int port = server.getPort();
    return clientFactory.newClientTransport(
        new InetSocketAddress("::1", port),
        testAuthority(server),
        null /* agent */);
  }

  @Override
  protected boolean metricsExpected() {
    return true;
  }

  // TODO(ejona): Flaky/Broken
  @Test
  @Ignore
  @Override
  public void flowControlPushBack() {}
}
