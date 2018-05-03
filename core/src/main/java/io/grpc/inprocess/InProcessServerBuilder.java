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

package io.grpc.inprocess;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.Preconditions;
import io.grpc.ExperimentalApi;
import io.grpc.ServerStreamTracer;
import io.grpc.internal.AbstractServerImplBuilder;
import io.grpc.internal.FixedObjectPool;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.ObjectPool;
import io.grpc.internal.SharedResourcePool;
import java.io.File;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

/**
 * Builder for a server that services in-process requests. Clients identify the in-process server by
 * its name.
 *
 * <p>The server is intended to be fully-featured, high performance, and useful in testing.
 *
 * <h3>Using JUnit TestRule</h3>
 * The class "GrpcServerRule" (from "grpc-java/testing") is a JUnit TestRule that
 * creates a {@link InProcessServer} and a {@link io.grpc.ManagedChannel ManagedChannel}. This
 * test rule contains the boilerplate code shown below. The classes "HelloWorldServerTest" and
 * "HelloWorldClientTest" (from "grpc-java/examples") demonstrate basic usage.
 *
 * <h3>Usage example</h3>
 * <h4>Server and client channel setup</h4>
 * <pre>
 *   String uniqueName = InProcessServerBuilder.generateName();
 *   Server server = InProcessServerBuilder.forName(uniqueName)
 *       .directExecutor() // directExecutor is fine for unit tests
 *       .addService(&#47;* your code here *&#47;)
 *       .build().start();
 *   ManagedChannel channel = InProcessChannelBuilder.forName(uniqueName)
 *       .directExecutor()
 *       .build();
 * </pre>
 *
 * <h4>Usage in tests</h4>
 * The channel can be used normally. A blocking stub example:
 * <pre>
 *   TestServiceGrpc.TestServiceBlockingStub blockingStub =
 *       TestServiceGrpc.newBlockingStub(channel);
 * </pre>
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1783")
public final class InProcessServerBuilder
    extends AbstractServerImplBuilder<InProcessServerBuilder> {
  /**
   * Create a server builder that will bind with the given name.
   *
   * @param name the identity of the server for clients to connect to
   * @return a new builder
   */
  public static InProcessServerBuilder forName(String name) {
    return new InProcessServerBuilder(name);
  }

  /**
   * Always fails.  Call {@link #forName} instead.
   */
  public static InProcessServerBuilder forPort(int port) {
    throw new UnsupportedOperationException("call forName() instead");
  }

  /**
   * Generates a new server name that is unique each time.
   */
  public static String generateName() {
    return UUID.randomUUID().toString();
  }

  private final String name;
  private ObjectPool<ScheduledExecutorService> schedulerPool =
      SharedResourcePool.forResource(GrpcUtil.TIMER_SERVICE);

  private InProcessServerBuilder(String name) {
    this.name = Preconditions.checkNotNull(name, "name");
    // In-process transport should not record its traffic to the stats module.
    // https://github.com/grpc/grpc-java/issues/2284
    setStatsRecordStartedRpcs(false);
    setStatsRecordFinishedRpcs(false);
    // Disable handshake timeout because it is unnecessary, and can trigger Thread creation that can
    // break some environments (like tests).
    handshakeTimeout(Long.MAX_VALUE, TimeUnit.SECONDS);
  }

  /**
   * Provides a custom scheduled executor service.
   *
   * <p>It's an optional parameter. If the user has not provided a scheduled executor service when
   * the channel is built, the builder will use a static cached thread pool.
   *
   * @return this
   *
   * @since 1.11.0
   */
  public InProcessServerBuilder scheduledExecutorService(
      ScheduledExecutorService scheduledExecutorService) {
    schedulerPool = new FixedObjectPool<ScheduledExecutorService>(
        checkNotNull(scheduledExecutorService, "scheduledExecutorService"));
    return this;
  }

  @Override
  protected InProcessServer buildTransportServer(
      List<ServerStreamTracer.Factory> streamTracerFactories) {
    return new InProcessServer(name, schedulerPool, streamTracerFactories);
  }

  @Override
  public InProcessServerBuilder useTransportSecurity(File certChain, File privateKey) {
    throw new UnsupportedOperationException("TLS not supported in InProcessServer");
  }
}
