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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.SHUTDOWN;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;
import static io.grpc.internal.ServiceConfigInterceptor.HEDGING_POLICY_KEY;
import static io.grpc.internal.ServiceConfigInterceptor.RETRY_POLICY_KEY;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.MoreObjects;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors;
import io.grpc.ClientStreamTracer;
import io.grpc.CompressorRegistry;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.Context;
import io.grpc.DecompressorRegistry;
import io.grpc.EquivalentAddressGroup;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.ChannelTrace;
import io.grpc.InternalInstrumented;
import io.grpc.InternalLogId;
import io.grpc.InternalWithLogId;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.NameResolver;
import io.grpc.Status;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;
import io.grpc.internal.RetriableStream.ChannelBufferMeter;
import io.grpc.internal.RetriableStream.Throttle;
import java.net.URI;
import java.net.URISyntaxException;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.logging.Level;
import java.util.logging.Logger;
import java.util.regex.Pattern;
import javax.annotation.CheckForNull;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/** A communication channel for making outgoing RPCs. */
@ThreadSafe
final class ManagedChannelImpl extends ManagedChannel implements
    InternalInstrumented<ChannelStats> {
  static final Logger logger = Logger.getLogger(ManagedChannelImpl.class.getName());

  // Matching this pattern means the target string is a URI target or at least intended to be one.
  // A URI target must be an absolute hierarchical URI.
  // From RFC 2396: scheme = alpha *( alpha | digit | "+" | "-" | "." )
  @VisibleForTesting
  static final Pattern URI_PATTERN = Pattern.compile("[a-zA-Z][a-zA-Z0-9+.-]*:/.*");

  static final long IDLE_TIMEOUT_MILLIS_DISABLE = -1;

  @VisibleForTesting
  static final long SUBCHANNEL_SHUTDOWN_DELAY_SECONDS = 5;

  @VisibleForTesting
  static final Status SHUTDOWN_NOW_STATUS =
      Status.UNAVAILABLE.withDescription("Channel shutdownNow invoked");

  @VisibleForTesting
  static final Status SHUTDOWN_STATUS =
      Status.UNAVAILABLE.withDescription("Channel shutdown invoked");

  @VisibleForTesting
  static final Status SUBCHANNEL_SHUTDOWN_STATUS =
      Status.UNAVAILABLE.withDescription("Subchannel shutdown invoked");

  private final InternalLogId logId = InternalLogId.allocate(getClass().getName());
  private final String target;
  private final NameResolver.Factory nameResolverFactory;
  private final Attributes nameResolverParams;
  private final LoadBalancer.Factory loadBalancerFactory;
  private final ClientTransportFactory transportFactory;
  private final Executor executor;
  private final ObjectPool<? extends Executor> executorPool;
  private final ObjectPool<? extends Executor> oobExecutorPool;
  private final TimeProvider timeProvider;
  private final int maxTraceEvents;

  private final ChannelExecutor channelExecutor = new ChannelExecutor() {
      @Override
      void handleUncaughtThrowable(Throwable t) {
        super.handleUncaughtThrowable(t);
        panic(t);
      }
    };

  private boolean fullStreamDecompression;

  private final DecompressorRegistry decompressorRegistry;
  private final CompressorRegistry compressorRegistry;

  private final Supplier<Stopwatch> stopwatchSupplier;
  /** The timout before entering idle mode. */
  private final long idleTimeoutMillis;

  private final ConnectivityStateManager channelStateManager = new ConnectivityStateManager();

  private final ServiceConfigInterceptor serviceConfigInterceptor;

  private final BackoffPolicy.Provider backoffPolicyProvider;

  /**
   * We delegate to this channel, so that we can have interceptors as necessary. If there aren't
   * any interceptors and the {@link io.grpc.BinaryLog} is {@code null} then this will just be a
   * {@link RealChannel}.
   */
  private final Channel interceptorChannel;
  @Nullable private final String userAgent;

  // Only null after channel is terminated. Must be assigned from the channelExecutor.
  private NameResolver nameResolver;

  // Must be accessed from the channelExecutor.
  private boolean nameResolverStarted;

  // null when channel is in idle mode.  Must be assigned from channelExecutor.
  @Nullable
  private LbHelperImpl lbHelper;

  // Must ONLY be assigned from updateSubchannelPicker(), which is called from channelExecutor.
  // null if channel is in idle mode.
  @Nullable
  private volatile SubchannelPicker subchannelPicker;

  // Must be accessed from the channelExecutor
  private boolean panicMode;

  // Must be mutated from channelExecutor
  // If any monitoring hook to be added later needs to get a snapshot of this Set, we could
  // switch to a ConcurrentHashMap.
  private final Set<InternalSubchannel> subchannels = new HashSet<InternalSubchannel>(16, .75f);

  // Must be mutated from channelExecutor
  private final Set<OobChannel> oobChannels = new HashSet<OobChannel>(1, .75f);

  // reprocess() must be run from channelExecutor
  private final DelayedClientTransport delayedTransport;
  private final UncommittedRetriableStreamsRegistry uncommittedRetriableStreamsRegistry
      = new UncommittedRetriableStreamsRegistry();

  // Shutdown states.
  //
  // Channel's shutdown process:
  // 1. shutdown(): stop accepting new calls from applications
  //   1a shutdown <- true
  //   1b subchannelPicker <- null
  //   1c delayedTransport.shutdown()
  // 2. delayedTransport terminated: stop stream-creation functionality
  //   2a terminating <- true
  //   2b loadBalancer.shutdown()
  //     * LoadBalancer will shutdown subchannels and OOB channels
  //   2c loadBalancer <- null
  //   2d nameResolver.shutdown()
  //   2e nameResolver <- null
  // 3. All subchannels and OOB channels terminated: Channel considered terminated

  private final AtomicBoolean shutdown = new AtomicBoolean(false);
  // Must only be mutated and read from channelExecutor
  private boolean shutdownNowed;
  // Must be mutated from channelExecutor
  private volatile boolean terminating;
  // Must be mutated from channelExecutor
  private volatile boolean terminated;
  private final CountDownLatch terminatedLatch = new CountDownLatch(1);

  private final CallTracer.Factory callTracerFactory;
  private final CallTracer channelCallTracer;
  @CheckForNull
  private final ChannelTracer channelTracer;
  private final InternalChannelz channelz;
  @CheckForNull
  private Boolean haveBackends; // a flag for doing channel tracing when flipped
  @Nullable
  private Map<String, Object> lastServiceConfig; // used for channel tracing when value changed

  // One instance per channel.
  private final ChannelBufferMeter channelBufferUsed = new ChannelBufferMeter();

  @Nullable
  private Throttle throttle;

  private final long perRpcBufferLimit;
  private final long channelBufferLimit;

  // Temporary false flag that can skip the retry code path.
  private final boolean retryEnabled;

  // Called from channelExecutor
  private final ManagedClientTransport.Listener delayedTransportListener =
      new ManagedClientTransport.Listener() {
        @Override
        public void transportShutdown(Status s) {
          checkState(shutdown.get(), "Channel must have been shut down");
        }

        @Override
        public void transportReady() {
          // Don't care
        }

        @Override
        public void transportInUse(final boolean inUse) {
          inUseStateAggregator.updateObjectInUse(delayedTransport, inUse);
        }

        @Override
        public void transportTerminated() {
          checkState(shutdown.get(), "Channel must have been shut down");
          terminating = true;
          shutdownNameResolverAndLoadBalancer(false);
          // No need to call channelStateManager since we are already in SHUTDOWN state.
          // Until LoadBalancer is shutdown, it may still create new subchannels.  We catch them
          // here.
          maybeShutdownNowSubchannels();
          maybeTerminateChannel();
        }
      };

  // Must be called from channelExecutor
  private void maybeShutdownNowSubchannels() {
    if (shutdownNowed) {
      for (InternalSubchannel subchannel : subchannels) {
        subchannel.shutdownNow(SHUTDOWN_NOW_STATUS);
      }
      for (OobChannel oobChannel : oobChannels) {
        oobChannel.getInternalSubchannel().shutdownNow(SHUTDOWN_NOW_STATUS);
      }
    }
  }

  // Must be accessed from channelExecutor
  @VisibleForTesting
  final InUseStateAggregator<Object> inUseStateAggregator =
      new InUseStateAggregator<Object>() {
        @Override
        void handleInUse() {
          exitIdleMode();
        }

        @Override
        void handleNotInUse() {
          if (shutdown.get()) {
            return;
          }
          rescheduleIdleTimer();
        }
      };

  @Override
  public ListenableFuture<ChannelStats> getStats() {
    final SettableFuture<ChannelStats> ret = SettableFuture.create();
    // subchannels and oobchannels can only be accessed from channelExecutor
    channelExecutor.executeLater(new Runnable() {
      @Override
      public void run() {
        ChannelStats.Builder builder = new InternalChannelz.ChannelStats.Builder();
        channelCallTracer.updateBuilder(builder);
        if (channelTracer != null) {
          channelTracer.updateBuilder(builder);
        }
        builder.setTarget(target).setState(channelStateManager.getState());
        List<InternalWithLogId> children = new ArrayList<>();
        children.addAll(subchannels);
        children.addAll(oobChannels);
        builder.setSubchannels(children);
        ret.set(builder.build());
      }
    }).drain();
    return ret;
  }

  @Override
  public InternalLogId getLogId() {
    return logId;
  }

  // Run from channelExecutor
  private class IdleModeTimer implements Runnable {

    @Override
    public void run() {
      enterIdleMode();
    }
  }

  // Must be called from channelExecutor
  private void shutdownNameResolverAndLoadBalancer(boolean verifyActive) {
    if (verifyActive) {
      checkState(nameResolver != null, "nameResolver is null");
      checkState(lbHelper != null, "lbHelper is null");
    }
    if (nameResolver != null) {
      cancelNameResolverBackoff();
      nameResolver.shutdown();
      nameResolver = null;
      nameResolverStarted = false;
    }
    if (lbHelper != null) {
      lbHelper.lb.shutdown();
      lbHelper = null;
    }
    subchannelPicker = null;
  }

  /**
   * Make the channel exit idle mode, if it's in it.
   *
   * <p>Must be called from channelExecutor
   */
  @VisibleForTesting
  void exitIdleMode() {
    if (shutdown.get() || panicMode) {
      return;
    }
    if (inUseStateAggregator.isInUse()) {
      // Cancel the timer now, so that a racing due timer will not put Channel on idleness
      // when the caller of exitIdleMode() is about to use the returned loadBalancer.
      cancelIdleTimer(false);
    } else {
      // exitIdleMode() may be called outside of inUseStateAggregator.handleNotInUse() while
      // isInUse() == false, in which case we still need to schedule the timer.
      rescheduleIdleTimer();
    }
    if (lbHelper != null) {
      return;
    }
    logger.log(Level.FINE, "[{0}] Exiting idle mode", getLogId());
    lbHelper = new LbHelperImpl(nameResolver);
    lbHelper.lb = loadBalancerFactory.newLoadBalancer(lbHelper);

    NameResolverListenerImpl listener = new NameResolverListenerImpl(lbHelper);
    try {
      nameResolver.start(listener);
      nameResolverStarted = true;
    } catch (Throwable t) {
      listener.onError(Status.fromThrowable(t));
    }
  }

  // Must be run from channelExecutor
  private void enterIdleMode() {
    logger.log(Level.FINE, "[{0}] Entering idle mode", getLogId());
    // nameResolver and loadBalancer are guaranteed to be non-null.  If any of them were null,
    // either the idleModeTimer ran twice without exiting the idle mode, or the task in shutdown()
    // did not cancel idleModeTimer, or enterIdle() ran while shutdown or in idle, all of
    // which are bugs.
    shutdownNameResolverAndLoadBalancer(true);
    delayedTransport.reprocess(null);
    nameResolver = getNameResolver(target, nameResolverFactory, nameResolverParams);
    if (channelTracer != null) {
      channelTracer.reportEvent(
          new ChannelTrace.Event.Builder()
              .setDescription("Entering IDLE state")
              .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
              .setTimestampNanos(timeProvider.currentTimeNanos())
              .build());
    }
    channelStateManager.gotoState(IDLE);
    if (inUseStateAggregator.isInUse()) {
      exitIdleMode();
    }
  }

  // Must be run from channelExecutor
  private void cancelIdleTimer(boolean permanent) {
    idleTimer.cancel(permanent);
  }

  // Always run from channelExecutor
  private void rescheduleIdleTimer() {
    if (idleTimeoutMillis == IDLE_TIMEOUT_MILLIS_DISABLE) {
      return;
    }
    idleTimer.reschedule(idleTimeoutMillis, TimeUnit.MILLISECONDS);
  }

  // Run from channelExecutor
  @VisibleForTesting
  class NameResolverRefresh implements Runnable {
    // Only mutated from channelExecutor
    boolean cancelled;

    @Override
    public void run() {
      if (cancelled) {
        // Race detected: this task was scheduled on channelExecutor before
        // cancelNameResolverBackoff() could cancel the timer.
        return;
      }
      nameResolverRefreshFuture = null;
      nameResolverRefresh = null;
      if (nameResolver != null) {
        nameResolver.refresh();
      }
    }
  }

  // Must be used from channelExecutor
  @Nullable private ScheduledFuture<?> nameResolverRefreshFuture;
  // Must be used from channelExecutor
  @Nullable private NameResolverRefresh nameResolverRefresh;
  // The policy to control backoff between name resolution attempts. Non-null when an attempt is
  // scheduled. Must be used from channelExecutor
  @Nullable private BackoffPolicy nameResolverBackoffPolicy;

  // Must be run from channelExecutor
  private void cancelNameResolverBackoff() {
    if (nameResolverRefreshFuture != null) {
      nameResolverRefreshFuture.cancel(false);
      nameResolverRefresh.cancelled = true;
      nameResolverRefreshFuture = null;
      nameResolverRefresh = null;
      nameResolverBackoffPolicy = null;
    }
  }

  private final ClientTransportProvider transportProvider = new ClientTransportProvider() {
    @Override
    public ClientTransport get(PickSubchannelArgs args) {
      SubchannelPicker pickerCopy = subchannelPicker;
      if (shutdown.get()) {
        // If channel is shut down, delayedTransport is also shut down which will fail the stream
        // properly.
        return delayedTransport;
      }
      if (pickerCopy == null) {
        channelExecutor.executeLater(new Runnable() {
            @Override
            public void run() {
              exitIdleMode();
            }
          }).drain();
        return delayedTransport;
      }
      // There is no need to reschedule the idle timer here.
      //
      // pickerCopy != null, which means idle timer has not expired when this method starts.
      // Even if idle timer expires right after we grab pickerCopy, and it shuts down LoadBalancer
      // which calls Subchannel.shutdown(), the InternalSubchannel will be actually shutdown after
      // SUBCHANNEL_SHUTDOWN_DELAY_SECONDS, which gives the caller time to start RPC on it.
      //
      // In most cases the idle timer is scheduled to fire after the transport has created the
      // stream, which would have reported in-use state to the channel that would have cancelled
      // the idle timer.
      PickResult pickResult = pickerCopy.pickSubchannel(args);
      ClientTransport transport = GrpcUtil.getTransportFromPickResult(
          pickResult, args.getCallOptions().isWaitForReady());
      if (transport != null) {
        return transport;
      }
      return delayedTransport;
    }

    @Override
    public <ReqT> RetriableStream<ReqT> newRetriableStream(
        final MethodDescriptor<ReqT, ?> method,
        final CallOptions callOptions,
        final Metadata headers,
        final Context context) {
      checkState(retryEnabled, "retry should be enabled");
      return new RetriableStream<ReqT>(
          method, headers, channelBufferUsed, perRpcBufferLimit, channelBufferLimit,
          getCallExecutor(callOptions), transportFactory.getScheduledExecutorService(),
          callOptions.getOption(RETRY_POLICY_KEY), callOptions.getOption(HEDGING_POLICY_KEY),
          throttle) {
        @Override
        Status prestart() {
          return uncommittedRetriableStreamsRegistry.add(this);
        }

        @Override
        void postCommit() {
          uncommittedRetriableStreamsRegistry.remove(this);
        }

        @Override
        ClientStream newSubstream(ClientStreamTracer.Factory tracerFactory, Metadata newHeaders) {
          CallOptions newOptions = callOptions.withStreamTracerFactory(tracerFactory);
          ClientTransport transport =
              get(new PickSubchannelArgsImpl(method, newHeaders, newOptions));
          Context origContext = context.attach();
          try {
            return transport.newStream(method, newHeaders, newOptions);
          } finally {
            context.detach(origContext);
          }
        }
      };
    }
  };

  private final Rescheduler idleTimer;

  ManagedChannelImpl(
      AbstractManagedChannelImplBuilder<?> builder,
      ClientTransportFactory clientTransportFactory,
      BackoffPolicy.Provider backoffPolicyProvider,
      ObjectPool<? extends Executor> oobExecutorPool,
      Supplier<Stopwatch> stopwatchSupplier,
      List<ClientInterceptor> interceptors,
      final TimeProvider timeProvider) {
    this.target = checkNotNull(builder.target, "target");
    this.nameResolverFactory = builder.getNameResolverFactory();
    this.nameResolverParams = checkNotNull(builder.getNameResolverParams(), "nameResolverParams");
    this.nameResolver = getNameResolver(target, nameResolverFactory, nameResolverParams);
    if (builder.loadBalancerFactory == null) {
      this.loadBalancerFactory = new AutoConfiguredLoadBalancerFactory();
    } else {
      this.loadBalancerFactory = builder.loadBalancerFactory;
    }
    this.executorPool = checkNotNull(builder.executorPool, "executorPool");
    this.oobExecutorPool = checkNotNull(oobExecutorPool, "oobExecutorPool");
    this.executor = checkNotNull(executorPool.getObject(), "executor");
    this.delayedTransport = new DelayedClientTransport(this.executor, this.channelExecutor);
    this.delayedTransport.start(delayedTransportListener);
    this.backoffPolicyProvider = backoffPolicyProvider;
    this.transportFactory =
        new CallCredentialsApplyingTransportFactory(clientTransportFactory, this.executor);
    this.retryEnabled = builder.retryEnabled && !builder.temporarilyDisableRetry;
    serviceConfigInterceptor = new ServiceConfigInterceptor(
        retryEnabled, builder.maxRetryAttempts, builder.maxHedgedAttempts);
    Channel channel = new RealChannel();
    channel = ClientInterceptors.intercept(channel, serviceConfigInterceptor);
    if (builder.binlog != null) {
      channel = builder.binlog.wrapChannel(channel);
    }
    this.interceptorChannel = ClientInterceptors.intercept(channel, interceptors);
    this.stopwatchSupplier = checkNotNull(stopwatchSupplier, "stopwatchSupplier");
    if (builder.idleTimeoutMillis == IDLE_TIMEOUT_MILLIS_DISABLE) {
      this.idleTimeoutMillis = builder.idleTimeoutMillis;
    } else {
      checkArgument(
          builder.idleTimeoutMillis
              >= AbstractManagedChannelImplBuilder.IDLE_MODE_MIN_TIMEOUT_MILLIS,
          "invalid idleTimeoutMillis %s", builder.idleTimeoutMillis);
      this.idleTimeoutMillis = builder.idleTimeoutMillis;
    }

    final class AutoDrainChannelExecutor implements Executor {

      @Override
      public void execute(Runnable command) {
        channelExecutor.executeLater(command);
        channelExecutor.drain();
      }
    }

    idleTimer = new Rescheduler(
        new IdleModeTimer(),
        new AutoDrainChannelExecutor(),
        transportFactory.getScheduledExecutorService(),
        stopwatchSupplier.get());
    this.fullStreamDecompression = builder.fullStreamDecompression;
    this.decompressorRegistry = checkNotNull(builder.decompressorRegistry, "decompressorRegistry");
    this.compressorRegistry = checkNotNull(builder.compressorRegistry, "compressorRegistry");
    this.userAgent = builder.userAgent;

    this.channelBufferLimit = builder.retryBufferSize;
    this.perRpcBufferLimit = builder.perRpcBufferLimit;
    this.timeProvider = checkNotNull(timeProvider, "timeProvider");
    this.callTracerFactory = new CallTracer.Factory() {
      @Override
      public CallTracer create() {
        return new CallTracer(timeProvider);
      }
    };
    channelCallTracer = callTracerFactory.create();
    this.channelz = checkNotNull(builder.channelz);
    channelz.addRootChannel(this);

    maxTraceEvents = builder.maxTraceEvents;
    if (maxTraceEvents > 0) {
      long currentTimeNanos = timeProvider.currentTimeNanos();
      channelTracer = new ChannelTracer(builder.maxTraceEvents, currentTimeNanos, "Channel");
    } else {
      channelTracer = null;
    }
    logger.log(Level.FINE, "[{0}] Created with target {1}", new Object[] {getLogId(), target});
  }

  @VisibleForTesting
  static NameResolver getNameResolver(String target, NameResolver.Factory nameResolverFactory,
      Attributes nameResolverParams) {
    // Finding a NameResolver. Try using the target string as the URI. If that fails, try prepending
    // "dns:///".
    URI targetUri = null;
    StringBuilder uriSyntaxErrors = new StringBuilder();
    try {
      targetUri = new URI(target);
      // For "localhost:8080" this would likely cause newNameResolver to return null, because
      // "localhost" is parsed as the scheme. Will fall into the next branch and try
      // "dns:///localhost:8080".
    } catch (URISyntaxException e) {
      // Can happen with ip addresses like "[::1]:1234" or 127.0.0.1:1234.
      uriSyntaxErrors.append(e.getMessage());
    }
    if (targetUri != null) {
      NameResolver resolver = nameResolverFactory.newNameResolver(targetUri, nameResolverParams);
      if (resolver != null) {
        return resolver;
      }
      // "foo.googleapis.com:8080" cause resolver to be null, because "foo.googleapis.com" is an
      // unmapped scheme. Just fall through and will try "dns:///foo.googleapis.com:8080"
    }

    // If we reached here, the targetUri couldn't be used.
    if (!URI_PATTERN.matcher(target).matches()) {
      // It doesn't look like a URI target. Maybe it's an authority string. Try with the default
      // scheme from the factory.
      try {
        targetUri = new URI(nameResolverFactory.getDefaultScheme(), "", "/" + target, null);
      } catch (URISyntaxException e) {
        // Should not be possible.
        throw new IllegalArgumentException(e);
      }
      NameResolver resolver = nameResolverFactory.newNameResolver(targetUri, nameResolverParams);
      if (resolver != null) {
        return resolver;
      }
    }
    throw new IllegalArgumentException(String.format(
        "cannot find a NameResolver for %s%s",
        target, uriSyntaxErrors.length() > 0 ? " (" + uriSyntaxErrors + ")" : ""));
  }

  /**
   * Initiates an orderly shutdown in which preexisting calls continue but new calls are immediately
   * cancelled.
   */
  @Override
  public ManagedChannelImpl shutdown() {
    logger.log(Level.FINE, "[{0}] shutdown() called", getLogId());
    if (!shutdown.compareAndSet(false, true)) {
      return this;
    }

    // Put gotoState(SHUTDOWN) as early into the channelExecutor's queue as possible.
    // delayedTransport.shutdown() may also add some tasks into the queue. But some things inside
    // delayedTransport.shutdown() like setting delayedTransport.shutdown = true are not run in the
    // channelExecutor's queue and should not be blocked, so we do not drain() immediately here.
    channelExecutor.executeLater(new Runnable() {
      @Override
      public void run() {
        if (channelTracer != null) {
          channelTracer.reportEvent(new ChannelTrace.Event.Builder()
              .setDescription("Entering SHUTDOWN state")
              .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
              .setTimestampNanos(timeProvider.currentTimeNanos())
              .build());
        }
        channelStateManager.gotoState(SHUTDOWN);
      }
    });

    uncommittedRetriableStreamsRegistry.onShutdown(SHUTDOWN_STATUS);
    channelExecutor.executeLater(new Runnable() {
        @Override
        public void run() {
          cancelIdleTimer(/* permanent= */ true);
        }
      }).drain();
    logger.log(Level.FINE, "[{0}] Shutting down", getLogId());
    return this;
  }

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are cancelled. Although
   * forceful, the shutdown process is still not instantaneous; {@link #isTerminated()} will likely
   * return {@code false} immediately after this method returns.
   */
  @Override
  public ManagedChannelImpl shutdownNow() {
    logger.log(Level.FINE, "[{0}] shutdownNow() called", getLogId());
    shutdown();
    uncommittedRetriableStreamsRegistry.onShutdownNow(SHUTDOWN_NOW_STATUS);
    channelExecutor.executeLater(new Runnable() {
        @Override
        public void run() {
          if (shutdownNowed) {
            return;
          }
          shutdownNowed = true;
          maybeShutdownNowSubchannels();
        }
      }).drain();
    return this;
  }

  // Called from channelExecutor
  @VisibleForTesting
  void panic(final Throwable t) {
    if (panicMode) {
      // Preserve the first panic information
      return;
    }
    panicMode = true;
    cancelIdleTimer(/* permanent= */ true);
    shutdownNameResolverAndLoadBalancer(false);
    SubchannelPicker newPicker = new SubchannelPicker() {
      final PickResult panicPickResult =
          PickResult.withDrop(
              Status.INTERNAL.withDescription("Panic! This is a bug!").withCause(t));
      @Override
      public PickResult pickSubchannel(PickSubchannelArgs args) {
        return panicPickResult;
      }
    };
    updateSubchannelPicker(newPicker);
    if (channelTracer != null) {
      channelTracer.reportEvent(
          new ChannelTrace.Event.Builder()
              .setDescription("Entering TRANSIENT_FAILURE state")
              .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
              .setTimestampNanos(timeProvider.currentTimeNanos())
              .build());
    }
    channelStateManager.gotoState(TRANSIENT_FAILURE);
  }

  // Called from channelExecutor
  private void updateSubchannelPicker(SubchannelPicker newPicker) {
    subchannelPicker = newPicker;
    delayedTransport.reprocess(newPicker);
  }

  @Override
  public boolean isShutdown() {
    return shutdown.get();
  }

  @Override
  public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException {
    return terminatedLatch.await(timeout, unit);
  }

  @Override
  public boolean isTerminated() {
    return terminated;
  }

  /*
   * Creates a new outgoing call on the channel.
   */
  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method,
      CallOptions callOptions) {
    return interceptorChannel.newCall(method, callOptions);
  }

  @Override
  public String authority() {
    return interceptorChannel.authority();
  }

  private Executor getCallExecutor(CallOptions callOptions) {
    Executor executor = callOptions.getExecutor();
    if (executor == null) {
      executor = this.executor;
    }
    return executor;
  }

  private class RealChannel extends Channel {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method,
        CallOptions callOptions) {
      return new ClientCallImpl<ReqT, RespT>(
              method,
              getCallExecutor(callOptions),
              callOptions,
              transportProvider,
              terminated ? null : transportFactory.getScheduledExecutorService(),
              channelCallTracer,
              retryEnabled)
          .setFullStreamDecompression(fullStreamDecompression)
          .setDecompressorRegistry(decompressorRegistry)
          .setCompressorRegistry(compressorRegistry);
    }

    @Override
    public String authority() {
      String authority = nameResolver.getServiceAuthority();
      return checkNotNull(authority, "authority");
    }
  }

  /**
   * Terminate the channel if termination conditions are met.
   */
  // Must be run from channelExecutor
  private void maybeTerminateChannel() {
    if (terminated) {
      return;
    }
    if (shutdown.get() && subchannels.isEmpty() && oobChannels.isEmpty()) {
      logger.log(Level.FINE, "[{0}] Terminated", getLogId());
      channelz.removeRootChannel(this);
      terminated = true;
      terminatedLatch.countDown();
      executorPool.returnObject(executor);
      // Release the transport factory so that it can deallocate any resources.
      transportFactory.close();
    }
  }

  @Override
  public ConnectivityState getState(boolean requestConnection) {
    ConnectivityState savedChannelState = channelStateManager.getState();
    if (requestConnection && savedChannelState == IDLE) {
      channelExecutor.executeLater(
          new Runnable() {
            @Override
            public void run() {
              exitIdleMode();
              if (subchannelPicker != null) {
                subchannelPicker.requestConnection();
              }
            }
          }).drain();
    }
    return savedChannelState;
  }

  @Override
  public void notifyWhenStateChanged(final ConnectivityState source, final Runnable callback) {
    channelExecutor.executeLater(
        new Runnable() {
          @Override
          public void run() {
            channelStateManager.notifyWhenStateChanged(callback, executor, source);
          }
        }).drain();
  }

  @Override
  public void resetConnectBackoff() {
    channelExecutor
        .executeLater(
            new Runnable() {
              @Override
              public void run() {
                if (shutdown.get()) {
                  return;
                }
                if (nameResolverRefreshFuture != null) {
                  checkState(nameResolverStarted, "name resolver must be started");
                  cancelNameResolverBackoff();
                  nameResolver.refresh();
                }
                for (InternalSubchannel subchannel : subchannels) {
                  subchannel.resetConnectBackoff();
                }
                for (OobChannel oobChannel : oobChannels) {
                  oobChannel.resetConnectBackoff();
                }
              }
            })
        .drain();
  }

  @Override
  public void enterIdle() {
    class PrepareToLoseNetworkRunnable implements Runnable {
      @Override
      public void run() {
        if (shutdown.get() || lbHelper == null) {
          return;
        }
        cancelIdleTimer(/* permanent= */ false);
        enterIdleMode();
      }
    }

    channelExecutor.executeLater(new PrepareToLoseNetworkRunnable()).drain();
  }

  /**
   * A registry that prevents channel shutdown from killing existing retry attempts that are in
   * backoff.
   */
  private final class UncommittedRetriableStreamsRegistry {
    // TODO(zdapeng): This means we would acquire a lock for each new retry-able stream,
    // it's worthwhile to look for a lock-free approach.
    final Object lock = new Object();

    @GuardedBy("lock")
    Collection<ClientStream> uncommittedRetriableStreams = new HashSet<ClientStream>();

    @GuardedBy("lock")
    Status shutdownStatus;

    void onShutdown(Status reason) {
      boolean shouldShutdownDelayedTransport = false;
      synchronized (lock) {
        if (shutdownStatus != null) {
          return;
        }
        shutdownStatus = reason;
        // Keep the delayedTransport open until there is no more uncommitted streams, b/c those
        // retriable streams, which may be in backoff and not using any transport, are already
        // started RPCs.
        if (uncommittedRetriableStreams.isEmpty()) {
          shouldShutdownDelayedTransport = true;
        }
      }

      if (shouldShutdownDelayedTransport) {
        delayedTransport.shutdown(reason);
      }
    }

    void onShutdownNow(Status reason) {
      onShutdown(reason);
      Collection<ClientStream> streams;

      synchronized (lock) {
        streams = new ArrayList<>(uncommittedRetriableStreams);
      }

      for (ClientStream stream : streams) {
        stream.cancel(reason);
      }
      delayedTransport.shutdownNow(reason);
    }

    /**
     * Registers a RetriableStream and return null if not shutdown, otherwise just returns the
     * shutdown Status.
     */
    @Nullable
    Status add(RetriableStream<?> retriableStream) {
      synchronized (lock) {
        if (shutdownStatus != null) {
          return shutdownStatus;
        }
        uncommittedRetriableStreams.add(retriableStream);
        return null;
      }
    }

    void remove(RetriableStream<?> retriableStream) {
      Status shutdownStatusCopy = null;

      synchronized (lock) {
        uncommittedRetriableStreams.remove(retriableStream);
        if (uncommittedRetriableStreams.isEmpty()) {
          shutdownStatusCopy = shutdownStatus;
          // Because retriable transport is long-lived, we take this opportunity to down-size the
          // hashmap.
          uncommittedRetriableStreams = new HashSet<ClientStream>();
        }
      }

      if (shutdownStatusCopy != null) {
        delayedTransport.shutdown(shutdownStatusCopy);
      }
    }
  }

  private class LbHelperImpl extends LoadBalancer.Helper {
    LoadBalancer lb;
    final NameResolver nr;

    LbHelperImpl(NameResolver nr) {
      this.nr = checkNotNull(nr, "NameResolver");
    }

    // Must be called from channelExecutor
    private void handleInternalSubchannelState(ConnectivityStateInfo newState) {
      if (newState.getState() == TRANSIENT_FAILURE || newState.getState() == IDLE) {
        nr.refresh();
      }
    }

    @Override
    public AbstractSubchannel createSubchannel(
        List<EquivalentAddressGroup> addressGroups, Attributes attrs) {
      checkNotNull(addressGroups, "addressGroups");
      checkNotNull(attrs, "attrs");
      // TODO(ejona): can we be even stricter? Like loadBalancer == null?
      checkState(!terminated, "Channel is terminated");
      final SubchannelImpl subchannel = new SubchannelImpl(attrs);
      ChannelTracer subchannelTracer = null;
      long subchannelCreationTime = timeProvider.currentTimeNanos();
      if (maxTraceEvents > 0) {
        subchannelTracer = new ChannelTracer(maxTraceEvents, subchannelCreationTime, "Subchannel");
      }
      final InternalSubchannel internalSubchannel = new InternalSubchannel(
          addressGroups,
          authority(),
          userAgent,
          backoffPolicyProvider,
          transportFactory,
          transportFactory.getScheduledExecutorService(),
          stopwatchSupplier,
          channelExecutor,
          new InternalSubchannel.Callback() {
            // All callbacks are run in channelExecutor
            @Override
            void onTerminated(InternalSubchannel is) {
              subchannels.remove(is);
              channelz.removeSubchannel(is);
              maybeTerminateChannel();
            }

            @Override
            void onStateChange(InternalSubchannel is, ConnectivityStateInfo newState) {
              handleInternalSubchannelState(newState);
              // Call LB only if it's not shutdown.  If LB is shutdown, lbHelper won't match.
              if (LbHelperImpl.this == ManagedChannelImpl.this.lbHelper) {
                lb.handleSubchannelState(subchannel, newState);
              }
            }

            @Override
            void onInUse(InternalSubchannel is) {
              inUseStateAggregator.updateObjectInUse(is, true);
            }

            @Override
            void onNotInUse(InternalSubchannel is) {
              inUseStateAggregator.updateObjectInUse(is, false);
            }
          },
          channelz,
          callTracerFactory.create(),
          subchannelTracer,
          timeProvider);
      if (channelTracer != null) {
        channelTracer.reportEvent(new ChannelTrace.Event.Builder()
            .setDescription("Child channel created")
            .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
            .setTimestampNanos(subchannelCreationTime)
            .setSubchannelRef(internalSubchannel)
            .build());
      }
      channelz.addSubchannel(internalSubchannel);
      subchannel.subchannel = internalSubchannel;
      logger.log(Level.FINE, "[{0}] {1} created for {2}",
          new Object[] {getLogId(), internalSubchannel.getLogId(), addressGroups});
      runSerialized(new Runnable() {
          @Override
          public void run() {
            if (terminating) {
              // Because runSerialized() doesn't guarantee the runnable has been executed upon when
              // returning, the subchannel may still be returned to the balancer without being
              // shutdown even if "terminating" is already true.  The subchannel will not be used in
              // this case, because delayed transport has terminated when "terminating" becomes
              // true, and no more requests will be sent to balancer beyond this point.
              internalSubchannel.shutdown(SHUTDOWN_STATUS);
            }
            if (!terminated) {
              // If channel has not terminated, it will track the subchannel and block termination
              // for it.
              subchannels.add(internalSubchannel);
            }
          }
        });
      return subchannel;
    }

    @Override
    public void updateBalancingState(
        final ConnectivityState newState, final SubchannelPicker newPicker) {
      checkNotNull(newState, "newState");
      checkNotNull(newPicker, "newPicker");

      runSerialized(
          new Runnable() {
            @Override
            public void run() {
              if (LbHelperImpl.this != lbHelper) {
                return;
              }
              updateSubchannelPicker(newPicker);
              // It's not appropriate to report SHUTDOWN state from lb.
              // Ignore the case of newState == SHUTDOWN for now.
              if (newState != SHUTDOWN) {
                if (channelTracer != null) {
                  channelTracer.reportEvent(
                      new ChannelTrace.Event.Builder()
                          .setDescription("Entering " + newState + " state")
                          .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
                          .setTimestampNanos(timeProvider.currentTimeNanos())
                          .build());
                }
                channelStateManager.gotoState(newState);
              }
            }
          });
    }

    @Override
    public void updateSubchannelAddresses(
        LoadBalancer.Subchannel subchannel, List<EquivalentAddressGroup> addrs) {
      checkArgument(subchannel instanceof SubchannelImpl,
          "subchannel must have been returned from createSubchannel");
      ((SubchannelImpl) subchannel).subchannel.updateAddresses(addrs);
    }

    @Override
    public ManagedChannel createOobChannel(EquivalentAddressGroup addressGroup, String authority) {
      // TODO(ejona): can we be even stricter? Like terminating?
      checkState(!terminated, "Channel is terminated");
      long oobChannelCreationTime = timeProvider.currentTimeNanos();
      ChannelTracer oobChannelTracer = null;
      ChannelTracer subchannelTracer = null;
      if (channelTracer != null) {
        oobChannelTracer = new ChannelTracer(maxTraceEvents, oobChannelCreationTime, "OobChannel");
      }
      final OobChannel oobChannel = new OobChannel(
          authority, oobExecutorPool, transportFactory.getScheduledExecutorService(),
          channelExecutor, callTracerFactory.create(), oobChannelTracer, channelz, timeProvider);
      if (channelTracer != null) {
        channelTracer.reportEvent(new ChannelTrace.Event.Builder()
            .setDescription("Child channel created")
            .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
            .setTimestampNanos(oobChannelCreationTime)
            .setChannelRef(oobChannel)
            .build());
        subchannelTracer = new ChannelTracer(maxTraceEvents, oobChannelCreationTime, "Subchannel");
      }
      final InternalSubchannel internalSubchannel = new InternalSubchannel(
          Collections.singletonList(addressGroup),
          authority, userAgent, backoffPolicyProvider, transportFactory,
          transportFactory.getScheduledExecutorService(), stopwatchSupplier, channelExecutor,
          // All callback methods are run from channelExecutor
          new InternalSubchannel.Callback() {
            @Override
            void onTerminated(InternalSubchannel is) {
              oobChannels.remove(oobChannel);
              channelz.removeSubchannel(is);
              oobChannel.handleSubchannelTerminated();
              maybeTerminateChannel();
            }

            @Override
            void onStateChange(InternalSubchannel is, ConnectivityStateInfo newState) {
              handleInternalSubchannelState(newState);
              oobChannel.handleSubchannelStateChange(newState);
            }
          },
          channelz,
          callTracerFactory.create(),
          subchannelTracer,
          timeProvider);
      if (oobChannelTracer != null) {
        oobChannelTracer.reportEvent(new ChannelTrace.Event.Builder()
            .setDescription("Child channel created")
            .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
            .setTimestampNanos(oobChannelCreationTime)
            .setSubchannelRef(internalSubchannel)
            .build());
      }
      channelz.addSubchannel(oobChannel);
      channelz.addSubchannel(internalSubchannel);
      oobChannel.setSubchannel(internalSubchannel);
      runSerialized(new Runnable() {
          @Override
          public void run() {
            if (terminating) {
              oobChannel.shutdown();
            }
            if (!terminated) {
              // If channel has not terminated, it will track the subchannel and block termination
              // for it.
              oobChannels.add(oobChannel);
            }
          }
        });
      return oobChannel;
    }

    @Override
    public void updateOobChannelAddresses(ManagedChannel channel, EquivalentAddressGroup eag) {
      checkArgument(channel instanceof OobChannel,
          "channel must have been returned from createOobChannel");
      ((OobChannel) channel).updateAddresses(eag);
    }

    @Override
    public String getAuthority() {
      return ManagedChannelImpl.this.authority();
    }

    @Override
    public NameResolver.Factory getNameResolverFactory() {
      return nameResolverFactory;
    }

    @Override
    public void runSerialized(Runnable task) {
      channelExecutor.executeLater(task).drain();
    }
  }

  private class NameResolverListenerImpl implements NameResolver.Listener {
    final LbHelperImpl helper;

    NameResolverListenerImpl(LbHelperImpl helperImpl) {
      this.helper = helperImpl;
    }

    @Override
    public void onAddresses(final List<EquivalentAddressGroup> servers, final Attributes config) {
      if (servers.isEmpty()) {
        onError(Status.UNAVAILABLE.withDescription("NameResolver returned an empty list"));
        return;
      }
      if (logger.isLoggable(Level.FINE)) {
        logger.log(Level.FINE, "[{0}] resolved address: {1}, config={2}",
            new Object[]{getLogId(), servers, config});
      }

      if (channelTracer != null && (haveBackends == null || !haveBackends)) {
        channelTracer.reportEvent(new ChannelTrace.Event.Builder()
            .setDescription("Address resolved: " + servers)
            .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
            .setTimestampNanos(timeProvider.currentTimeNanos())
            .build());
        haveBackends = true;
      }
      final Map<String, Object> serviceConfig =
          config.get(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG);
      if (channelTracer != null && serviceConfig != null
          && !serviceConfig.equals(lastServiceConfig)) {
        channelTracer.reportEvent(new ChannelTrace.Event.Builder()
            .setDescription("Service config changed")
            .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
            .setTimestampNanos(timeProvider.currentTimeNanos())
            .build());
        lastServiceConfig = serviceConfig;
      }

      final class NamesResolved implements Runnable {
        @Override
        public void run() {
          // Call LB only if it's not shutdown.  If LB is shutdown, lbHelper won't match.
          if (NameResolverListenerImpl.this.helper != ManagedChannelImpl.this.lbHelper) {
            return;
          }

          nameResolverBackoffPolicy = null;

          if (serviceConfig != null) {
            try {
              serviceConfigInterceptor.handleUpdate(serviceConfig);
              if (retryEnabled) {
                throttle = getThrottle(config);
              }
            } catch (RuntimeException re) {
              logger.log(
                  Level.WARNING,
                  "[" + getLogId() + "] Unexpected exception from parsing service config",
                  re);
            }
          }

          helper.lb.handleResolvedAddressGroups(servers, config);
        }
      }

      helper.runSerialized(new NamesResolved());
    }

    @Override
    public void onError(final Status error) {
      checkArgument(!error.isOk(), "the error status must not be OK");
      logger.log(Level.WARNING, "[{0}] Failed to resolve name. status={1}",
          new Object[] {getLogId(), error});
      if (channelTracer != null && (haveBackends == null || haveBackends)) {
        channelTracer.reportEvent(new ChannelTrace.Event.Builder()
            .setDescription("Failed to resolve name")
            .setSeverity(ChannelTrace.Event.Severity.CT_WARNING)
            .setTimestampNanos(timeProvider.currentTimeNanos())
            .build());
        haveBackends = false;
      }
      channelExecutor
          .executeLater(
              new Runnable() {
                @Override
                public void run() {
                  // Call LB only if it's not shutdown.  If LB is shutdown, lbHelper won't match.
                  if (NameResolverListenerImpl.this.helper != ManagedChannelImpl.this.lbHelper) {
                    return;
                  }
                  helper.lb.handleNameResolutionError(error);
                  if (nameResolverRefreshFuture != null) {
                    // The name resolver may invoke onError multiple times, but we only want to
                    // schedule one backoff attempt
                    // TODO(ericgribkoff) Update contract of NameResolver.Listener or decide if we
                    // want to reset the backoff interval upon repeated onError() calls
                    return;
                  }
                  if (nameResolverBackoffPolicy == null) {
                    nameResolverBackoffPolicy = backoffPolicyProvider.get();
                  }
                  long delayNanos = nameResolverBackoffPolicy.nextBackoffNanos();
                  if (logger.isLoggable(Level.FINE)) {
                    logger.log(
                        Level.FINE,
                        "[{0}] Scheduling DNS resolution backoff for {1} ns",
                        new Object[] {logId, delayNanos});
                  }
                  nameResolverRefresh = new NameResolverRefresh();
                  nameResolverRefreshFuture =
                      transportFactory
                          .getScheduledExecutorService()
                          .schedule(nameResolverRefresh, delayNanos, TimeUnit.NANOSECONDS);
                }
              })
          .drain();
    }
  }

  @Nullable
  private static Throttle getThrottle(Attributes config) {
    return ServiceConfigUtil.getThrottlePolicy(
        config.get(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG));
  }

  private final class SubchannelImpl extends AbstractSubchannel {
    // Set right after SubchannelImpl is created.
    InternalSubchannel subchannel;
    final Object shutdownLock = new Object();
    final Attributes attrs;

    @GuardedBy("shutdownLock")
    boolean shutdownRequested;
    @GuardedBy("shutdownLock")
    ScheduledFuture<?> delayedShutdownTask;

    SubchannelImpl(Attributes attrs) {
      this.attrs = checkNotNull(attrs, "attrs");
    }

    @Override
    ClientTransport obtainActiveTransport() {
      return subchannel.obtainActiveTransport();
    }

    @Override
    InternalInstrumented<ChannelStats> getInternalSubchannel() {
      return subchannel;
    }

    @Override
    public void shutdown() {
      synchronized (shutdownLock) {
        if (shutdownRequested) {
          if (terminating && delayedShutdownTask != null) {
            // shutdown() was previously called when terminating == false, thus a delayed shutdown()
            // was scheduled.  Now since terminating == true, We should expedite the shutdown.
            delayedShutdownTask.cancel(false);
            delayedShutdownTask = null;
            // Will fall through to the subchannel.shutdown() at the end.
          } else {
            return;
          }
        } else {
          shutdownRequested = true;
        }
        // Add a delay to shutdown to deal with the race between 1) a transport being picked and
        // newStream() being called on it, and 2) its Subchannel is shut down by LoadBalancer (e.g.,
        // because of address change, or because LoadBalancer is shutdown by Channel entering idle
        // mode). If (2) wins, the app will see a spurious error. We work around this by delaying
        // shutdown of Subchannel for a few seconds here.
        //
        // TODO(zhangkun83): consider a better approach
        // (https://github.com/grpc/grpc-java/issues/2562).
        if (!terminating) {
          delayedShutdownTask = transportFactory.getScheduledExecutorService().schedule(
              new LogExceptionRunnable(
                  new Runnable() {
                    @Override
                    public void run() {
                      subchannel.shutdown(SUBCHANNEL_SHUTDOWN_STATUS);
                    }
                  }), SUBCHANNEL_SHUTDOWN_DELAY_SECONDS, TimeUnit.SECONDS);
          return;
        }
      }
      // When terminating == true, no more real streams will be created. It's safe and also
      // desirable to shutdown timely.
      subchannel.shutdown(SHUTDOWN_STATUS);
    }

    @Override
    public void requestConnection() {
      subchannel.obtainActiveTransport();
    }

    @Override
    public List<EquivalentAddressGroup> getAllAddresses() {
      return subchannel.getAddressGroups();
    }

    @Override
    public Attributes getAttributes() {
      return attrs;
    }

    @Override
    public String toString() {
      return subchannel.getLogId().toString();
    }
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("logId", logId.getId())
        .add("target", target)
        .toString();
  }
}
