/*
 * Copyright 2014, gRPC Authors All rights reserved.
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

package io.grpc.internal;

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
import io.opencensus.trace.Tracing;
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

  public static ManagedChannelBuilder<?> forAddress(String name, int port) {
    throw new UnsupportedOperationException("Subclass failed to hide static factory");
  }

  public static ManagedChannelBuilder<?> forTarget(String target) {
    throw new UnsupportedOperationException("Subclass failed to hide static factory");
  }

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

  private static final ObjectPool<? extends Executor> DEFAULT_EXECUTOR_POOL =
      SharedResourcePool.forResource(GrpcUtil.SHARED_CHANNEL_EXECUTOR);

  private static final NameResolver.Factory DEFAULT_NAME_RESOLVER_FACTORY =
      NameResolverProvider.asFactory();

  private static final LoadBalancer.Factory DEFAULT_LOAD_BALANCER_FACTORY =
      PickFirstBalancerFactory.getInstance();

  private static final DecompressorRegistry DEFAULT_DECOMPRESSOR_REGISTRY =
      DecompressorRegistry.getDefaultInstance();

  private static final CompressorRegistry DEFAULT_COMPRESSOR_REGISTRY =
      CompressorRegistry.getDefaultInstance();

  ObjectPool<? extends Executor> executorPool = DEFAULT_EXECUTOR_POOL;

  private final List<ClientInterceptor> interceptors = new ArrayList<ClientInterceptor>();

  // Access via getter, which may perform authority override as needed
  private NameResolver.Factory nameResolverFactory = DEFAULT_NAME_RESOLVER_FACTORY;

  final String target;

  @Nullable
  private final SocketAddress directServerAddress;

  @Nullable
  String userAgent;

  @VisibleForTesting
  @Nullable
  String authorityOverride;


  private ProxyDetector proxyDetector = ProxyDetector.DEFAULT_INSTANCE;

  LoadBalancer.Factory loadBalancerFactory = DEFAULT_LOAD_BALANCER_FACTORY;

  boolean fullStreamDecompression;

  DecompressorRegistry decompressorRegistry = DEFAULT_DECOMPRESSOR_REGISTRY;

  CompressorRegistry compressorRegistry = DEFAULT_COMPRESSOR_REGISTRY;

  long idleTimeoutMillis = IDLE_MODE_DEFAULT_TIMEOUT_MILLIS;

  private int maxInboundMessageSize = GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;

  /**
   * Sets the maximum message size allowed for a single gRPC frame. If an inbound messages
   * larger than this limit is received it will not be processed and the RPC will fail with
   * RESOURCE_EXHAUSTED.
   */
  // Can be overridden by subclasses.
  @Override
  public T maxInboundMessageSize(int max) {
    checkArgument(max >= 0, "negative max");
    maxInboundMessageSize = max;
    return thisT();
  }

  protected final int maxInboundMessageSize() {
    return maxInboundMessageSize;
  }

  private boolean statsEnabled = true;
  private boolean recordStats = true;
  private boolean tracingEnabled = true;

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
    if (executor != null) {
      this.executorPool = new FixedObjectPool<Executor>(executor);
    } else {
      this.executorPool = DEFAULT_EXECUTOR_POOL;
    }
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
    if (resolverFactory != null) {
      this.nameResolverFactory = resolverFactory;
    } else {
      this.nameResolverFactory = DEFAULT_NAME_RESOLVER_FACTORY;
    }
    return thisT();
  }

  @Override
  public final T loadBalancerFactory(LoadBalancer.Factory loadBalancerFactory) {
    Preconditions.checkState(directServerAddress == null,
        "directServerAddress is set (%s), which forbids the use of LoadBalancer.Factory",
        directServerAddress);
    if (loadBalancerFactory != null) {
      this.loadBalancerFactory = loadBalancerFactory;
    } else {
      this.loadBalancerFactory = DEFAULT_LOAD_BALANCER_FACTORY;
    }
    return thisT();
  }

  @Override
  public final T enableFullStreamDecompression() {
    this.fullStreamDecompression = true;
    return thisT();
  }

  @Override
  public final T decompressorRegistry(DecompressorRegistry registry) {
    if (registry != null) {
      this.decompressorRegistry = registry;
    } else {
      this.decompressorRegistry = DEFAULT_DECOMPRESSOR_REGISTRY;
    } 
    return thisT();
  }

  @Override
  public final T compressorRegistry(CompressorRegistry registry) {
    if (registry != null) {
      this.compressorRegistry = registry;
    } else {
      this.compressorRegistry = DEFAULT_COMPRESSOR_REGISTRY;
    }
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
  protected final T statsContextFactory(StatsContextFactory statsFactory) {
    this.statsFactory = statsFactory;
    return thisT();
  }

  /**
   * Disable or enable stats features.  Enabled by default.
   */
  protected void setStatsEnabled(boolean value) {
    statsEnabled = value;
  }

  /**
   * Disable or enable stats recording.  Effective only if {@link #setStatsEnabled} is set to true.
   * Enabled by default.
   */
  protected void setRecordStats(boolean value) {
    recordStats = value;
  }

  /**
   * Disable or enable tracing features.  Enabled by default.
   */
  protected void setTracingEnabled(boolean value) {
    tracingEnabled = value;
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
    return new ManagedChannelImpl(
        this,
        buildTransportFactory(),
        // TODO(carl-mastrangelo): Allow clients to pass this in
        new ExponentialBackoffPolicy.Provider(),
        SharedResourcePool.forResource(GrpcUtil.SHARED_CHANNEL_EXECUTOR),
        GrpcUtil.STOPWATCH_SUPPLIER,
        getEffectiveInterceptors(),
        proxyDetector);
  }

  @VisibleForTesting
  final List<ClientInterceptor> getEffectiveInterceptors() {
    List<ClientInterceptor> effectiveInterceptors =
        new ArrayList<ClientInterceptor>(this.interceptors);
    if (statsEnabled) {
      StatsContextFactory statsCtxFactory =
          this.statsFactory != null ? this.statsFactory : Stats.getStatsContextFactory();
      if (statsCtxFactory != null) {
        CensusStatsModule censusStats =
            new CensusStatsModule(statsCtxFactory, GrpcUtil.STOPWATCH_SUPPLIER, true, recordStats);
        // First interceptor runs last (see ClientInterceptors.intercept()), so that no
        // other interceptor can override the tracer factory we set in CallOptions.
        effectiveInterceptors.add(0, censusStats.getClientInterceptor());
      }
    }
    if (tracingEnabled) {
      CensusTracingModule censusTracing =
          new CensusTracingModule(Tracing.getTracer(),
              Tracing.getPropagationComponent().getBinaryFormat());
      effectiveInterceptors.add(0, censusTracing.getClientInterceptor());
    }
    return effectiveInterceptors;
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

  /**
   * Returns a {@link NameResolver.Factory} for the channel.
   */
  NameResolver.Factory getNameResolverFactory() {
    if (authorityOverride == null) {
      return nameResolverFactory;
    } else {
      return new OverrideAuthorityNameResolverFactory(nameResolverFactory, authorityOverride);
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
