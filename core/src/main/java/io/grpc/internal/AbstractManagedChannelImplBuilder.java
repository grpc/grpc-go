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
import static com.google.common.base.Preconditions.checkArgument;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.instrumentation.stats.Stats;
import com.google.instrumentation.stats.StatsContextFactory;
import io.grpc.Attributes;
import io.grpc.ClientInterceptor;
import io.grpc.CompressorRegistry;
import io.grpc.DecompressorRegistry;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.NameResolver;
import io.grpc.NameResolverProvider;
import io.grpc.PickFirstBalancerFactory;
import java.net.SocketAddress;
import java.net.URI;
import java.net.URISyntaxException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;

/**
 * The base class for channel builders.
 *
 * @param <T> The concrete type of this builder.
 */
public abstract class AbstractManagedChannelImplBuilder
        <T extends AbstractManagedChannelImplBuilder<T>> extends ManagedChannelBuilder<T> {
  private static final String DIRECT_ADDRESS_SCHEME = "directaddress";

  /**
   * An idle timeout larger than this would disable idle mode.
   */
  @VisibleForTesting
  static final long IDLE_MODE_MAX_TIMEOUT_DAYS = 30;

  /**
   * The default idle timeout.
   */
  @VisibleForTesting
  static final long IDLE_MODE_DEFAULT_TIMEOUT_MILLIS = TimeUnit.MINUTES.toMillis(30);

  /**
   * An idle timeout smaller than this would be capped to it.
   */
  @VisibleForTesting
  static final long IDLE_MODE_MIN_TIMEOUT_MILLIS = TimeUnit.SECONDS.toMillis(1);

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

  private LoadBalancer.Factory loadBalancerFactory;

  @Nullable
  private DecompressorRegistry decompressorRegistry;

  @Nullable
  private CompressorRegistry compressorRegistry;

  private long idleTimeoutMillis = IDLE_MODE_DEFAULT_TIMEOUT_MILLIS;

  private int maxInboundMessageSize = GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;

  // Can be overriden by subclasses.
  @Override
  public T maxInboundMessageSize(int max) {
    checkArgument(max >= 0, "negative max");
    maxInboundMessageSize = max;
    return thisT();
  }

  protected final int maxInboundMessageSize() {
    return maxInboundMessageSize;
  }

  @Nullable
  private StatsContextFactory statsFactory;

  protected AbstractManagedChannelImplBuilder(String target) {
    this.target = Preconditions.checkNotNull(target, "target");
    this.directServerAddress = null;
  }

  /**
   * Returns a target string for the SocketAddress. It is only used as a placeholder, because
   * DirectAddressNameResolverFactory will not actually try to use it. However, it must be a valid
   * URI.
   */
  @VisibleForTesting
  static String makeTargetStringForDirectAddress(SocketAddress address) {
    try {
      return new URI(DIRECT_ADDRESS_SCHEME, "", "/" + address, null).toString();
    } catch (URISyntaxException e) {
      // It should not happen.
      throw new RuntimeException(e);
    }
  }

  protected AbstractManagedChannelImplBuilder(SocketAddress directServerAddress, String authority) {
    this.target = makeTargetStringForDirectAddress(directServerAddress);
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
        "directServerAddress is set (%s), which forbids the use of LoadBalancer.Factory",
        directServerAddress);
    this.loadBalancerFactory = loadBalancerFactory;
    return thisT();
  }

  @Override
  public final T decompressorRegistry(DecompressorRegistry registry) {
    this.decompressorRegistry = registry;
    return thisT();
  }

  @Override
  public final T compressorRegistry(CompressorRegistry registry) {
    this.compressorRegistry = registry;
    return thisT();
  }

  @Override
  public final T userAgent(@Nullable String userAgent) {
    this.userAgent = userAgent;
    return thisT();
  }

  @Override
  public final T overrideAuthority(String authority) {
    this.authorityOverride = checkAuthority(authority);
    return thisT();
  }

  @Override
  public final T idleTimeout(long value, TimeUnit unit) {
    checkArgument(value > 0, "idle timeout is %s, but must be positive", value);
    // We convert to the largest unit to avoid overflow
    if (unit.toDays(value) >= IDLE_MODE_MAX_TIMEOUT_DAYS) {
      // This disables idle mode
      this.idleTimeoutMillis = ManagedChannelImpl.IDLE_TIMEOUT_MILLIS_DISABLE;
    } else {
      this.idleTimeoutMillis = Math.max(unit.toMillis(value), IDLE_MODE_MIN_TIMEOUT_MILLIS);
    }
    return thisT();
  }

  /**
   * Override the default stats implementation.
   */
  @VisibleForTesting
  protected T statsContextFactory(StatsContextFactory statsFactory) {
    this.statsFactory = statsFactory;
    return thisT();
  }

  @VisibleForTesting
  final long getIdleTimeoutMillis() {
    return idleTimeoutMillis;
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
  public ManagedChannel build() {
    ClientTransportFactory transportFactory = buildTransportFactory();
    if (authorityOverride != null) {
      transportFactory = new AuthorityOverridingTransportFactory(
        transportFactory, authorityOverride);
    }
    NameResolver.Factory nameResolverFactory = this.nameResolverFactory;
    if (nameResolverFactory == null) {
      // Avoid loading the provider unless necessary, as a way to workaround a possibly-costly
      // and poorly optimized getResource() call on Android. If any other piece of code calls
      // getResource(), then this shouldn't be a problem unless called on the UI thread.
      nameResolverFactory = NameResolverProvider.asFactory();
    }
    return new ManagedChannelImpl(
        target,
        // TODO(carl-mastrangelo): Allow clients to pass this in
        new ExponentialBackoffPolicy.Provider(),
        nameResolverFactory,
        getNameResolverParams(),
        firstNonNull(loadBalancerFactory, PickFirstBalancerFactory.getInstance()),
        transportFactory,
        firstNonNull(decompressorRegistry, DecompressorRegistry.getDefaultInstance()),
        firstNonNull(compressorRegistry, CompressorRegistry.getDefaultInstance()),
        SharedResourcePool.forResource(GrpcUtil.TIMER_SERVICE),
        getExecutorPool(executor),
        SharedResourcePool.forResource(GrpcUtil.SHARED_CHANNEL_EXECUTOR),
        GrpcUtil.STOPWATCH_SUPPLIER,
        idleTimeoutMillis,
        userAgent,
        interceptors,
        firstNonNull(
            statsFactory,
            firstNonNull(Stats.getStatsContextFactory(), NoopStatsContextFactory.INSTANCE)));
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

  private static ObjectPool<? extends Executor> getExecutorPool(final @Nullable Executor executor) {
    if (executor != null) {
      return new ObjectPool<Executor>() {
        @Override
        public Executor getObject() {
          return executor;
        }

        @Override
        public Executor returnObject(Object returned) {
          return null;
        }
      };
    } else {
      return SharedResourcePool.forResource(GrpcUtil.SHARED_CHANNEL_EXECUTOR);
    }
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
    public ConnectionClientTransport newClientTransport(SocketAddress serverAddress,
        String authority, @Nullable String userAgent) {
      return factory.newClientTransport(serverAddress, authorityOverride, userAgent);
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
          listener.onAddresses(
              Collections.singletonList(new EquivalentAddressGroup(address)),
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

  /**
   * Returns the correctly typed version of the builder.
   */
  private T thisT() {
    @SuppressWarnings("unchecked")
    T thisT = (T) this;
    return thisT;
  }
}
