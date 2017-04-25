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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors;
import io.grpc.CompressorRegistry;
import io.grpc.ConnectivityStateInfo;
import io.grpc.DecompressorRegistry;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.ManagedChannel;
import io.grpc.MethodDescriptor;
import io.grpc.NameResolver;
import io.grpc.Status;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;
import java.net.URI;
import java.net.URISyntaxException;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.logging.Level;
import java.util.logging.Logger;
import java.util.regex.Pattern;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/** A communication channel for making outgoing RPCs. */
@ThreadSafe
public final class ManagedChannelImpl extends ManagedChannel implements WithLogId {
  private static final Logger log = Logger.getLogger(ManagedChannelImpl.class.getName());

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

  private final String target;
  private final NameResolver.Factory nameResolverFactory;
  private final Attributes nameResolverParams;
  private final LoadBalancer.Factory loadBalancerFactory;
  private final ClientTransportFactory transportFactory;
  private final Executor executor;
  private final ObjectPool<? extends Executor> executorPool;
  private final ObjectPool<? extends Executor> oobExecutorPool;
  private final LogId logId = LogId.allocate(getClass().getName());

  private final ChannelExecutor channelExecutor = new ChannelExecutor();

  private final DecompressorRegistry decompressorRegistry;
  private final CompressorRegistry compressorRegistry;

  private final ObjectPool<ScheduledExecutorService> timerServicePool;
  private final Supplier<Stopwatch> stopwatchSupplier;
  /** The timout before entering idle mode. */
  private final long idleTimeoutMillis;

  /**
   * Executor that runs deadline timers for requests.
   */
  // Must be assigned from channelExecutor
  private volatile ScheduledExecutorService scheduledExecutor;

  private final BackoffPolicy.Provider backoffPolicyProvider;

  /**
   * We delegate to this channel, so that we can have interceptors as necessary. If there aren't
   * any interceptors this will just be {@link RealChannel}.
   */
  private final Channel interceptorChannel;
  @Nullable private final String userAgent;

  // Only null after channel is terminated. Must be assigned from the channelExecutor.
  private NameResolver nameResolver;

  // null when channel is in idle mode.  Must be assigned from channelExecutor.
  @Nullable
  private LoadBalancer loadBalancer;

  // Must be assigned from channelExecutor.  null if channel is in idle mode.
  @Nullable
  private volatile SubchannelPicker subchannelPicker;

  // Must be mutated from channelExecutor
  // If any monitoring hook to be added later needs to get a snapshot of this Set, we could
  // switch to a ConcurrentHashMap.
  private final Set<InternalSubchannel> subchannels = new HashSet<InternalSubchannel>(16, .75f);

  // Must be mutated from channelExecutor
  private final Set<InternalSubchannel> oobChannels = new HashSet<InternalSubchannel>(1, .75f);

  // reprocess() must be run from channelExecutor
  private final DelayedClientTransport delayedTransport;

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
          if (loadBalancer != null) {
            loadBalancer.shutdown();
            loadBalancer = null;
          }
          if (nameResolver != null) {
            nameResolver.shutdown();
            nameResolver = null;
          }

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
      for (InternalSubchannel oobChannel : oobChannels) {
        oobChannel.shutdownNow(SHUTDOWN_NOW_STATUS);
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

  // Run from channelExecutor
  private class IdleModeTimer implements Runnable {
    // Only mutated from channelExecutor
    boolean cancelled;

    @Override
    public void run() {
      if (cancelled) {
        // Race detected: this task was scheduled on channelExecutor before cancelIdleTimer()
        // could cancel the timer.
        return;
      }
      log.log(Level.FINE, "[{0}] Entering idle mode", getLogId());
      // nameResolver and loadBalancer are guaranteed to be non-null.  If any of them were null,
      // either the idleModeTimer ran twice without exiting the idle mode, or the task in shutdown()
      // did not cancel idleModeTimer, both of which are bugs.
      nameResolver.shutdown();
      nameResolver = getNameResolver(target, nameResolverFactory, nameResolverParams);
      loadBalancer.shutdown();
      loadBalancer = null;
      subchannelPicker = null;
    }
  }

  // Must be used from channelExecutor
  @Nullable
  private ScheduledFuture<?> idleModeTimerFuture;
  // Must be used from channelExecutor
  @Nullable
  private IdleModeTimer idleModeTimer;

  /**
   * Make the channel exit idle mode, if it's in it.
   *
   * <p>Must be called from channelExecutor
   */
  @VisibleForTesting
  void exitIdleMode() {
    if (shutdown.get()) {
      return;
    }
    if (inUseStateAggregator.isInUse()) {
      // Cancel the timer now, so that a racing due timer will not put Channel on idleness
      // when the caller of exitIdleMode() is about to use the returned loadBalancer.
      cancelIdleTimer();
    } else {
      // exitIdleMode() may be called outside of inUseStateAggregator.handleNotInUse() while
      // isInUse() == false, in which case we still need to schedule the timer.
      rescheduleIdleTimer();
    }
    if (loadBalancer != null) {
      return;
    }
    log.log(Level.FINE, "[{0}] Exiting idle mode", getLogId());
    LbHelperImpl helper = new LbHelperImpl(nameResolver);
    helper.lb = loadBalancerFactory.newLoadBalancer(helper);
    this.loadBalancer = helper.lb;

    NameResolverListenerImpl listener = new NameResolverListenerImpl(helper);
    try {
      nameResolver.start(listener);
    } catch (Throwable t) {
      listener.onError(Status.fromThrowable(t));
    }
  }

  // Must be run from channelExecutor
  private void cancelIdleTimer() {
    if (idleModeTimerFuture != null) {
      idleModeTimerFuture.cancel(false);
      idleModeTimer.cancelled = true;
      idleModeTimerFuture = null;
      idleModeTimer = null;
    }
  }

  // Always run from channelExecutor
  private void rescheduleIdleTimer() {
    if (idleTimeoutMillis == IDLE_TIMEOUT_MILLIS_DISABLE) {
      return;
    }
    cancelIdleTimer();
    idleModeTimer = new IdleModeTimer();
    idleModeTimerFuture = scheduledExecutor.schedule(
        new LogExceptionRunnable(new Runnable() {
            @Override
            public void run() {
              channelExecutor.executeLater(idleModeTimer).drain();
            }
          }),
        idleTimeoutMillis, TimeUnit.MILLISECONDS);
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
  };

  ManagedChannelImpl(String target, BackoffPolicy.Provider backoffPolicyProvider,
      NameResolver.Factory nameResolverFactory, Attributes nameResolverParams,
      LoadBalancer.Factory loadBalancerFactory, ClientTransportFactory transportFactory,
      DecompressorRegistry decompressorRegistry, CompressorRegistry compressorRegistry,
      ObjectPool<ScheduledExecutorService> timerServicePool,
      ObjectPool<? extends Executor> executorPool, ObjectPool<? extends Executor> oobExecutorPool,
      Supplier<Stopwatch> stopwatchSupplier, long idleTimeoutMillis,
      @Nullable String userAgent,
      List<ClientInterceptor> interceptors) {
    this.target = checkNotNull(target, "target");
    this.nameResolverFactory = checkNotNull(nameResolverFactory, "nameResolverFactory");
    this.nameResolverParams = checkNotNull(nameResolverParams, "nameResolverParams");
    this.nameResolver = getNameResolver(target, nameResolverFactory, nameResolverParams);
    this.loadBalancerFactory = checkNotNull(loadBalancerFactory, "loadBalancerFactory");
    this.executorPool = checkNotNull(executorPool, "executorPool");
    this.oobExecutorPool = checkNotNull(oobExecutorPool, "oobExecutorPool");
    this.executor = checkNotNull(executorPool.getObject(), "executor");
    this.delayedTransport = new DelayedClientTransport(this.executor, this.channelExecutor);
    this.delayedTransport.start(delayedTransportListener);
    this.backoffPolicyProvider = backoffPolicyProvider;
    this.transportFactory =
        new CallCredentialsApplyingTransportFactory(transportFactory, this.executor);
    this.interceptorChannel = ClientInterceptors.intercept(new RealChannel(), interceptors);
    this.timerServicePool = checkNotNull(timerServicePool, "timerServicePool");
    this.scheduledExecutor = checkNotNull(timerServicePool.getObject(), "timerService");
    this.stopwatchSupplier = checkNotNull(stopwatchSupplier, "stopwatchSupplier");
    if (idleTimeoutMillis == IDLE_TIMEOUT_MILLIS_DISABLE) {
      this.idleTimeoutMillis = idleTimeoutMillis;
    } else {
      checkArgument(
          idleTimeoutMillis >= AbstractManagedChannelImplBuilder.IDLE_MODE_MIN_TIMEOUT_MILLIS,
          "invalid idleTimeoutMillis %s", idleTimeoutMillis);
      this.idleTimeoutMillis = idleTimeoutMillis;
    }
    this.decompressorRegistry = checkNotNull(decompressorRegistry, "decompressorRegistry");
    this.compressorRegistry = checkNotNull(compressorRegistry, "compressorRegistry");
    this.userAgent = userAgent;

    log.log(Level.FINE, "[{0}] Created with target {1}", new Object[] {getLogId(), target});
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
    log.log(Level.FINE, "[{0}] shutdown() called", getLogId());
    if (!shutdown.compareAndSet(false, true)) {
      return this;
    }
    delayedTransport.shutdown();
    channelExecutor.executeLater(new Runnable() {
        @Override
        public void run() {
          cancelIdleTimer();
        }
      }).drain();
    log.log(Level.FINE, "[{0}] Shutting down", getLogId());
    return this;
  }

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are cancelled. Although
   * forceful, the shutdown process is still not instantaneous; {@link #isTerminated()} will likely
   * return {@code false} immediately after this method returns.
   */
  @Override
  public ManagedChannelImpl shutdownNow() {
    log.log(Level.FINE, "[{0}] shutdownNow() called", getLogId());
    shutdown();
    delayedTransport.shutdownNow(SHUTDOWN_NOW_STATUS);
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

  private class RealChannel extends Channel {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method,
        CallOptions callOptions) {
      Executor executor = callOptions.getExecutor();
      if (executor == null) {
        executor = ManagedChannelImpl.this.executor;
      }
      return new ClientCallImpl<ReqT, RespT>(
          method,
          executor,
          callOptions,
          transportProvider,
          scheduledExecutor)
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
      log.log(Level.FINE, "[{0}] Terminated", getLogId());
      terminated = true;
      terminatedLatch.countDown();
      executorPool.returnObject(executor);
      scheduledExecutor = timerServicePool.returnObject(scheduledExecutor);
      // Release the transport factory so that it can deallocate any resources.
      transportFactory.close();
    }
  }

  private class LbHelperImpl extends LoadBalancer.Helper {
    LoadBalancer lb;
    final NameResolver nr;

    LbHelperImpl(NameResolver nr) {
      this.nr = checkNotNull(nr, "NameResolver");
    }

    @Override
    public SubchannelImpl createSubchannel(EquivalentAddressGroup addressGroup, Attributes attrs) {
      checkNotNull(addressGroup, "addressGroup");
      checkNotNull(attrs, "attrs");
      ScheduledExecutorService scheduledExecutorCopy = scheduledExecutor;
      checkState(scheduledExecutorCopy != null,
          "scheduledExecutor is already cleared. Looks like you are calling this method after "
          + "you've already shut down");
      final SubchannelImplImpl subchannel = new SubchannelImplImpl(attrs);
      final InternalSubchannel internalSubchannel = new InternalSubchannel(
            addressGroup, authority(), userAgent, backoffPolicyProvider, transportFactory,
            scheduledExecutorCopy, stopwatchSupplier, channelExecutor,
            new InternalSubchannel.Callback() {
              // All callbacks are run in channelExecutor
              @Override
              void onTerminated(InternalSubchannel is) {
                subchannels.remove(is);
                maybeTerminateChannel();
              }

              @Override
              void onStateChange(InternalSubchannel is, ConnectivityStateInfo newState) {
                if ((newState.getState() == TRANSIENT_FAILURE || newState.getState() == IDLE)) {
                  nr.refresh();
                }
                lb.handleSubchannelState(subchannel, newState);
              }

              @Override
              void onInUse(InternalSubchannel is) {
                inUseStateAggregator.updateObjectInUse(is, true);
              }

              @Override
              void onNotInUse(InternalSubchannel is) {
                inUseStateAggregator.updateObjectInUse(is, false);
              }
            });
      subchannel.subchannel = internalSubchannel;
      log.log(Level.FINE, "[{0}] {1} created for {2}",
          new Object[] {getLogId(), internalSubchannel.getLogId(), addressGroup});
      runSerialized(new Runnable() {
          @Override
          public void run() {
            if (terminating) {
              // Because runSerialized() doesn't guarantee the runnable has been executed upon when
              // returning, the subchannel may still be returned to the balancer without being
              // shutdown even if "terminating" is already true.  The subchannel will not be used in
              // this case, because delayed transport has terminated when "terminating" becomes
              // true, and no more requests will be sent to balancer beyond this point.
              internalSubchannel.shutdown();
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
    public void updateSubchannelAddresses(
        LoadBalancer.Subchannel subchannel, EquivalentAddressGroup addrs) {
      checkArgument(subchannel instanceof SubchannelImplImpl,
          "subchannel must have been returned from createSubchannel");
      ((SubchannelImplImpl) subchannel).subchannel.updateAddresses(addrs);
    }

    @Override
    public ManagedChannel createOobChannel(EquivalentAddressGroup addressGroup, String authority) {
      ScheduledExecutorService scheduledExecutorCopy = scheduledExecutor;
      checkState(scheduledExecutorCopy != null,
          "scheduledExecutor is already cleared. Looks like you are calling this method after "
          + "you've already shut down");
      final OobChannel oobChannel = new OobChannel(
          authority, oobExecutorPool, scheduledExecutorCopy, channelExecutor);
      final InternalSubchannel internalSubchannel = new InternalSubchannel(
          addressGroup, authority, userAgent, backoffPolicyProvider, transportFactory,
          scheduledExecutorCopy, stopwatchSupplier, channelExecutor,
          // All callback methods are run from channelExecutor
          new InternalSubchannel.Callback() {
            @Override
            void onTerminated(InternalSubchannel is) {
              oobChannels.remove(is);
              oobChannel.handleSubchannelTerminated();
              maybeTerminateChannel();
            }

            @Override
            void onStateChange(InternalSubchannel is, ConnectivityStateInfo newState) {
              oobChannel.handleSubchannelStateChange(newState);
            }
          });
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
              oobChannels.add(internalSubchannel);
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

    @Override
    public void updatePicker(final SubchannelPicker picker) {
      runSerialized(new Runnable() {
          @Override
          public void run() {
            subchannelPicker = picker;
            delayedTransport.reprocess(picker);
          }
        });
    }
  }

  @Override
  public LogId getLogId() {
    return logId;
  }

  private class NameResolverListenerImpl implements NameResolver.Listener {
    final LoadBalancer balancer;
    final LoadBalancer.Helper helper;

    NameResolverListenerImpl(LbHelperImpl helperImpl) {
      this.balancer = helperImpl.lb;
      this.helper = helperImpl;
    }

    @Deprecated
    @Override
    public void onUpdate(
        final List<io.grpc.ResolvedServerInfoGroup> servers, final Attributes config) {
      ArrayList<EquivalentAddressGroup> eags =
          new ArrayList<EquivalentAddressGroup>(servers.size());
      for (io.grpc.ResolvedServerInfoGroup infoGroup : servers) {
        eags.add(infoGroup.toEquivalentAddressGroup());
      }
      onAddresses(eags, config);
    }

    @Override
    public void onAddresses(final List<EquivalentAddressGroup> servers, final Attributes config) {
      if (servers.isEmpty()) {
        onError(Status.UNAVAILABLE.withDescription("NameResolver returned an empty list"));
        return;
      }
      log.log(Level.FINE, "[{0}] resolved address: {1}, config={2}",
          new Object[] {getLogId(), servers, config});
      helper.runSerialized(new Runnable() {
          @Override
          public void run() {
            if (terminated) {
              return;
            }
            try {
              balancer.handleResolvedAddressGroups(servers, config);
            } catch (Throwable e) {
              log.log(
                  Level.WARNING, "[" + getLogId() + "] Unexpected exception from LoadBalancer", e);
              // It must be a bug! Push the exception back to LoadBalancer in the hope that it may
              // be propagated to the application.
              balancer.handleNameResolutionError(Status.INTERNAL.withCause(e)
                  .withDescription("Thrown from handleResolvedAddresses(): " + e));
            }
          }
        });
    }

    @Override
    public void onError(final Status error) {
      checkArgument(!error.isOk(), "the error status must not be OK");
      log.log(Level.WARNING, "[{0}] Failed to resolve name. status={1}",
          new Object[] {getLogId(), error});
      channelExecutor.executeLater(new Runnable() {
          @Override
          public void run() {
            if (terminated) {
              return;
            }
            balancer.handleNameResolutionError(error);
          }
        }).drain();
    }
  }

  private final class SubchannelImplImpl extends SubchannelImpl {
    // Set right after SubchannelImplImpl is created.
    InternalSubchannel subchannel;
    final Object shutdownLock = new Object();
    final Attributes attrs;

    @GuardedBy("shutdownLock")
    boolean shutdownRequested;
    @GuardedBy("shutdownLock")
    ScheduledFuture<?> delayedShutdownTask;

    SubchannelImplImpl(Attributes attrs) {
      this.attrs = checkNotNull(attrs, "attrs");
    }

    @Override
    ClientTransport obtainActiveTransport() {
      return subchannel.obtainActiveTransport();
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
        ScheduledExecutorService scheduledExecutorCopy = scheduledExecutor;
        // Add a delay to shutdown to deal with the race between 1) a transport being picked and
        // newStream() being called on it, and 2) its Subchannel is shut down by LoadBalancer (e.g.,
        // because of address change, or because LoadBalancer is shutdown by Channel entering idle
        // mode). If (2) wins, the app will see a spurious error. We work around this by delaying
        // shutdown of Subchannel for a few seconds here.
        //
        // TODO(zhangkun83): consider a better approach
        // (https://github.com/grpc/grpc-java/issues/2562).
        if (!terminating && scheduledExecutorCopy != null) {
          delayedShutdownTask = scheduledExecutorCopy.schedule(
              new LogExceptionRunnable(
                  new Runnable() {
                    @Override
                    public void run() {
                      subchannel.shutdown();
                    }
                  }), SUBCHANNEL_SHUTDOWN_DELAY_SECONDS, TimeUnit.SECONDS);
          return;
        }
      }
      // Two possible ways to get here:
      //
      // 1. terminating == true: no more real streams will be created, it's safe and also desirable
      // to shutdown timely.
      //
      // 2. scheduledExecutor == null: possible only when Channel has already been terminated.
      // Though may not be necessary, we'll do it anyway.
      subchannel.shutdown();
    }

    @Override
    public void requestConnection() {
      subchannel.obtainActiveTransport();
    }

    @Override
    public EquivalentAddressGroup getAddresses() {
      return subchannel.getAddressGroup();
    }

    @Override
    public Attributes getAttributes() {
      return attrs;
    }
  }
}
