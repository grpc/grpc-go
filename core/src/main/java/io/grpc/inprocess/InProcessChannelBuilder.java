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

import io.grpc.ExperimentalApi;
import io.grpc.Internal;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.ConnectionClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourceHolder;
import java.net.SocketAddress;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;

/**
 * Builder for a channel that issues in-process requests. Clients identify the in-process server by
 * its name.
 *
 * <p>The channel is intended to be fully-featured, high performance, and useful in testing.
 *
 * <p>For usage examples, see {@link InProcessServerBuilder}.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1783")
public final class InProcessChannelBuilder extends
        AbstractManagedChannelImplBuilder<InProcessChannelBuilder> {
  /**
   * Create a channel builder that will connect to the server with the given name.
   *
   * @param name the identity of the server to connect to
   * @return a new builder
   */
  public static InProcessChannelBuilder forName(String name) {
    return new InProcessChannelBuilder(name);
  }

  /**
   * Always fails.  Call {@link #forName} instead.
   */
  public static InProcessChannelBuilder forTarget(String target) {
    throw new UnsupportedOperationException("call forName() instead");
  }

  /**
   * Always fails.  Call {@link #forName} instead.
   */
  public static InProcessChannelBuilder forAddress(String name, int port) {
    throw new UnsupportedOperationException("call forName() instead");
  }

  private final String name;
  private ScheduledExecutorService scheduledExecutorService;

  private InProcessChannelBuilder(String name) {
    super(new InProcessSocketAddress(name), "localhost");
    this.name = checkNotNull(name, "name");
    // In-process transport should not record its traffic to the stats module.
    // https://github.com/grpc/grpc-java/issues/2284
    setStatsRecordStartedRpcs(false);
    setStatsRecordFinishedRpcs(false);
  }

  @Override
  public final InProcessChannelBuilder maxInboundMessageSize(int max) {
    // TODO(carl-mastrangelo): maybe throw an exception since this not enforced?
    return super.maxInboundMessageSize(max);
  }

  /**
   * Does nothing.
   */
  @Override
  public InProcessChannelBuilder useTransportSecurity() {
    return this;
  }

  /**
   * Does nothing.
   *
   * @deprecated use {@link #usePlaintext()} instead.
   */
  @Override
  @Deprecated
  public InProcessChannelBuilder usePlaintext(boolean skipNegotiation) {
    return this;
  }

  /**
   * Does nothing.
   */
  @Override
  public InProcessChannelBuilder usePlaintext() {
    return this;
  }

  /** Does nothing. */
  @Override
  public InProcessChannelBuilder keepAliveTime(long keepAliveTime, TimeUnit timeUnit) {
    return this;
  }

  /** Does nothing. */
  @Override
  public InProcessChannelBuilder keepAliveTimeout(long keepAliveTimeout, TimeUnit timeUnit) {
    return this;
  }

  /** Does nothing. */
  @Override
  public InProcessChannelBuilder keepAliveWithoutCalls(boolean enable) {
    return this;
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
  public InProcessChannelBuilder scheduledExecutorService(
      ScheduledExecutorService scheduledExecutorService) {
    this.scheduledExecutorService =
        checkNotNull(scheduledExecutorService, "scheduledExecutorService");
    return this;
  }

  @Override
  @Internal
  protected ClientTransportFactory buildTransportFactory() {
    return new InProcessClientTransportFactory(name, scheduledExecutorService);
  }

  /**
   * Creates InProcess transports. Exposed for internal use, as it should be private.
   */
  static final class InProcessClientTransportFactory implements ClientTransportFactory {
    private final String name;
    private final ScheduledExecutorService timerService;
    private final boolean useSharedTimer;
    private boolean closed;

    private InProcessClientTransportFactory(
        String name, @Nullable ScheduledExecutorService scheduledExecutorService) {
      this.name = name;
      useSharedTimer = scheduledExecutorService == null;
      timerService = useSharedTimer
          ? SharedResourceHolder.get(GrpcUtil.TIMER_SERVICE) : scheduledExecutorService;
    }

    @Override
    public ConnectionClientTransport newClientTransport(
        SocketAddress addr, ClientTransportOptions options) {
      if (closed) {
        throw new IllegalStateException("The transport factory is closed.");
      }
      return new InProcessTransport(name, options.getAuthority(), options.getUserAgent());
    }

    @Override
    public ScheduledExecutorService getScheduledExecutorService() {
      return timerService;
    }

    @Override
    public void close() {
      if (closed) {
        return;
      }
      closed = true;
      if (useSharedTimer) {
        SharedResourceHolder.release(GrpcUtil.TIMER_SERVICE, timerService);
      }
    }
  }
}
