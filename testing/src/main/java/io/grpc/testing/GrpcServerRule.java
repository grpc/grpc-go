/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.testing;

import static com.google.common.base.Preconditions.checkState;

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
 * external gRPC-based services and asserting that the expected requests were made. However, due to
 * it's limitation that {@code GrpcServerRule} does not support useful features such as transport
 * types other than in-process, multiple channels per server, custom channel or server builder
 * options, and configuration inside individual test methods, users would end up to a difficult
 * situation when later they want to make extensions to their tests that were using {@code
 * GrpcServerRule}. So in general it is more favorable to use the more flexible {@code TestRule}
 * {@link GrpcCleanupRule} than {@code GrpcServerRule} in unit tests.
 *
 * <p>An {@link AbstractStub} can be created against this service by using the
 * {@link ManagedChannel} provided by {@link GrpcServerRule#getChannel()}.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2488")
public final class GrpcServerRule extends ExternalResource {

  private ManagedChannel channel;
  private Server server;
  private String serverName;
  private MutableHandlerRegistry serviceRegistry;
  private boolean useDirectExecutor;

  /**
   * Returns {@code this} configured to use a direct executor for the {@link ManagedChannel} and
   * {@link Server}. This can only be called at the rule instantiation.
   */
  public final GrpcServerRule directExecutor() {
    checkState(serverName == null, "directExecutor() can only be called at the rule instantiation");
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
