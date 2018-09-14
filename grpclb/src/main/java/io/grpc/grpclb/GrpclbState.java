/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.SHUTDOWN;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import com.google.protobuf.util.Durations;
import io.grpc.Attributes;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.InternalLogId;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.BackoffPolicy;
import io.grpc.internal.GrpcAttributes;
import io.grpc.internal.TimeProvider;
import io.grpc.lb.v1.ClientStats;
import io.grpc.lb.v1.InitialLoadBalanceRequest;
import io.grpc.lb.v1.InitialLoadBalanceResponse;
import io.grpc.lb.v1.LoadBalanceRequest;
import io.grpc.lb.v1.LoadBalanceResponse;
import io.grpc.lb.v1.LoadBalanceResponse.LoadBalanceResponseTypeCase;
import io.grpc.lb.v1.LoadBalancerGrpc;
import io.grpc.lb.v1.Server;
import io.grpc.lb.v1.ServerList;
import io.grpc.stub.StreamObserver;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Map.Entry;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;
import javax.annotation.concurrent.NotThreadSafe;

/**
 * The states of a GRPCLB working session of {@link GrpclbLoadBalancer}.  Created when
 * GrpclbLoadBalancer switches to GRPCLB mode.  Closed and discarded when GrpclbLoadBalancer
 * switches away from GRPCLB mode.
 */
@NotThreadSafe
final class GrpclbState {
  private static final Logger logger = Logger.getLogger(GrpclbState.class.getName());

  static final long FALLBACK_TIMEOUT_MS = TimeUnit.SECONDS.toMillis(10);
  private static final Attributes LB_PROVIDED_BACKEND_ATTRS =
      Attributes.newBuilder().set(GrpcAttributes.ATTR_LB_PROVIDED_BACKEND, true).build();

  @VisibleForTesting
  static final PickResult DROP_PICK_RESULT =
      PickResult.withDrop(Status.UNAVAILABLE.withDescription("Dropped as requested by balancer"));

  @VisibleForTesting
  static final RoundRobinEntry BUFFER_ENTRY = new RoundRobinEntry() {
      @Override
      public PickResult picked(Metadata headers) {
        return PickResult.withNoResult();
      }

      @Override
      public String toString() {
        return "BUFFER_ENTRY";
      }
    };

  private final InternalLogId logId;
  private final String serviceName;
  private final Helper helper;
  private final SubchannelPool subchannelPool;
  private final TimeProvider time;
  private final ScheduledExecutorService timerService;

  private static final Attributes.Key<AtomicReference<ConnectivityStateInfo>> STATE_INFO =
      Attributes.Key.create("io.grpc.grpclb.GrpclbLoadBalancer.stateInfo");
  private final BackoffPolicy.Provider backoffPolicyProvider;

  // Scheduled only once.  Never reset.
  @Nullable
  private FallbackModeTask fallbackTimer;
  private List<EquivalentAddressGroup> fallbackBackendList = Collections.emptyList();
  private boolean usingFallbackBackends;
  // True if the current balancer has returned a serverlist.  Will be reset to false when lost
  // connection to a balancer.
  private boolean balancerWorking;
  @Nullable
  private BackoffPolicy lbRpcRetryPolicy;
  @Nullable
  private LbRpcRetryTask lbRpcRetryTimer;
  private long prevLbRpcStartNanos;

  @Nullable
  private ManagedChannel lbCommChannel;

  @Nullable
  private LbStream lbStream;
  private Map<EquivalentAddressGroup, Subchannel> subchannels = Collections.emptyMap();

  // Has the same size as the round-robin list from the balancer.
  // A drop entry from the round-robin list becomes a DropEntry here.
  // A backend entry from the robin-robin list becomes a null here.
  private List<DropEntry> dropList = Collections.emptyList();
  // Contains only non-drop, i.e., backends from the round-robin list from the balancer.
  private List<BackendEntry> backendList = Collections.emptyList();
  private RoundRobinPicker currentPicker =
      new RoundRobinPicker(Collections.<DropEntry>emptyList(), Arrays.asList(BUFFER_ENTRY));

  GrpclbState(
      Helper helper,
      SubchannelPool subchannelPool,
      TimeProvider time,
      ScheduledExecutorService timerService,
      BackoffPolicy.Provider backoffPolicyProvider,
      InternalLogId logId) {
    this.helper = checkNotNull(helper, "helper");
    this.subchannelPool = checkNotNull(subchannelPool, "subchannelPool");
    this.time = checkNotNull(time, "time provider");
    this.timerService = checkNotNull(timerService, "timerService");
    this.backoffPolicyProvider = checkNotNull(backoffPolicyProvider, "backoffPolicyProvider");
    this.serviceName = checkNotNull(helper.getAuthority(), "helper returns null authority");
    this.logId = checkNotNull(logId, "logId");
  }

  void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo newState) {
    if (newState.getState() == SHUTDOWN || !(subchannels.values().contains(subchannel))) {
      return;
    }
    if (newState.getState() == IDLE) {
      subchannel.requestConnection();
    }
    subchannel.getAttributes().get(STATE_INFO).set(newState);
    maybeUseFallbackBackends();
    maybeUpdatePicker();
  }

  /**
   * Handle new addresses of the balancer and backends from the resolver, and create connection if
   * not yet connected.
   */
  void handleAddresses(
      List<LbAddressGroup> newLbAddressGroups, List<EquivalentAddressGroup> newBackendServers) {
    if (newLbAddressGroups.isEmpty()) {
      propagateError(Status.UNAVAILABLE.withDescription(
              "NameResolver returned no LB address while asking for GRPCLB"));
      return;
    }
    LbAddressGroup newLbAddressGroup = flattenLbAddressGroups(newLbAddressGroups);
    startLbComm(newLbAddressGroup);
    // Avoid creating a new RPC just because the addresses were updated, as it can cause a
    // stampeding herd. The current RPC may be on a connection to an address not present in
    // newLbAddressGroups, but we're considering that "okay". If we detected the RPC is to an
    // outdated backend, we could choose to re-create the RPC.
    if (lbStream == null) {
      startLbRpc();
    }
    fallbackBackendList = newBackendServers;
    // Start the fallback timer if it's never started
    if (fallbackTimer == null) {
      logger.log(Level.FINE, "[{0}] Starting fallback timer.", new Object[] {logId});
      fallbackTimer = new FallbackModeTask();
      fallbackTimer.schedule();
    }
    if (usingFallbackBackends) {
      // Populate the new fallback backends to round-robin list.
      useFallbackBackends();
    }
    maybeUpdatePicker();
  }

  private void maybeUseFallbackBackends() {
    if (balancerWorking) {
      return;
    }
    if (usingFallbackBackends) {
      return;
    }
    if (fallbackTimer != null && !fallbackTimer.discarded) {
      return;
    }
    int numReadySubchannels = 0;
    for (Subchannel subchannel : subchannels.values()) {
      if (subchannel.getAttributes().get(STATE_INFO).get().getState() == READY) {
        numReadySubchannels++;
      }
    }
    if (numReadySubchannels > 0) {
      return;
    }
    // Fallback contiditions met
    useFallbackBackends();
  }

  /**
   * Populate the round-robin lists with the fallback backends.
   */
  private void useFallbackBackends() {
    usingFallbackBackends = true;
    logger.log(Level.INFO, "[{0}] Using fallback: {1}", new Object[] {logId, fallbackBackendList});

    List<DropEntry> newDropList = new ArrayList<>();
    List<BackendAddressGroup> newBackendAddrList = new ArrayList<>();
    for (EquivalentAddressGroup eag : fallbackBackendList) {
      newDropList.add(null);
      newBackendAddrList.add(new BackendAddressGroup(eag, null));
    }
    useRoundRobinLists(newDropList, newBackendAddrList, null);
  }

  private void shutdownLbComm() {
    if (lbCommChannel != null) {
      lbCommChannel.shutdown();
      lbCommChannel = null;
    }
    shutdownLbRpc();
  }

  private void shutdownLbRpc() {
    if (lbStream != null) {
      lbStream.close(null);
      // lbStream will be set to null in LbStream.cleanup()
    }
  }

  private void startLbComm(LbAddressGroup lbAddressGroup) {
    checkNotNull(lbAddressGroup, "lbAddressGroup");
    if (lbCommChannel == null) {
      lbCommChannel = helper.createOobChannel(
          lbAddressGroup.getAddresses(), lbAddressGroup.getAuthority());
    } else if (lbAddressGroup.getAuthority().equals(lbCommChannel.authority())) {
      helper.updateOobChannelAddresses(lbCommChannel, lbAddressGroup.getAddresses());
    } else {
      // Full restart of channel
      shutdownLbComm();
      lbCommChannel = helper.createOobChannel(
          lbAddressGroup.getAddresses(), lbAddressGroup.getAuthority());
    }
  }

  private void startLbRpc() {
    checkState(lbStream == null, "previous lbStream has not been cleared yet");
    LoadBalancerGrpc.LoadBalancerStub stub = LoadBalancerGrpc.newStub(lbCommChannel);
    lbStream = new LbStream(stub);
    lbStream.start();
    prevLbRpcStartNanos = time.currentTimeNanos();

    LoadBalanceRequest initRequest = LoadBalanceRequest.newBuilder()
        .setInitialRequest(InitialLoadBalanceRequest.newBuilder()
            .setName(serviceName).build())
        .build();
    try {
      lbStream.lbRequestWriter.onNext(initRequest);
    } catch (Exception e) {
      lbStream.close(e);
    }
  }

  private void cancelFallbackTimer() {
    if (fallbackTimer != null) {
      fallbackTimer.cancel();
    }
  }

  private void cancelLbRpcRetryTimer() {
    if (lbRpcRetryTimer != null) {
      lbRpcRetryTimer.cancel();
    }
  }

  void shutdown() {
    shutdownLbComm();
    // We close the subchannels through subchannelPool instead of helper just for convenience of
    // testing.
    for (Subchannel subchannel : subchannels.values()) {
      subchannelPool.returnSubchannel(subchannel);
    }
    subchannels = Collections.emptyMap();
    subchannelPool.clear();
    cancelFallbackTimer();
    cancelLbRpcRetryTimer();
  }

  void propagateError(Status status) {
    logger.log(Level.FINE, "[{0}] Had an error: {1}; dropList={2}; backendList={3}",
        new Object[] {logId, status, dropList, backendList});
    if (backendList.isEmpty()) {
      maybeUpdatePicker(
          TRANSIENT_FAILURE, new RoundRobinPicker(dropList, Arrays.asList(new ErrorEntry(status))));
    }
  }

  @VisibleForTesting
  @Nullable
  GrpclbClientLoadRecorder getLoadRecorder() {
    if (lbStream == null) {
      return null;
    }
    return lbStream.loadRecorder;
  }

  /**
   * Populate the round-robin lists with the given values.
   */
  private void useRoundRobinLists(
      List<DropEntry> newDropList, List<BackendAddressGroup> newBackendAddrList,
      @Nullable GrpclbClientLoadRecorder loadRecorder) {
    logger.log(Level.FINE, "[{0}] Using round-robin list: {1}, droplist={2}",
         new Object[] {logId, newBackendAddrList, newDropList});
    HashMap<EquivalentAddressGroup, Subchannel> newSubchannelMap =
        new HashMap<EquivalentAddressGroup, Subchannel>();
    List<BackendEntry> newBackendList = new ArrayList<>();

    for (BackendAddressGroup backendAddr : newBackendAddrList) {
      EquivalentAddressGroup eag = backendAddr.getAddresses();
      Subchannel subchannel = newSubchannelMap.get(eag);
      if (subchannel == null) {
        subchannel = subchannels.get(eag);
        if (subchannel == null) {
          Attributes subchannelAttrs = Attributes.newBuilder()
              .set(STATE_INFO,
                  new AtomicReference<ConnectivityStateInfo>(
                      ConnectivityStateInfo.forNonError(IDLE)))
              .build();
          subchannel = subchannelPool.takeOrCreateSubchannel(eag, subchannelAttrs);
          subchannel.requestConnection();
        }
        newSubchannelMap.put(eag, subchannel);
      }
      BackendEntry entry;
      // Only picks with tokens are reported to LoadRecorder
      if (backendAddr.getToken() == null) {
        entry = new BackendEntry(subchannel);
      } else {
        entry = new BackendEntry(subchannel, loadRecorder, backendAddr.getToken());
      }
      newBackendList.add(entry);
    }

    // Close Subchannels whose addresses have been delisted
    for (Entry<EquivalentAddressGroup, Subchannel> entry : subchannels.entrySet()) {
      EquivalentAddressGroup eag = entry.getKey();
      if (!newSubchannelMap.containsKey(eag)) {
        subchannelPool.returnSubchannel(entry.getValue());
      }
    }

    subchannels = Collections.unmodifiableMap(newSubchannelMap);
    dropList = Collections.unmodifiableList(newDropList);
    backendList = Collections.unmodifiableList(newBackendList);
  }

  @VisibleForTesting
  class FallbackModeTask implements Runnable {
    private ScheduledFuture<?> scheduledFuture;
    private boolean discarded;

    @Override
    public void run() {
      helper.runSerialized(new Runnable() {
          @Override
          public void run() {
            checkState(fallbackTimer == FallbackModeTask.this, "fallback timer mismatch");
            discarded = true;
            maybeUseFallbackBackends();
            maybeUpdatePicker();
          }
        });
    }

    void cancel() {
      discarded = true;
      scheduledFuture.cancel(false);
    }

    void schedule() {
      checkState(scheduledFuture == null, "FallbackModeTask already scheduled");
      scheduledFuture = timerService.schedule(this, FALLBACK_TIMEOUT_MS, TimeUnit.MILLISECONDS);
    }
  }

  @VisibleForTesting
  class LbRpcRetryTask implements Runnable {
    private ScheduledFuture<?> scheduledFuture;

    @Override
    public void run() {
      helper.runSerialized(new Runnable() {
          @Override
          public void run() {
            checkState(
                lbRpcRetryTimer == LbRpcRetryTask.this, "LbRpc retry timer mismatch");
            startLbRpc();
          }
        });
    }

    void cancel() {
      scheduledFuture.cancel(false);
    }

    void schedule(long delayNanos) {
      checkState(scheduledFuture == null, "LbRpcRetryTask already scheduled");
      scheduledFuture = timerService.schedule(this, delayNanos, TimeUnit.NANOSECONDS);
    }
  }

  @VisibleForTesting
  class LoadReportingTask implements Runnable {
    private final LbStream stream;

    LoadReportingTask(LbStream stream) {
      this.stream = stream;
    }

    @Override
    public void run() {
      helper.runSerialized(new Runnable() {
          @Override
          public void run() {
            stream.loadReportFuture = null;
            stream.sendLoadReport();
          }
        });
    }
  }

  private class LbStream implements StreamObserver<LoadBalanceResponse> {
    final GrpclbClientLoadRecorder loadRecorder;
    final LoadBalancerGrpc.LoadBalancerStub stub;
    StreamObserver<LoadBalanceRequest> lbRequestWriter;

    // These fields are only accessed from helper.runSerialized()
    boolean initialResponseReceived;
    boolean closed;
    long loadReportIntervalMillis = -1;
    ScheduledFuture<?> loadReportFuture;

    LbStream(LoadBalancerGrpc.LoadBalancerStub stub) {
      this.stub = checkNotNull(stub, "stub");
      // Stats data only valid for current LbStream.  We do not carry over data from previous
      // stream.
      loadRecorder = new GrpclbClientLoadRecorder(time);
    }

    void start() {
      lbRequestWriter = stub.withWaitForReady().balanceLoad(this);
    }

    @Override public void onNext(final LoadBalanceResponse response) {
      helper.runSerialized(new Runnable() {
          @Override
          public void run() {
            handleResponse(response);
          }
        });
    }

    @Override public void onError(final Throwable error) {
      helper.runSerialized(new Runnable() {
          @Override
          public void run() {
            handleStreamClosed(Status.fromThrowable(error)
                .augmentDescription("Stream to GRPCLB LoadBalancer had an error"));
          }
        });
    }

    @Override public void onCompleted() {
      helper.runSerialized(new Runnable() {
          @Override
          public void run() {
            handleStreamClosed(
                Status.UNAVAILABLE.withDescription("Stream to GRPCLB LoadBalancer was closed"));
          }
        });
    }

    // Following methods must be run in helper.runSerialized()

    private void sendLoadReport() {
      if (closed) {
        return;
      }
      ClientStats stats = loadRecorder.generateLoadReport();
      // TODO(zhangkun83): flow control?
      try {
        lbRequestWriter.onNext(LoadBalanceRequest.newBuilder().setClientStats(stats).build());
        scheduleNextLoadReport();
      } catch (Exception e) {
        close(e);
      }
    }

    private void scheduleNextLoadReport() {
      if (loadReportIntervalMillis > 0) {
        loadReportFuture = timerService.schedule(
            new LoadReportingTask(this), loadReportIntervalMillis, TimeUnit.MILLISECONDS);
      }
    }

    private void handleResponse(LoadBalanceResponse response) {
      if (closed) {
        return;
      }
      logger.log(Level.FINER, "[{0}] Got an LB response: {1}", new Object[] {logId, response});

      LoadBalanceResponseTypeCase typeCase = response.getLoadBalanceResponseTypeCase();
      if (!initialResponseReceived) {
        if (typeCase != LoadBalanceResponseTypeCase.INITIAL_RESPONSE) {
          logger.log(
              Level.WARNING,
              "[{0}] : Did not receive response with type initial response: {1}",
              new Object[] {logId, response});
          return;
        }
        initialResponseReceived = true;
        InitialLoadBalanceResponse initialResponse = response.getInitialResponse();
        loadReportIntervalMillis =
            Durations.toMillis(initialResponse.getClientStatsReportInterval());
        scheduleNextLoadReport();
        return;
      }

      if (typeCase != LoadBalanceResponseTypeCase.SERVER_LIST) {
        logger.log(
            Level.WARNING,
            "[{0}] : Ignoring unexpected response type: {1}",
            new Object[] {logId, response});
        return;
      }

      balancerWorking = true;
      // TODO(zhangkun83): handle delegate from initialResponse
      ServerList serverList = response.getServerList();
      List<DropEntry> newDropList = new ArrayList<>();
      List<BackendAddressGroup> newBackendAddrList = new ArrayList<>();
      // Construct the new collections. Create new Subchannels when necessary.
      for (Server server : serverList.getServersList()) {
        String token = server.getLoadBalanceToken();
        if (server.getDrop()) {
          newDropList.add(new DropEntry(loadRecorder, token));
        } else {
          newDropList.add(null);
          InetSocketAddress address;
          try {
            address = new InetSocketAddress(
                InetAddress.getByAddress(server.getIpAddress().toByteArray()), server.getPort());
          } catch (UnknownHostException e) {
            propagateError(
                Status.UNAVAILABLE
                    .withDescription("Host for server not found: " + server)
                    .withCause(e));
            continue;
          }
          // ALTS code can use the presence of ATTR_LB_PROVIDED_BACKEND to select ALTS instead of
          // TLS, with Netty.
          EquivalentAddressGroup eag =
              new EquivalentAddressGroup(address, LB_PROVIDED_BACKEND_ATTRS);
          newBackendAddrList.add(new BackendAddressGroup(eag, token));
        }
      }
      // Stop using fallback backends as soon as a new server list is received from the balancer.
      usingFallbackBackends = false;
      cancelFallbackTimer();
      useRoundRobinLists(newDropList, newBackendAddrList, loadRecorder);
      maybeUpdatePicker();
    }

    private void handleStreamClosed(Status error) {
      checkArgument(!error.isOk(), "unexpected OK status");
      if (closed) {
        return;
      }
      closed = true;
      cleanUp();
      propagateError(error);
      balancerWorking = false;
      maybeUseFallbackBackends();
      maybeUpdatePicker();

      long delayNanos = 0;
      if (initialResponseReceived || lbRpcRetryPolicy == null) {
        // Reset the backoff sequence if balancer has sent the initial response, or backoff sequence
        // has never been initialized.
        lbRpcRetryPolicy = backoffPolicyProvider.get();
      }
      // Backoff only when balancer wasn't working previously.
      if (!initialResponseReceived) {
        // The back-off policy determines the interval between consecutive RPC upstarts, thus the
        // actual delay may be smaller than the value from the back-off policy, or even negative,
        // depending how much time was spent in the previous RPC.
        delayNanos =
            prevLbRpcStartNanos + lbRpcRetryPolicy.nextBackoffNanos() - time.currentTimeNanos();
      }
      if (delayNanos <= 0) {
        startLbRpc();
      } else {
        lbRpcRetryTimer = new LbRpcRetryTask();
        lbRpcRetryTimer.schedule(delayNanos);
      }
    }

    void close(@Nullable Exception error) {
      if (closed) {
        return;
      }
      closed = true;
      cleanUp();
      try {
        if (error == null) {
          lbRequestWriter.onCompleted();
        } else {
          lbRequestWriter.onError(error);
        }
      } catch (Exception e) {
        // Don't care
      }
    }

    private void cleanUp() {
      if (loadReportFuture != null) {
        loadReportFuture.cancel(false);
        loadReportFuture = null;
      }
      if (lbStream == this) {
        lbStream = null;
      }
    }
  }

  /**
   * Make and use a picker out of the current lists and the states of subchannels if they have
   * changed since the last picker created.
   */
  private void maybeUpdatePicker() {
    List<RoundRobinEntry> pickList = new ArrayList<>(backendList.size());
    Status error = null;
    boolean hasIdle = false;
    for (BackendEntry entry : backendList) {
      Subchannel subchannel = entry.result.getSubchannel();
      Attributes attrs = subchannel.getAttributes();
      ConnectivityStateInfo stateInfo = attrs.get(STATE_INFO).get();
      if (stateInfo.getState() == READY) {
        pickList.add(entry);
      } else if (stateInfo.getState() == TRANSIENT_FAILURE) {
        error = stateInfo.getStatus();
      } else if (stateInfo.getState() == IDLE) {
        hasIdle = true;
      }
    }
    ConnectivityState state;
    if (pickList.isEmpty()) {
      if (error != null && !hasIdle) {
        logger.log(Level.FINE, "[{0}] No ready Subchannel. Using error: {1}",
            new Object[] {logId, error});
        pickList.add(new ErrorEntry(error));
        state = TRANSIENT_FAILURE;
      } else {
        logger.log(Level.FINE, "[{0}] No ready Subchannel and still connecting", logId);
        pickList.add(BUFFER_ENTRY);
        state = CONNECTING;
      }
    } else {
      logger.log(
          Level.FINE, "[{0}] Using drop list {1} and pick list {2}",
          new Object[] {logId, dropList, pickList});
      state = READY;
    }
    maybeUpdatePicker(state, new RoundRobinPicker(dropList, pickList));
  }

  /**
   * Update the given picker to the helper if it's different from the current one.
   */
  private void maybeUpdatePicker(ConnectivityState state, RoundRobinPicker picker) {
    // Discard the new picker if we are sure it won't make any difference, in order to save
    // re-processing pending streams, and avoid unnecessary resetting of the pointer in
    // RoundRobinPicker.
    if (picker.dropList.equals(currentPicker.dropList)
        && picker.pickList.equals(currentPicker.pickList)) {
      return;
    }
    // No need to skip ErrorPicker. If the current picker is ErrorPicker, there won't be any pending
    // stream thus no time is wasted in re-process.
    currentPicker = picker;
    helper.updateBalancingState(state, picker);
  }

  private LbAddressGroup flattenLbAddressGroups(List<LbAddressGroup> groupList) {
    assert !groupList.isEmpty();
    List<EquivalentAddressGroup> eags = new ArrayList<>(groupList.size());
    String authority = groupList.get(0).getAuthority();
    for (LbAddressGroup group : groupList) {
      if (!authority.equals(group.getAuthority())) {
        // TODO(ejona): Allow different authorities for different addresses. Requires support from
        // Helper.
        logger.log(Level.WARNING,
            "[{0}] Multiple authorities found for LB. "
            + "Skipping addresses for {0} in preference to {1}",
            new Object[] {logId, group.getAuthority(), authority});
      } else {
        eags.add(group.getAddresses());
      }
    }
    // ALTS code can use the presence of ATTR_LB_ADDR_AUTHORITY to select ALTS instead of TLS, with
    // Netty.
    // TODO(ejona): The process here is a bit of a hack because ATTR_LB_ADDR_AUTHORITY isn't
    // actually used in the normal case. https://github.com/grpc/grpc-java/issues/4618 should allow
    // this to be more obvious.
    Attributes attrs = Attributes.newBuilder()
        .set(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY, authority)
        .build();
    return new LbAddressGroup(flattenEquivalentAddressGroup(eags, attrs), authority);
  }

  /**
   * Flattens list of EquivalentAddressGroup objects into one EquivalentAddressGroup object.
   */
  private static EquivalentAddressGroup flattenEquivalentAddressGroup(
      List<EquivalentAddressGroup> groupList, Attributes attrs) {
    List<SocketAddress> addrs = new ArrayList<>();
    for (EquivalentAddressGroup group : groupList) {
      addrs.addAll(group.getAddresses());
    }
    return new EquivalentAddressGroup(addrs, attrs);
  }

  @VisibleForTesting
  static final class DropEntry {
    private final GrpclbClientLoadRecorder loadRecorder;
    private final String token;

    DropEntry(GrpclbClientLoadRecorder loadRecorder, String token) {
      this.loadRecorder = checkNotNull(loadRecorder, "loadRecorder");
      this.token = checkNotNull(token, "token");
    }

    PickResult picked() {
      loadRecorder.recordDroppedRequest(token);
      return DROP_PICK_RESULT;
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("loadRecorder", loadRecorder)
          .add("token", token)
          .toString();
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(loadRecorder, token);
    }

    @Override
    public boolean equals(Object other) {
      if (!(other instanceof DropEntry)) {
        return false;
      }
      DropEntry that = (DropEntry) other;
      return Objects.equal(loadRecorder, that.loadRecorder) && Objects.equal(token, that.token);
    }
  }

  private interface RoundRobinEntry {
    PickResult picked(Metadata headers);
  }

  @VisibleForTesting
  static final class BackendEntry implements RoundRobinEntry {
    @VisibleForTesting
    final PickResult result;
    @Nullable
    private final GrpclbClientLoadRecorder loadRecorder;
    @Nullable
    private final String token;

    /**
     * Creates a BackendEntry whose usage will be reported to load recorder.
     */
    BackendEntry(Subchannel subchannel, GrpclbClientLoadRecorder loadRecorder, String token) {
      this.result = PickResult.withSubchannel(subchannel, loadRecorder);
      this.loadRecorder = checkNotNull(loadRecorder, "loadRecorder");
      this.token = checkNotNull(token, "token");
    }

    /**
     * Creates a BackendEntry whose usage will not be reported.
     */
    BackendEntry(Subchannel subchannel) {
      this.result = PickResult.withSubchannel(subchannel);
      this.loadRecorder = null;
      this.token = null;
    }

    @Override
    public PickResult picked(Metadata headers) {
      headers.discardAll(GrpclbConstants.TOKEN_METADATA_KEY);
      if (token != null) {
        headers.put(GrpclbConstants.TOKEN_METADATA_KEY, token);
      }
      return result;
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("result", result)
          .add("loadRecorder", loadRecorder)
          .add("token", token)
          .toString();
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(loadRecorder, result, token);
    }

    @Override
    public boolean equals(Object other) {
      if (!(other instanceof BackendEntry)) {
        return false;
      }
      BackendEntry that = (BackendEntry) other;
      return Objects.equal(result, that.result) && Objects.equal(token, that.token)
          && Objects.equal(loadRecorder, that.loadRecorder);
    }
  }

  @VisibleForTesting
  static final class ErrorEntry implements RoundRobinEntry {
    final PickResult result;

    ErrorEntry(Status status) {
      result = PickResult.withError(status);
    }

    @Override
    public PickResult picked(Metadata headers) {
      return result;
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(result);
    }

    @Override
    public boolean equals(Object other) {
      if (!(other instanceof ErrorEntry)) {
        return false;
      }
      return Objects.equal(result, ((ErrorEntry) other).result);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("result", result)
          .toString();
    }
  }

  @VisibleForTesting
  static final class RoundRobinPicker extends SubchannelPicker {
    @VisibleForTesting
    final List<DropEntry> dropList;
    private int dropIndex;

    @VisibleForTesting
    final List<? extends RoundRobinEntry> pickList;
    private int pickIndex;

    // dropList can be empty, which means no drop.
    // pickList must not be empty.
    RoundRobinPicker(List<DropEntry> dropList, List<? extends RoundRobinEntry> pickList) {
      this.dropList = checkNotNull(dropList, "dropList");
      this.pickList = checkNotNull(pickList, "pickList");
      checkArgument(!pickList.isEmpty(), "pickList is empty");
    }

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      synchronized (pickList) {
        // Two-level round-robin.
        // First round-robin on dropList. If a drop entry is selected, request will be dropped.  If
        // a non-drop entry is selected, then round-robin on pickList.  This makes sure requests are
        // dropped at the same proportion as the drop entries appear on the round-robin list from
        // the balancer, while only READY backends (that make up pickList) are selected for the
        // non-drop cases.
        if (!dropList.isEmpty()) {
          DropEntry drop = dropList.get(dropIndex);
          dropIndex++;
          if (dropIndex == dropList.size()) {
            dropIndex = 0;
          }
          if (drop != null) {
            return drop.picked();
          }
        }

        RoundRobinEntry pick = pickList.get(pickIndex);
        pickIndex++;
        if (pickIndex == pickList.size()) {
          pickIndex = 0;
        }
        return pick.picked(args.getHeaders());
      }
    }
  }
}
