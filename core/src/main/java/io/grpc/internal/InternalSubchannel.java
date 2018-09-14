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

package io.grpc.internal;

import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.SHUTDOWN;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.MoreObjects;
import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import com.google.errorprone.annotations.ForOverride;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.ChannelTrace;
import io.grpc.InternalInstrumented;
import io.grpc.InternalLogId;
import io.grpc.InternalWithLogId;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.CheckForNull;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Transports for a single {@link SocketAddress}.
 */
@ThreadSafe
final class InternalSubchannel implements InternalInstrumented<ChannelStats> {
  private static final Logger log = Logger.getLogger(InternalSubchannel.class.getName());

  private final InternalLogId logId = InternalLogId.allocate(getClass().getName());
  private final String authority;
  private final String userAgent;
  private final BackoffPolicy.Provider backoffPolicyProvider;
  private final Callback callback;
  private final ClientTransportFactory transportFactory;
  private final ScheduledExecutorService scheduledExecutor;
  private final InternalChannelz channelz;
  private final CallTracer callsTracer;
  @CheckForNull
  private final ChannelTracer channelTracer;
  private final TimeProvider timeProvider;

  // File-specific convention: methods without GuardedBy("lock") MUST NOT be called under the lock.
  private final Object lock = new Object();

  // File-specific convention:
  //
  // 1. In a method without GuardedBy("lock"), executeLater() MUST be followed by a drain() later in
  // the same method.
  //
  // 2. drain() MUST NOT be called under "lock".
  //
  // 3. Every synchronized("lock") must be inside a try-finally which calls drain() in "finally".
  private final ChannelExecutor channelExecutor;

  /**
   * The index of the address corresponding to pendingTransport/activeTransport, or at beginning if
   * both are null.
   */
  @GuardedBy("lock")
  private Index addressIndex;

  /**
   * The policy to control back off between reconnects. Non-{@code null} when a reconnect task is
   * scheduled.
   */
  @GuardedBy("lock")
  private BackoffPolicy reconnectPolicy;

  /**
   * Timer monitoring duration since entering CONNECTING state.
   */
  @GuardedBy("lock")
  private final Stopwatch connectingTimer;

  @GuardedBy("lock")
  @Nullable
  private ScheduledFuture<?> reconnectTask;

  @GuardedBy("lock")
  private boolean reconnectCanceled;

  /**
   * All transports that are not terminated. At the very least the value of {@link #activeTransport}
   * will be present, but previously used transports that still have streams or are stopping may
   * also be present.
   */
  @GuardedBy("lock")
  private final Collection<ConnectionClientTransport> transports = new ArrayList<>();

  // Must only be used from channelExecutor
  private final InUseStateAggregator<ConnectionClientTransport> inUseStateAggregator =
      new InUseStateAggregator<ConnectionClientTransport>() {
        @Override
        void handleInUse() {
          callback.onInUse(InternalSubchannel.this);
        }

        @Override
        void handleNotInUse() {
          callback.onNotInUse(InternalSubchannel.this);
        }
      };

  /**
   * The to-be active transport, which is not ready yet.
   */
  @GuardedBy("lock")
  @Nullable
  private ConnectionClientTransport pendingTransport;

  /**
   * The transport for new outgoing requests. 'lock' must be held when assigning to it. Non-null
   * only in READY state.
   */
  @Nullable
  private volatile ManagedClientTransport activeTransport;

  @GuardedBy("lock")
  private ConnectivityStateInfo state = ConnectivityStateInfo.forNonError(IDLE);

  @GuardedBy("lock")
  private Status shutdownReason;

  InternalSubchannel(List<EquivalentAddressGroup> addressGroups, String authority, String userAgent,
      BackoffPolicy.Provider backoffPolicyProvider,
      ClientTransportFactory transportFactory, ScheduledExecutorService scheduledExecutor,
      Supplier<Stopwatch> stopwatchSupplier, ChannelExecutor channelExecutor, Callback callback,
      InternalChannelz channelz, CallTracer callsTracer, @Nullable ChannelTracer channelTracer,
      TimeProvider timeProvider) {
    Preconditions.checkNotNull(addressGroups, "addressGroups");
    Preconditions.checkArgument(!addressGroups.isEmpty(), "addressGroups is empty");
    checkListHasNoNulls(addressGroups, "addressGroups contains null entry");
    this.addressIndex = new Index(
        Collections.unmodifiableList(new ArrayList<>(addressGroups)));
    this.authority = authority;
    this.userAgent = userAgent;
    this.backoffPolicyProvider = backoffPolicyProvider;
    this.transportFactory = transportFactory;
    this.scheduledExecutor = scheduledExecutor;
    this.connectingTimer = stopwatchSupplier.get();
    this.channelExecutor = channelExecutor;
    this.callback = callback;
    this.channelz = channelz;
    this.callsTracer = callsTracer;
    this.channelTracer = channelTracer;
    this.timeProvider = timeProvider;
  }

  /**
   * Returns a READY transport that will be used to create new streams.
   *
   * <p>Returns {@code null} if the state is not READY.  Will try to connect if state is IDLE.
   */
  @Nullable
  ClientTransport obtainActiveTransport() {
    ClientTransport savedTransport = activeTransport;
    if (savedTransport != null) {
      return savedTransport;
    }
    try {
      synchronized (lock) {
        savedTransport = activeTransport;
        // Check again, since it could have changed before acquiring the lock
        if (savedTransport != null) {
          return savedTransport;
        }
        if (state.getState() == IDLE) {
          gotoNonErrorState(CONNECTING);
          startNewTransport();
        }
      }
    } finally {
      channelExecutor.drain();
    }
    return null;
  }

  @GuardedBy("lock")
  private void startNewTransport() {
    Preconditions.checkState(reconnectTask == null, "Should have no reconnectTask scheduled");

    if (addressIndex.isAtBeginning()) {
      connectingTimer.reset().start();
    }
    SocketAddress address = addressIndex.getCurrentAddress();

    ProxyParameters proxy = null;
    if (address instanceof ProxySocketAddress) {
      proxy = ((ProxySocketAddress) address).getProxyParameters();
      address = ((ProxySocketAddress) address).getAddress();
    }

    ClientTransportFactory.ClientTransportOptions options =
        new ClientTransportFactory.ClientTransportOptions()
          .setAuthority(authority)
          .setEagAttributes(addressIndex.getCurrentEagAttributes())
          .setUserAgent(userAgent)
          .setProxyParameters(proxy);
    ConnectionClientTransport transport =
        new CallTracingTransport(
            transportFactory.newClientTransport(address, options), callsTracer);
    channelz.addClientSocket(transport);
    if (log.isLoggable(Level.FINE)) {
      log.log(Level.FINE, "[{0}] Created {1} for {2}",
          new Object[] {logId, transport.getLogId(), address});
    }
    pendingTransport = transport;
    transports.add(transport);
    Runnable runnable = transport.start(new TransportListener(transport, address));
    if (runnable != null) {
      channelExecutor.executeLater(runnable);
    }
  }

  /**
   * Only called after all addresses attempted and failed (TRANSIENT_FAILURE).
   * @param status the causal status when the channel begins transition to
   *     TRANSIENT_FAILURE.
   */
  @GuardedBy("lock")
  private void scheduleBackoff(final Status status) {
    class EndOfCurrentBackoff implements Runnable {
      @Override
      public void run() {
        try {
          synchronized (lock) {
            reconnectTask = null;
            if (reconnectCanceled) {
              // Even though cancelReconnectTask() will cancel this task, the task may have already
              // started when it's being canceled.
              return;
            }
            gotoNonErrorState(CONNECTING);
            startNewTransport();
          }
        } catch (Throwable t) {
          log.log(Level.WARNING, "Exception handling end of backoff", t);
        } finally {
          channelExecutor.drain();
        }
      }
    }

    gotoState(ConnectivityStateInfo.forTransientFailure(status));
    if (reconnectPolicy == null) {
      reconnectPolicy = backoffPolicyProvider.get();
    }
    long delayNanos =
        reconnectPolicy.nextBackoffNanos() - connectingTimer.elapsed(TimeUnit.NANOSECONDS);
    if (log.isLoggable(Level.FINE)) {
      log.log(Level.FINE, "[{0}] Scheduling backoff for {1} ns", new Object[]{logId, delayNanos});
    }
    Preconditions.checkState(reconnectTask == null, "previous reconnectTask is not done");
    reconnectCanceled = false;
    reconnectTask = scheduledExecutor.schedule(
        new LogExceptionRunnable(new EndOfCurrentBackoff()),
        delayNanos,
        TimeUnit.NANOSECONDS);
  }

  /**
   * Immediately attempt to reconnect if the current state is TRANSIENT_FAILURE. Otherwise this
   * method has no effect.
   */
  void resetConnectBackoff() {
    try {
      synchronized (lock) {
        if (state.getState() != TRANSIENT_FAILURE) {
          return;
        }
        cancelReconnectTask();
        gotoNonErrorState(CONNECTING);
        startNewTransport();
      }
    } finally {
      channelExecutor.drain();
    }
  }

  @GuardedBy("lock")
  private void gotoNonErrorState(ConnectivityState newState) {
    gotoState(ConnectivityStateInfo.forNonError(newState));
  }

  @GuardedBy("lock")
  private void gotoState(final ConnectivityStateInfo newState) {
    if (state.getState() != newState.getState()) {
      Preconditions.checkState(state.getState() != SHUTDOWN,
          "Cannot transition out of SHUTDOWN to " + newState);
      state = newState;
      if (channelTracer != null) {
        channelTracer.reportEvent(
            new ChannelTrace.Event.Builder()
                .setDescription("Entering " + state + " state")
                .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
                .setTimestampNanos(timeProvider.currentTimeNanos())
                .build());
      }
      channelExecutor.executeLater(new Runnable() {
          @Override
          public void run() {
            callback.onStateChange(InternalSubchannel.this, newState);
          }
        });
    }
  }

  /** Replaces the existing addresses, avoiding unnecessary reconnects. */
  public void updateAddresses(List<EquivalentAddressGroup> newAddressGroups) {
    Preconditions.checkNotNull(newAddressGroups, "newAddressGroups");
    checkListHasNoNulls(newAddressGroups, "newAddressGroups contains null entry");
    Preconditions.checkArgument(!newAddressGroups.isEmpty(), "newAddressGroups is empty");
    newAddressGroups =
        Collections.unmodifiableList(new ArrayList<>(newAddressGroups));
    ManagedClientTransport savedTransport = null;
    try {
      synchronized (lock) {
        SocketAddress previousAddress = addressIndex.getCurrentAddress();
        addressIndex.updateGroups(newAddressGroups);
        if (state.getState() == READY || state.getState() == CONNECTING) {
          if (!addressIndex.seekTo(previousAddress)) {
            // Forced to drop the connection
            if (state.getState() == READY) {
              savedTransport = activeTransport;
              activeTransport = null;
              addressIndex.reset();
              gotoNonErrorState(IDLE);
            } else {
              savedTransport = pendingTransport;
              pendingTransport = null;
              addressIndex.reset();
              startNewTransport();
            }
          }
        }
      }
    } finally {
      channelExecutor.drain();
    }
    if (savedTransport != null) {
      savedTransport.shutdown(
          Status.UNAVAILABLE.withDescription(
              "InternalSubchannel closed transport due to address change"));
    }
  }

  public void shutdown(Status reason) {
    ManagedClientTransport savedActiveTransport;
    ConnectionClientTransport savedPendingTransport;
    try {
      synchronized (lock) {
        if (state.getState() == SHUTDOWN) {
          return;
        }
        shutdownReason = reason;
        gotoNonErrorState(SHUTDOWN);
        savedActiveTransport = activeTransport;
        savedPendingTransport = pendingTransport;
        activeTransport = null;
        pendingTransport = null;
        addressIndex.reset();
        if (transports.isEmpty()) {
          handleTermination();
          if (log.isLoggable(Level.FINE)) {
            log.log(Level.FINE, "[{0}] Terminated in shutdown()", logId);
          }
        }  // else: the callback will be run once all transports have been terminated
        cancelReconnectTask();
      }
    } finally {
      channelExecutor.drain();
    }
    if (savedActiveTransport != null) {
      savedActiveTransport.shutdown(reason);
    }
    if (savedPendingTransport != null) {
      savedPendingTransport.shutdown(reason);
    }
  }

  @Override
  public String toString() {
    // addressGroupsCopy being a little stale is fine, just avoid calling toString with the lock
    // since there may be many addresses.
    Object addressGroupsCopy;
    synchronized (lock) {
      addressGroupsCopy = addressIndex.getGroups();
    }
    return MoreObjects.toStringHelper(this)
          .add("logId", logId.getId())
          .add("addressGroups", addressGroupsCopy)
          .toString();
  }

  @GuardedBy("lock")
  private void handleTermination() {
    channelExecutor.executeLater(new Runnable() {
        @Override
        public void run() {
          callback.onTerminated(InternalSubchannel.this);
        }
      });
  }

  private void handleTransportInUseState(
      final ConnectionClientTransport transport, final boolean inUse) {
    channelExecutor.executeLater(new Runnable() {
        @Override
        public void run() {
          inUseStateAggregator.updateObjectInUse(transport, inUse);
        }
      }).drain();
  }

  void shutdownNow(Status reason) {
    shutdown(reason);
    Collection<ManagedClientTransport> transportsCopy;
    try {
      synchronized (lock) {
        transportsCopy = new ArrayList<ManagedClientTransport>(transports);
      }
    } finally {
      channelExecutor.drain();
    }
    for (ManagedClientTransport transport : transportsCopy) {
      transport.shutdownNow(reason);
    }
  }

  List<EquivalentAddressGroup> getAddressGroups() {
    try {
      synchronized (lock) {
        return addressIndex.getGroups();
      }
    } finally {
      channelExecutor.drain();
    }
  }

  @GuardedBy("lock")
  private void cancelReconnectTask() {
    if (reconnectTask != null) {
      reconnectTask.cancel(false);
      reconnectCanceled = true;
      reconnectTask = null;
      reconnectPolicy = null;
    }
  }

  @Override
  public InternalLogId getLogId() {
    return logId;
  }


  @Override
  public ListenableFuture<ChannelStats> getStats() {
    SettableFuture<ChannelStats> ret = SettableFuture.create();
    ChannelStats.Builder builder = new ChannelStats.Builder();

    List<EquivalentAddressGroup> addressGroupsSnapshot;
    List<InternalWithLogId> transportsSnapshot;
    synchronized (lock) {
      addressGroupsSnapshot = addressIndex.getGroups();
      transportsSnapshot = new ArrayList<InternalWithLogId>(transports);
    }

    builder.setTarget(addressGroupsSnapshot.toString()).setState(getState());
    builder.setSockets(transportsSnapshot);
    callsTracer.updateBuilder(builder);
    if (channelTracer != null) {
      channelTracer.updateBuilder(builder);
    }
    ret.set(builder.build());
    return ret;
  }

  @VisibleForTesting
  ConnectivityState getState() {
    try {
      synchronized (lock) {
        return state.getState();
      }
    } finally {
      channelExecutor.drain();
    }
  }

  private static void checkListHasNoNulls(List<?> list, String msg) {
    for (Object item : list) {
      Preconditions.checkNotNull(item, msg);
    }
  }

  /** Listener for real transports. */
  private class TransportListener implements ManagedClientTransport.Listener {
    final ConnectionClientTransport transport;
    final SocketAddress address;

    TransportListener(ConnectionClientTransport transport, SocketAddress address) {
      this.transport = transport;
      this.address = address;
    }

    @Override
    public void transportReady() {
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] {1} for {2} is ready",
            new Object[] {logId, transport.getLogId(), address});
      }
      Status savedShutdownReason;
      try {
        synchronized (lock) {
          savedShutdownReason = shutdownReason;
          reconnectPolicy = null;
          if (savedShutdownReason != null) {
            // activeTransport should have already been set to null by shutdown(). We keep it null.
            Preconditions.checkState(activeTransport == null,
                "Unexpected non-null activeTransport");
          } else if (pendingTransport == transport) {
            gotoNonErrorState(READY);
            activeTransport = transport;
            pendingTransport = null;
          }
        }
      } finally {
        channelExecutor.drain();
      }
      if (savedShutdownReason != null) {
        transport.shutdown(savedShutdownReason);
      }
    }

    @Override
    public void transportInUse(boolean inUse) {
      handleTransportInUseState(transport, inUse);
    }

    @Override
    public void transportShutdown(Status s) {
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] {1} for {2} is being shutdown with status {3}",
            new Object[] {logId, transport.getLogId(), address, s});
      }
      try {
        synchronized (lock) {
          if (state.getState() == SHUTDOWN) {
            return;
          }
          if (activeTransport == transport) {
            gotoNonErrorState(IDLE);
            activeTransport = null;
            addressIndex.reset();
          } else if (pendingTransport == transport) {
            Preconditions.checkState(state.getState() == CONNECTING,
                "Expected state is CONNECTING, actual state is %s", state.getState());
            addressIndex.increment();
            // Continue reconnect if there are still addresses to try.
            if (!addressIndex.isValid()) {
              pendingTransport = null;
              addressIndex.reset();
              // Initiate backoff
              // Transition to TRANSIENT_FAILURE
              scheduleBackoff(s);
            } else {
              startNewTransport();
            }
          }
        }
      } finally {
        channelExecutor.drain();
      }
    }

    @Override
    public void transportTerminated() {
      if (log.isLoggable(Level.FINE)) {
        log.log(Level.FINE, "[{0}] {1} for {2} is terminated",
            new Object[] {logId, transport.getLogId(), address});
      }
      channelz.removeClientSocket(transport);
      handleTransportInUseState(transport, false);
      try {
        synchronized (lock) {
          transports.remove(transport);
          if (state.getState() == SHUTDOWN && transports.isEmpty()) {
            if (log.isLoggable(Level.FINE)) {
              log.log(Level.FINE, "[{0}] Terminated in transportTerminated()", logId);
            }
            handleTermination();
          }
        }
      } finally {
        channelExecutor.drain();
      }
      Preconditions.checkState(activeTransport != transport,
          "activeTransport still points to this transport. "
          + "Seems transportShutdown() was not called.");
    }
  }

  // All methods are called in channelExecutor, which is a serializing executor.
  abstract static class Callback {
    /**
     * Called when the subchannel is terminated, which means it's shut down and all transports
     * have been terminated.
     */
    @ForOverride
    void onTerminated(InternalSubchannel is) { }

    /**
     * Called when the subchannel's connectivity state has changed.
     */
    @ForOverride
    void onStateChange(InternalSubchannel is, ConnectivityStateInfo newState) { }

    /**
     * Called when the subchannel's in-use state has changed to true, which means at least one
     * transport is in use.
     */
    @ForOverride
    void onInUse(InternalSubchannel is) { }

    /**
     * Called when the subchannel's in-use state has changed to false, which means no transport is
     * in use.
     */
    @ForOverride
    void onNotInUse(InternalSubchannel is) { }
  }

  @VisibleForTesting
  static final class CallTracingTransport extends ForwardingConnectionClientTransport {
    private final ConnectionClientTransport delegate;
    private final CallTracer callTracer;

    private CallTracingTransport(ConnectionClientTransport delegate, CallTracer callTracer) {
      this.delegate = delegate;
      this.callTracer = callTracer;
    }

    @Override
    protected ConnectionClientTransport delegate() {
      return delegate;
    }

    @Override
    public ClientStream newStream(
        MethodDescriptor<?, ?> method, Metadata headers, CallOptions callOptions) {
      final ClientStream streamDelegate = super.newStream(method, headers, callOptions);
      return new ForwardingClientStream() {
        @Override
        protected ClientStream delegate() {
          return streamDelegate;
        }

        @Override
        public void start(final ClientStreamListener listener) {
          callTracer.reportCallStarted();
          super.start(new ForwardingClientStreamListener() {
            @Override
            protected ClientStreamListener delegate() {
              return listener;
            }

            @Override
            public void closed(Status status, Metadata trailers) {
              callTracer.reportCallEnded(status.isOk());
              super.closed(status, trailers);
            }

            @Override
            public void closed(
                Status status, RpcProgress rpcProgress, Metadata trailers) {
              callTracer.reportCallEnded(status.isOk());
              super.closed(status, rpcProgress, trailers);
            }
          });
        }
      };
    }
  }

  /** Index as in 'i', the pointer to an entry. Not a "search index." */
  @VisibleForTesting
  static final class Index {
    private List<EquivalentAddressGroup> addressGroups;
    private int groupIndex;
    private int addressIndex;

    public Index(List<EquivalentAddressGroup> groups) {
      this.addressGroups = groups;
    }

    public boolean isValid() {
      // addressIndex will never be invalid
      return groupIndex < addressGroups.size();
    }

    public boolean isAtBeginning() {
      return groupIndex == 0 && addressIndex == 0;
    }

    public void increment() {
      EquivalentAddressGroup group = addressGroups.get(groupIndex);
      addressIndex++;
      if (addressIndex >= group.getAddresses().size()) {
        groupIndex++;
        addressIndex = 0;
      }
    }

    public void reset() {
      groupIndex = 0;
      addressIndex = 0;
    }

    public SocketAddress getCurrentAddress() {
      return addressGroups.get(groupIndex).getAddresses().get(addressIndex);
    }

    public Attributes getCurrentEagAttributes() {
      return addressGroups.get(groupIndex).getAttributes();
    }

    public List<EquivalentAddressGroup> getGroups() {
      return addressGroups;
    }

    /** Update to new groups, resetting the current index. */
    public void updateGroups(List<EquivalentAddressGroup> newGroups) {
      addressGroups = newGroups;
      reset();
    }

    /** Returns false if the needle was not found and the current index was left unchanged. */
    public boolean seekTo(SocketAddress needle) {
      for (int i = 0; i < addressGroups.size(); i++) {
        EquivalentAddressGroup group = addressGroups.get(i);
        int j = group.getAddresses().indexOf(needle);
        if (j == -1) {
          continue;
        }
        this.groupIndex = i;
        this.addressIndex = j;
        return true;
      }
      return false;
    }
  }
}
