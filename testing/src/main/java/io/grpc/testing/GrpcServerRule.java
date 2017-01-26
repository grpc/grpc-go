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

package io.grpc.testing;

import io.grpc.BindableService;
import io.grpc.ExperimentalApi;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.ServerServiceDefinition;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.AbstractStub;
import io.grpc.util.MutableHandlerRegistry;
import java.util.UUID;
import java.util.concurrent.TimeUnit;
import org.junit.rules.ExternalResource;
import org.junit.rules.TestRule;

/**
 * {@code GrpcServerRule} is a JUnit {@link TestRule} that starts an in-process gRPC service with
 * a {@link MutableHandlerRegistry} for adding services. It is particularly useful for mocking out
 * external gRPC-based services and asserting that the expected requests were made.
 *
 * <p>An {@link AbstractStub} can be created against this service by using the
 * {@link ManagedChannel} provided by {@link GrpcServerRule#getChannel()}.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2488")
public class GrpcServerRule extends ExternalResource {

  private ManagedChannel channel;
  private Server server;
  private String serverName;
  private MutableHandlerRegistry serviceRegistry;
  private boolean useDirectExecutor;

  /**
   * Returns {@code this} configured to use a direct executor for the {@link ManagedChannel} and
   * {@link Server}.
   */
  public final GrpcServerRule directExecutor() {
    useDirectExecutor = true;
    return this;
  }

  /**
   * Returns a {@link ManagedChannel} connected to this service.
   */
  public final ManagedChannel getChannel() {
    return channel;
  }

  /**
   * Returns the underlying gRPC {@link Server} for this service.
   */
  public final Server getServer() {
    return server;
  }

  /**
   * Returns the randomly generated server name for this service.
   */
  public final String getServerName() {
    return serverName;
  }

  /**
   * Returns the service registry for this service. The registry is used to add service instances
   * (e.g. {@link BindableService} or {@link ServerServiceDefinition} to the server.
   */
  public final MutableHandlerRegistry getServiceRegistry() {
    return serviceRegistry;
  }

  /**
   * After the test has completed, clean up the channel and server.
   */
  @Override
  protected void after() {
    serverName = null;
    serviceRegistry = null;

    channel.shutdown();
    server.shutdown();

    try {
      channel.awaitTermination(1, TimeUnit.MINUTES);
      server.awaitTermination(1, TimeUnit.MINUTES);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new RuntimeException(e);
    } finally {
      channel.shutdownNow();
      channel = null;

      server.shutdownNow();
      server = null;
    }
  }

  /**
   * Before the test has started, create the server and channel.
   */
  @Override
  protected void before() throws Throwable {
    serverName = UUID.randomUUID().toString();

    serviceRegistry = new MutableHandlerRegistry();

    InProcessServerBuilder serverBuilder = InProcessServerBuilder.forName(serverName)
        .fallbackHandlerRegistry(serviceRegistry);

    if (useDirectExecutor) {
      serverBuilder.directExecutor();
    }

    server = serverBuilder.build().start();

    InProcessChannelBuilder channelBuilder = InProcessChannelBuilder.forName(serverName);

    if (useDirectExecutor) {
      channelBuilder.directExecutor();
    }

    channel = channelBuilder.build();
  }
}
