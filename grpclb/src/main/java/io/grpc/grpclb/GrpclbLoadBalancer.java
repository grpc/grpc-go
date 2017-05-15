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

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.SHUTDOWN;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import com.google.protobuf.util.Durations;
import io.grpc.Attributes;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.grpclb.GrpclbConstants.LbPolicy;
import io.grpc.grpclb.LoadBalanceResponse.LoadBalanceResponseTypeCase;
import io.grpc.internal.LogId;
import io.grpc.internal.ObjectPool;
import io.grpc.internal.WithLogId;
import io.grpc.stub.StreamObserver;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.EnumMap;
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

/**
 * A {@link LoadBalancer} that uses the GRPCLB protocol.
 *
 * <p>Optionally, when requested by the naming system, will delegate the work to a local pick-first
 * or round-robin balancer.
 */
class GrpclbLoadBalancer extends LoadBalancer implements WithLogId {
  private static final Logger logger = Logger.getLogger(GrpclbLoadBalancer.class.getName());

  @VisibleForTesting
  static final Map<DropType, PickResult> DROP_PICK_RESULTS;

  static {
    EnumMap<DropType, PickResult> map = new EnumMap<DropType, PickResult>(DropType.class);
    for (DropType dropType : DropType.values()) {
      map.put(
          dropType,
          PickResult.withError(
              Status.UNAVAILABLE.withDescription(
                  "Dropped as requested by balancer. Type: " + dropType)));
    }
    DROP_PICK_RESULTS = Collections.unmodifiableMap(map);
  }

  @VisibleForTesting
  static final SubchannelPicker BUFFER_PICKER = new SubchannelPicker() {
      @Override
      public PickResult pickSubchannel(PickSubchannelArgs args) {
        return PickResult.withNoResult();
      }
    };

  private final LogId logId = LogId.allocate(getClass().getName());

  private final String serviceName;
  private final Helper helper;
  private final Factory pickFirstBalancerFactory;
  private final Factory roundRobinBalancerFactory;
  private final ObjectPool<ScheduledExecutorService> timerServicePool;
  private final TimeProvider time;

  private static final Attributes.Key<AtomicReference<ConnectivityStateInfo>> STATE_INFO =
      Attributes.Key.of("io.grpc.grpclb.GrpclbLoadBalancer.stateInfo");

  // All mutable states in this class are mutated ONLY from Channel Executor

  ///////////////////////////////////////////////////////////////////////////////
  // General states.
  ///////////////////////////////////////////////////////////////////////////////
  private ScheduledExecutorService timerService;

  // If not null, all work is delegated to it.
  @Nullable
  private LoadBalancer delegate;
  private LbPolicy lbPolicy;

  ///////////////////////////////////////////////////////////////////////////////
  // GRPCLB states, valid only if lbPolicy == GRPCLB
  ///////////////////////////////////////////////////////////////////////////////

  // null if there isn't any available LB addresses.
  @Nullable
  private LbAddressGroup lbAddressGroup;
  @Nullable
  private ManagedChannel lbCommChannel;

  @Nullable
  private LbStream lbStream;
  private Map<EquivalentAddressGroup, Subchannel> subchannels = Collections.emptyMap();

  private List<RoundRobinEntry> roundRobinList = Collections.emptyList();
  private SubchannelPicker currentPicker = BUFFER_PICKER;

  GrpclbLoadBalancer(Helper helper, Factory pickFirstBalancerFactory,
      Factory roundRobinBalancerFactory, ObjectPool<ScheduledExecutorService> timerServicePool,
      TimeProvider time) {
    this.helper = checkNotNull(helper, "helper");
    this.serviceName = checkNotNull(helper.getAuthority(), "helper returns null authority");
    this.pickFirstBalancerFactory =
        checkNotNull(pickFirstBalancerFactory, "pickFirstBalancerFactory");
    this.roundRobinBalancerFactory =
        checkNotNull(roundRobinBalancerFactory, "roundRobinBalancerFactory");
    this.timerServicePool = checkNotNull(timerServicePool, "timerServicePool");
    this.timerService = checkNotNull(timerServicePool.getObject(), "timerService");
    this.time = checkNotNull(time, "time provider");
  }

  @Override
  public LogId getLogId() {
    return logId;
  }

  @Override
  public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo newState) {
    if (delegate != null) {
      delegate.handleSubchannelState(subchannel, newState);
      return;
    }
    if (newState.getState() == SHUTDOWN || !(subchannels.values().contains(subchannel))) {
      return;
    }
    if (newState.getState() == IDLE) {
      subchannel.requestConnection();
    }
    subchannel.getAttributes().get(STATE_INFO).set(newState);
    maybeUpdatePicker();
  }

  @Override
  public void handleResolvedAddressGroups(
      List<EquivalentAddressGroup> updatedServers, Attributes attributes) {
    LbPolicy newLbPolicy = attributes.get(GrpclbConstants.ATTR_LB_POLICY);
    // LB addresses and backend addresses are treated separately
    List<LbAddressGroup> newLbAddressGroups = new ArrayList<LbAddressGroup>();
    List<EquivalentAddressGroup> newBackendServers = new ArrayList<EquivalentAddressGroup>();
    for (EquivalentAddressGroup server : updatedServers) {
      String lbAddrAuthority = server.getAttributes().get(GrpclbConstants.ATTR_LB_ADDR_AUTHORITY);
      if (lbAddrAuthority != null) {
        newLbAddressGroups.add(new LbAddressGroup(server, lbAddrAuthority));
      } else {
        newBackendServers.add(server);
      }
    }

    if (newBackendServers.isEmpty()) {
      // handleResolvedAddressGroups()'s javadoc has guaranteed updatedServers is never empty.
      checkState(!newLbAddressGroups.isEmpty(),
          "No backend address nor LB address.  updatedServers=%s", updatedServers);
      if (newLbPolicy != LbPolicy.GRPCLB) {
        newLbPolicy = LbPolicy.GRPCLB;
        logger.log(Level.FINE, "[{0}] Switching to GRPCLB because all addresses are balancers",
            logId);
      }
    }
    if (newLbPolicy == null) {
      logger.log(Level.FINE, "[{0}] New config missing policy. Using PICK_FIRST", logId);
      newLbPolicy = LbPolicy.PICK_FIRST;
    }

    // Switch LB policy if requested
    if (newLbPolicy != lbPolicy) {
      shutdownDelegate();
      shutdownLbComm();
      lbAddressGroup = null;
      switch (newLbPolicy) {
        case PICK_FIRST:
          delegate = checkNotNull(pickFirstBalancerFactory.newLoadBalancer(helper),
              "pickFirstBalancerFactory.newLoadBalancer()");
          break;
        case ROUND_ROBIN:
          delegate = checkNotNull(roundRobinBalancerFactory.newLoadBalancer(helper),
              "roundRobinBalancerFactory.newLoadBalancer()");
          break;
        default:
          // Do nohting
      }
    }
    lbPolicy = newLbPolicy;

    // Consume the new addresses
    switch (lbPolicy) {
      case PICK_FIRST:
      case ROUND_ROBIN:
        checkNotNull(delegate, "delegate should not be null. newLbPolicy=" + newLbPolicy);
        delegate.handleResolvedAddressGroups(newBackendServers, attributes);
        break;
      case GRPCLB:
        if (newLbAddressGroups.isEmpty()) {
          shutdownLbComm();
          lbAddressGroup = null;
          handleGrpclbError(Status.UNAVAILABLE.withDescription(
                  "NameResolver returned no LB address while asking for GRPCLB"));
        } else {
          lbAddressGroup = flattenLbAddressGroups(newLbAddressGroups);
          startLbComm();
          // Avoid creating a new RPC just because the addresses were updated, as it can cause a
          // stampeding herd. The current RPC may be on a connection to an address not present in
          // newLbAddressGroups, but we're considering that "okay". If we detected the RPC is to an
          // outdated backend, we could choose to re-create the RPC.
          if (lbStream == null) {
            startLbRpc();
          }
        }
        break;
      default:
        // Do nothing
    }
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
    }
  }

  private void startLbComm() {
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

  private void shutdownDelegate() {
    if (delegate != null) {
      delegate.shutdown();
      delegate = null;
    }
  }

  @Override
  public void shutdown() {
    shutdownDelegate();
    shutdownLbComm();
    for (Subchannel subchannel : subchannels.values()) {
      subchannel.shutdown();
    }
    subchannels = Collections.emptyMap();
    timerService = timerServicePool.returnObject(timerService);
  }

  private void handleGrpclbError(Status status) {
    logger.log(Level.FINE, "[{0}] Had an error: {1}; roundRobinList={2}",
        new Object[] {logId, status, roundRobinList});
    if (roundRobinList.isEmpty()) {
      maybeUpdatePicker(new ErrorPicker(status));
    }
  }

  @Override
  public void handleNameResolutionError(Status error) {
    if (delegate != null) {
      delegate.handleNameResolutionError(error);
    } else {
      handleGrpclbError(error);
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

  private class LbStream implements StreamObserver<LoadBalanceResponse> {
    final StreamObserver<LoadBalanceRequest> lbRequestWriter;
    final GrpclbClientLoadRecorder loadRecorder;

    final Runnable loadReportRunnable = new Runnable() {
        @Override
        public void run() {
          helper.runSerialized(new Runnable() {
              @Override
              public void run() {
                loadReportTask = null;
                sendLoadReport();
              }
            });
        }
      };

    // These fields are only accessed from helper.runSerialized()
    boolean initialResponseReceived;
    boolean closed;
    long loadReportIntervalMillis = -1;
    ScheduledFuture<?> loadReportTask;

    LbStream(LoadBalancerGrpc.LoadBalancerStub stub) {
      // Stats data only valid for current LbStream.  We do not carry over data from previous
      // stream.
      loadRecorder = new GrpclbClientLoadRecorder(time);
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
        loadReportTask = timerService.schedule(
            loadReportRunnable, loadReportIntervalMillis, TimeUnit.MILLISECONDS);
      }
    }

    private void handleResponse(LoadBalanceResponse response) {
      if (closed) {
        return;
      }
      logger.log(Level.FINE, "[{0}] Got an LB response: {1}", new Object[] {logId, response});

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

      // TODO(zhangkun83): handle delegate from initialResponse
      ServerList serverList = response.getServerList();
      HashMap<EquivalentAddressGroup, Subchannel> newSubchannelMap =
          new HashMap<EquivalentAddressGroup, Subchannel>();
      List<RoundRobinEntry> newRoundRobinList = new ArrayList<RoundRobinEntry>();
      // TODO(zhangkun83): honor expiration_interval
      // Construct the new collections. Create new Subchannels when necessary.
      for (Server server : serverList.getServersList()) {
        if (server.getDropForRateLimiting()) {
          newRoundRobinList.add(new RoundRobinEntry(DropType.RATE_LIMITING, loadRecorder));
        } else if (server.getDropForLoadBalancing()) {
          newRoundRobinList.add(new RoundRobinEntry(DropType.LOAD_BALANCING, loadRecorder));
        } else {
          InetSocketAddress address;
          try {
            address = new InetSocketAddress(
                InetAddress.getByAddress(server.getIpAddress().toByteArray()), server.getPort());
          } catch (UnknownHostException e) {
            handleGrpclbError(Status.UNAVAILABLE.withCause(e));
            continue;
          }
          EquivalentAddressGroup eag = new EquivalentAddressGroup(address);
          String token = server.getLoadBalanceToken();
          Subchannel subchannel = newSubchannelMap.get(eag);
          if (subchannel == null) {
            subchannel = subchannels.get(eag);
            if (subchannel == null) {
              Attributes subchannelAttrs = Attributes.newBuilder()
                  .set(STATE_INFO,
                      new AtomicReference<ConnectivityStateInfo>(
                          ConnectivityStateInfo.forNonError(IDLE)))
                  .build();
              subchannel = helper.createSubchannel(eag, subchannelAttrs);
              subchannel.requestConnection();
            }
            newSubchannelMap.put(eag, subchannel);
          }
          newRoundRobinList.add(new RoundRobinEntry(subchannel, loadRecorder, token));
        }
      }
      // Close Subchannels whose addresses have been delisted
      for (Entry<EquivalentAddressGroup, Subchannel> entry : subchannels.entrySet()) {
        EquivalentAddressGroup eag = entry.getKey();
        if (!newSubchannelMap.containsKey(eag)) {
          entry.getValue().shutdown();
        }
      }

      subchannels = newSubchannelMap;
      roundRobinList = newRoundRobinList;
      maybeUpdatePicker();
    }

    private void handleStreamClosed(Status status) {
      if (closed) {
        return;
      }
      closed = true;
      cleanUp();
      handleGrpclbError(status);
      startLbRpc();
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
      if (loadReportTask != null) {
        loadReportTask.cancel(false);
        loadReportTask = null;
      }
      if (lbStream == this) {
        lbStream = null;
      }
    }
  }

  /**
   * Make and use a picker out of the current roundRobinList and the states of subchannels if they
   * have changed since the last picker created.
   */
  private void maybeUpdatePicker() {
    List<RoundRobinEntry> resultList = new ArrayList<RoundRobinEntry>();
    Status error = null;
    // TODO(zhangkun83): if roundRobinList contains at least one address, but none of them are
    // ready, maybe we should always return BUFFER_PICKER, no matter if there are drop entries or
    // not.
    for (RoundRobinEntry entry : roundRobinList) {
      Subchannel subchannel = entry.result.getSubchannel();
      if (subchannel != null) {
        Attributes attrs = subchannel.getAttributes();
        ConnectivityStateInfo stateInfo = attrs.get(STATE_INFO).get();
        if (stateInfo.getState() == READY) {
          resultList.add(entry);
        } else if (stateInfo.getState() == TRANSIENT_FAILURE) {
          error = stateInfo.getStatus();
        }
      } else {
        // This is a drop entry.
        resultList.add(entry);
      }
    }
    if (resultList.isEmpty()) {
      if (error != null) {
        logger.log(Level.FINE, "[{0}] No ready Subchannel. Using error: {1}",
            new Object[] {logId, error});
        maybeUpdatePicker(new ErrorPicker(error));
      } else {
        logger.log(Level.FINE, "[{0}] No ready Subchannel and no error", logId);
        maybeUpdatePicker(BUFFER_PICKER);
      }
    } else {
      logger.log(Level.FINE, "[{0}] Using list {1}", new Object[] {logId, resultList});
      maybeUpdatePicker(new RoundRobinPicker(resultList));
    }
  }

  /**
   * Update the given picker to the helper if it's different from the current one.
   */
  private void maybeUpdatePicker(SubchannelPicker picker) {
    // Discard the new picker if we are sure it won't make any difference, in order to save
    // re-processing pending streams, and avoid unnecessary resetting of the pointer in
    // RoundRobinPicker.
    if (picker == BUFFER_PICKER && currentPicker == BUFFER_PICKER) {
      return;
    }
    if (picker instanceof RoundRobinPicker && currentPicker instanceof RoundRobinPicker) {
      if (((RoundRobinPicker) picker).list.equals(((RoundRobinPicker) currentPicker).list)) {
        return;
      }
    }
    // No need to skip ErrorPicker. If the current picker is ErrorPicker, there won't be any pending
    // stream thus no time is wasted in re-process.
    currentPicker = picker;
    helper.updatePicker(picker);
  }

  @VisibleForTesting
  LoadBalancer getDelegate() {
    return delegate;
  }

  @VisibleForTesting
  LbPolicy getLbPolicy() {
    return lbPolicy;
  }

  private LbAddressGroup flattenLbAddressGroups(List<LbAddressGroup> groupList) {
    assert !groupList.isEmpty();
    List<EquivalentAddressGroup> eags = new ArrayList<EquivalentAddressGroup>(groupList.size());
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
    return new LbAddressGroup(flattenEquivalentAddressGroup(eags), authority);
  }

  /**
   * Flattens list of EquivalentAddressGroup objects into one EquivalentAddressGroup object.
   */
  private static EquivalentAddressGroup flattenEquivalentAddressGroup(
      List<EquivalentAddressGroup> groupList) {
    List<SocketAddress> addrs = new ArrayList<SocketAddress>();
    for (EquivalentAddressGroup group : groupList) {
      for (SocketAddress addr : group.getAddresses()) {
        addrs.add(addr);
      }
    }
    return new EquivalentAddressGroup(addrs);
  }

  @VisibleForTesting
  static final class ErrorPicker extends SubchannelPicker {
    final PickResult result;

    ErrorPicker(Status status) {
      result = PickResult.withError(status);
    }

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      return result;
    }
  }

  @VisibleForTesting
  static final class RoundRobinEntry {
    final PickResult result;
    final GrpclbClientLoadRecorder loadRecorder;
    @Nullable
    private final String token;
    @Nullable
    private final DropType dropType;

    /**
     * A non-drop result.
     */
    RoundRobinEntry(Subchannel subchannel, GrpclbClientLoadRecorder loadRecorder, String token) {
      this.loadRecorder = checkNotNull(loadRecorder, "loadRecorder");
      this.result = PickResult.withSubchannel(subchannel, loadRecorder);
      this.token = token;
      this.dropType = null;
    }

    /**
     * A drop result.
     */
    RoundRobinEntry(DropType dropType, GrpclbClientLoadRecorder loadRecorder) {
      this.loadRecorder = checkNotNull(loadRecorder, "loadRecorder");
      // We re-use the status for each DropType to make it easy to test, because Status class
      // intentionally doesn't implement equals().
      this.result = DROP_PICK_RESULTS.get(dropType);
      this.token = null;
      this.dropType = dropType;
    }

    void updateHeaders(Metadata headers) {
      if (token != null) {
        headers.discardAll(GrpclbConstants.TOKEN_METADATA_KEY);
        headers.put(GrpclbConstants.TOKEN_METADATA_KEY, token);
      }
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("result", result)
          .add("token", token)
          .add("dropType", dropType)
          .toString();
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(result, token, dropType);
    }

    @Override
    public boolean equals(Object other) {
      if (!(other instanceof RoundRobinEntry)) {
        return false;
      }
      RoundRobinEntry that = (RoundRobinEntry) other;
      return Objects.equal(result, that.result) && Objects.equal(token, that.token)
          && Objects.equal(dropType, that.dropType);
    }
  }

  @VisibleForTesting
  static final class RoundRobinPicker extends SubchannelPicker {
    final List<RoundRobinEntry> list;
    private int index;

    RoundRobinPicker(List<RoundRobinEntry> resultList) {
      checkArgument(!resultList.isEmpty(), "resultList is empty");
      this.list = checkNotNull(resultList, "resultList");
    }

    @Override
    public PickResult pickSubchannel(PickSubchannelArgs args) {
      synchronized (list) {
        RoundRobinEntry result = list.get(index);
        index++;
        if (index == list.size()) {
          index = 0;
        }
        result.updateHeaders(args.getHeaders());
        if (result.dropType != null) {
          result.loadRecorder.recordDroppedRequest(result.dropType);
        }
        return result.result;
      }
    }
  }
}
