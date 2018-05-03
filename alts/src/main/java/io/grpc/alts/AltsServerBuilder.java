/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts;

import com.google.common.base.MoreObjects;
import io.grpc.BindableService;
import io.grpc.CompressorRegistry;
import io.grpc.DecompressorRegistry;
import io.grpc.ExperimentalApi;
import io.grpc.HandlerRegistry;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.ServerInterceptor;
import io.grpc.ServerServiceDefinition;
import io.grpc.ServerStreamTracer.Factory;
import io.grpc.ServerTransportFilter;
import io.grpc.alts.internal.AltsHandshakerOptions;
import io.grpc.alts.internal.AltsProtocolNegotiator;
import io.grpc.alts.internal.AltsTsiHandshaker;
import io.grpc.alts.internal.HandshakerServiceGrpc;
import io.grpc.alts.internal.RpcProtocolVersionsUtil;
import io.grpc.alts.internal.TsiHandshaker;
import io.grpc.alts.internal.TsiHandshakerFactory;
import io.grpc.netty.NettyServerBuilder;
import java.io.File;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.util.List;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;

/**
 * gRPC secure server builder used for ALTS. This class adds on the necessary ALTS support to create
 * a production server on Google Cloud Platform.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4151")
public final class AltsServerBuilder extends ServerBuilder<AltsServerBuilder> {

  private final NettyServerBuilder delegate;
  private boolean enableUntrustedAlts;

  private AltsServerBuilder(NettyServerBuilder nettyDelegate) {
    this.delegate = nettyDelegate;
  }

  /** Creates a gRPC server builder for the given port. */
  public static AltsServerBuilder forPort(int port) {
    NettyServerBuilder nettyDelegate =
        NettyServerBuilder.forAddress(new InetSocketAddress(port))
            .maxConnectionIdle(1, TimeUnit.HOURS)
            .keepAliveTime(270, TimeUnit.SECONDS)
            .keepAliveTimeout(20, TimeUnit.SECONDS)
            .permitKeepAliveTime(10, TimeUnit.SECONDS)
            .permitKeepAliveWithoutCalls(true);
    return new AltsServerBuilder(nettyDelegate);
  }

  /**
   * Enables untrusted ALTS for testing. If this function is called, we will not check whether ALTS
   * is running on Google Cloud Platform.
   */
  public AltsServerBuilder enableUntrustedAltsForTesting() {
    enableUntrustedAlts = true;
    return this;
  }

  /** Sets a new handshaker service address for testing. */
  public AltsServerBuilder setHandshakerAddressForTesting(String handshakerAddress) {
    HandshakerServiceChannel.setHandshakerAddressForTesting(handshakerAddress);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder handshakeTimeout(long timeout, TimeUnit unit) {
    delegate.handshakeTimeout(timeout, unit);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder directExecutor() {
    delegate.directExecutor();
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder addStreamTracerFactory(Factory factory) {
    delegate.addStreamTracerFactory(factory);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder addTransportFilter(ServerTransportFilter filter) {
    delegate.addTransportFilter(filter);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder executor(Executor executor) {
    delegate.executor(executor);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder addService(ServerServiceDefinition service) {
    delegate.addService(service);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder addService(BindableService bindableService) {
    delegate.addService(bindableService);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder fallbackHandlerRegistry(HandlerRegistry fallbackRegistry) {
    delegate.fallbackHandlerRegistry(fallbackRegistry);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder useTransportSecurity(File certChain, File privateKey) {
    throw new UnsupportedOperationException("Can't set TLS settings for ALTS");
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder decompressorRegistry(DecompressorRegistry registry) {
    delegate.decompressorRegistry(registry);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder compressorRegistry(CompressorRegistry registry) {
    delegate.compressorRegistry(registry);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public AltsServerBuilder intercept(ServerInterceptor interceptor) {
    delegate.intercept(interceptor);
    return this;
  }

  /** {@inheritDoc} */
  @Override
  public Server build() {
    CheckGcpEnvironment.check(enableUntrustedAlts);
    delegate.protocolNegotiator(
        AltsProtocolNegotiator.create(
            new TsiHandshakerFactory() {
              @Override
              public TsiHandshaker newHandshaker() {
                // Used the shared grpc channel to connecting to the ALTS handshaker service.
                return AltsTsiHandshaker.newServer(
                    HandshakerServiceGrpc.newStub(HandshakerServiceChannel.get()),
                    new AltsHandshakerOptions(RpcProtocolVersionsUtil.getRpcProtocolVersions()));
              }
            }));
    return new AltsServer(delegate.build());
  }

  static final class AltsServer extends io.grpc.Server {
    private final Server delegate;

    AltsServer(Server delegate) {
      this.delegate = delegate;
    }

    @Override
    public List<ServerServiceDefinition> getImmutableServices() {
      return delegate.getImmutableServices();
    }

    @Override
    public List<ServerServiceDefinition> getMutableServices() {
      return delegate.getMutableServices();
    }

    @Override
    public int getPort() {
      return delegate.getPort();
    }

    @Override
    public List<ServerServiceDefinition> getServices() {
      return delegate.getServices();
    }

    @Override
    public Server start() throws IOException {
      delegate.start();
      return this;
    }

    @Override
    public Server shutdown() {
      delegate.shutdown();
      return this;
    }

    @Override
    public Server shutdownNow() {
      delegate.shutdownNow();
      return this;
    }

    @Override
    public boolean isShutdown() {
      return delegate.isShutdown();
    }

    @Override
    public boolean isTerminated() {
      return delegate.isTerminated();
    }

    @Override
    public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException {
      return delegate.awaitTermination(timeout, unit);
    }

    @Override
    public void awaitTermination() throws InterruptedException {
      delegate.awaitTermination();
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this).add("delegate", delegate).toString();
    }
  }
}
