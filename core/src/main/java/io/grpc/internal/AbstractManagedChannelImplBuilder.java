/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc.internal;

import static com.google.common.base.MoreObjects.firstNonNull;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.Attributes;
import io.grpc.ClientInterceptor;
import io.grpc.CompressorRegistry;
import io.grpc.DecompressorRegistry;
import io.grpc.ExperimentalApi;
import io.grpc.LoadBalancer;
import io.grpc.ManagedChannelBuilder;
import io.grpc.NameResolver;
import io.grpc.NameResolverRegistry;
import io.grpc.ResolvedServerInfo;
import io.grpc.SimpleLoadBalancerFactory;

import java.net.SocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.Executor;

import javax.annotation.Nullable;

/**
 * The base class for channel builders.
 *
 * @param <T> The concrete type of this builder.
 */
public abstract class AbstractManagedChannelImplBuilder
        <T extends AbstractManagedChannelImplBuilder<T>> extends ManagedChannelBuilder<T> {
  private static final String DIRECT_ADDRESS_SCHEME = "directaddress";

  @Nullable
  private Executor executor;
  private final List<ClientInterceptor> interceptors = new ArrayList<ClientInterceptor>();

  private final String target;

  @Nullable
  private final SocketAddress directServerAddress;

  @Nullable
  private String userAgent;

  @Nullable
  private String authorityOverride;

  @Nullable
  private NameResolver.Factory nameResolverFactory;

  @Nullable
  private LoadBalancer.Factory loadBalancerFactory;

  @Nullable
  private DecompressorRegistry decompressorRegistry;

  @Nullable
  private CompressorRegistry compressorRegistry;

  protected AbstractManagedChannelImplBuilder(String target) {
    this.target = Preconditions.checkNotNull(target);
    this.directServerAddress = null;
  }

  protected AbstractManagedChannelImplBuilder(SocketAddress directServerAddress, String authority) {
    this.target = DIRECT_ADDRESS_SCHEME + ":///" + directServerAddress;
    this.directServerAddress = directServerAddress;
    this.nameResolverFactory = new DirectAddressNameResolverFactory(directServerAddress, authority);
  }

  @Override
  public final T directExecutor() {
    return executor(MoreExecutors.directExecutor());
  }

  @Override
  public final T executor(Executor executor) {
    this.executor = executor;
    return thisT();
  }

  @Override
  public final T intercept(List<ClientInterceptor> interceptors) {
    this.interceptors.addAll(interceptors);
    return thisT();
  }

  @Override
  public final T intercept(ClientInterceptor... interceptors) {
    return intercept(Arrays.asList(interceptors));
  }

  @Override
  public final T nameResolverFactory(NameResolver.Factory resolverFactory) {
    Preconditions.checkState(directServerAddress == null,
        "directServerAddress is set (%s), which forbids the use of NameResolverFactory",
        directServerAddress);
    this.nameResolverFactory = resolverFactory;
    return thisT();
  }

  @Override
  public final T loadBalancerFactory(LoadBalancer.Factory loadBalancerFactory) {
    Preconditions.checkState(directServerAddress == null,
        "directServerAddress is set (%s), which forbids the use of LoadBalancerFactory",
        directServerAddress);
    this.loadBalancerFactory = loadBalancerFactory;
    return thisT();
  }

  @Override
  @ExperimentalApi
  public final T decompressorRegistry(DecompressorRegistry registry) {
    this.decompressorRegistry = registry;
    return thisT();
  }

  @Override
  @ExperimentalApi
  public final T compressorRegistry(CompressorRegistry registry) {
    this.compressorRegistry = registry;
    return thisT();
  }

  private T thisT() {
    @SuppressWarnings("unchecked")
    T thisT = (T) this;
    return thisT;
  }

  @Override
  public final T userAgent(String userAgent) {
    this.userAgent = userAgent;
    return thisT();
  }

  @Override
  public final T overrideAuthority(String authority) {
    this.authorityOverride = checkAuthority(authority);
    return thisT();
  }

  /**
   * Verifies the authority is valid.  This method exists as an escape hatch for putting in an
   * authority that is valid, but would fail the default validation provided by this
   * implementation.
   */
  protected String checkAuthority(String authority) {
    return GrpcUtil.checkAuthority(authority);
  }

  @Override
  public ManagedChannelImpl build() {
    ClientTransportFactory transportFactory = buildTransportFactory();
    if (authorityOverride != null) {
      transportFactory = new AuthorityOverridingTransportFactory(
        transportFactory, authorityOverride);
    }
    return new ManagedChannelImpl(
        target,
        // TODO(carl-mastrangelo): Allow clients to pass this in
        new ExponentialBackoffPolicy.Provider(),
        firstNonNull(nameResolverFactory, NameResolverRegistry.getDefaultRegistry()),
        getNameResolverParams(),
        firstNonNull(loadBalancerFactory, SimpleLoadBalancerFactory.getInstance()),
        transportFactory,
        firstNonNull(decompressorRegistry, DecompressorRegistry.getDefaultInstance()),
        firstNonNull(compressorRegistry, CompressorRegistry.getDefaultInstance()),
        executor, userAgent, interceptors);
  }

  /**
   * Subclasses should override this method to provide the {@link ClientTransportFactory}
   * appropriate for this channel. This method is meant for Transport implementors and should not
   * be used by normal users.
   */
  protected abstract ClientTransportFactory buildTransportFactory();

  /**
   * Subclasses can override this method to provide additional parameters to {@link
   * NameResolver.Factory#newNameResolver}. The default implementation returns {@link
   * Attributes#EMPTY}.
   */
  protected Attributes getNameResolverParams() {
    return Attributes.EMPTY;
  }

  private static class AuthorityOverridingTransportFactory implements ClientTransportFactory {
    final ClientTransportFactory factory;
    final String authorityOverride;

    AuthorityOverridingTransportFactory(
        ClientTransportFactory factory, String authorityOverride) {
      this.factory = Preconditions.checkNotNull(factory, "factory should not be null");
      this.authorityOverride = Preconditions.checkNotNull(
        authorityOverride, "authorityOverride should not be null");
    }

    @Override
    public ManagedClientTransport newClientTransport(SocketAddress serverAddress,
        String authority) {
      return factory.newClientTransport(serverAddress, authorityOverride);
    }

    @Override
    public void close() {
      factory.close();
    }
  }

  private static class DirectAddressNameResolverFactory extends NameResolver.Factory {
    final SocketAddress address;
    final String authority;

    DirectAddressNameResolverFactory(SocketAddress address, String authority) {
      this.address = address;
      this.authority = authority;
    }

    @Override
    public NameResolver newNameResolver(URI notUsedUri, Attributes params) {
      return new NameResolver() {
        @Override
        public String getServiceAuthority() {
          return authority;
        }

        @Override
        public void start(final Listener listener) {
          listener.onUpdate(
              Collections.singletonList(new ResolvedServerInfo(address, Attributes.EMPTY)),
              Attributes.EMPTY);
        }

        @Override
        public void shutdown() {}
      };
    }

    @Override
    public String getDefaultScheme() {
      return DIRECT_ADDRESS_SCHEME;
    }
  }
}
