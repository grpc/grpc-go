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
import io.grpc.Attributes;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.grpclb.GrpclbConstants.LbPolicy;
import io.grpc.internal.LogId;
import io.grpc.internal.WithLogId;
import io.grpc.stub.StreamObserver;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Map.Entry;
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

  private static final Attributes.Key<AtomicReference<ConnectivityStateInfo>> STATE_INFO =
      Attributes.Key.of("io.grpc.grpclb.GrpclbLoadBalancer.stateInfo");

  @VisibleForTesting
  static final Metadata.Key<String> TOKEN_KEY =
      Metadata.Key.of("lb-token", Metadata.ASCII_STRING_MARSHALLER);

  @VisibleForTesting
  static final RoundRobinEntry DROP_ENTRY =
      new RoundRobinEntry(Status.UNAVAILABLE.withDescription("Drop requested by balancer"));

  // All mutable states in this class are mutated ONLY from Channel Executor

  ///////////////////////////////////////////////////////////////////////////////
  // General states.
  ///////////////////////////////////////////////////////////////////////////////

  // If not null, all work is delegated to it.
  @Nullable
  private LoadBalancer delegate;
  private LbPolicy lbPolicy;

  ///////////////////////////////////////////////////////////////////////////////
  // GRPCLB states, valid only if lbPolicy == GRPCLB
  ///////////////////////////////////////////////////////////////////////////////

  // null if there isn't any available LB addresses.
  // If non-null, never empty.
  @Nullable
  private List<LbAddressGroup> lbAddressGroups;
  @Nullable
  private ManagedChannel lbCommChannel;
  // Points to the position of the LB address that lbCommChannel is bound to, if
  // lbCommChannel != null.
  private int currentLbIndex;
  @Nullable
  private LbResponseObserver lbResponseObserver;
  @Nullable
  private StreamObserver<LoadBalanceRequest> lbRequestWriter;
  private Map<EquivalentAddressGroup, Subchannel> subchannels = Collections.emptyMap();

  private List<RoundRobinEntry> roundRobinList = Collections.emptyList();

  GrpclbLoadBalancer(Helper helper, Factory pickFirstBalancerFactory,
      Factory roundRobinBalancerFactory) {
    this.helper = checkNotNull(helper, "helper");
    this.serviceName = checkNotNull(helper.getAuthority(), "helper returns null authority");
    this.pickFirstBalancerFactory =
        checkNotNull(pickFirstBalancerFactory, "pickFirstBalancerFactory");
    this.roundRobinBalancerFactory =
        checkNotNull(roundRobinBalancerFactory, "roundRobinBalancerFactory");
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
    helper.updatePicker(makePicker());
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
      lbAddressGroups = null;
      currentLbIndex = 0;
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
          lbAddressGroups = null;
          handleGrpclbError(Status.UNAVAILABLE.withDescription(
                  "NameResolver returned no LB address while asking for GRPCLB"));
        } else {
          // See if the currently used LB server is in the new list.
          int newIndexOfCurrentLb = -1;
          if (lbAddressGroups != null) {
            LbAddressGroup currentLb = lbAddressGroups.get(currentLbIndex);
            newIndexOfCurrentLb = newLbAddressGroups.indexOf(currentLb);
          }
          lbAddressGroups = newLbAddressGroups;
          if (newIndexOfCurrentLb == -1) {
            shutdownLbComm();
            currentLbIndex = 0;
            startLbComm();
          } else {
            // Current LB is still in the list, calibrate index.
            currentLbIndex = newIndexOfCurrentLb;
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
    if (lbRequestWriter != null) {
      lbRequestWriter.onCompleted();
      lbRequestWriter = null;
    }
    if (lbResponseObserver != null) {
      lbResponseObserver.dismissed = true;
      lbResponseObserver = null;
    }
  }

  private void startLbComm() {
    checkState(lbCommChannel == null, "previous lbCommChannel has not been closed yet");
    checkState(lbRequestWriter == null, "previous lbRequestWriter has not been cleared yet");
    checkState(lbResponseObserver == null, "previous lbResponseObserver has not been cleared yet");
    LbAddressGroup currentLb = lbAddressGroups.get(currentLbIndex);
    lbCommChannel = helper.createOobChannel(currentLb.getAddresses(), currentLb.getAuthority());
    LoadBalancerGrpc.LoadBalancerStub stub = LoadBalancerGrpc.newStub(lbCommChannel);
    lbResponseObserver = new LbResponseObserver();
    lbRequestWriter = stub.balanceLoad(lbResponseObserver);

    LoadBalanceRequest initRequest = LoadBalanceRequest.newBuilder()
        .setInitialRequest(InitialLoadBalanceRequest.newBuilder()
            .setName(serviceName).build())
        .build();
    lbRequestWriter.onNext(initRequest);
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
  }

  private void handleGrpclbError(Status status) {
    logger.log(Level.FINE, "[{0}] Had an error: {1}; roundRobinList={2}",
        new Object[] {logId, status, roundRobinList});
    if (roundRobinList.isEmpty()) {
      helper.updatePicker(new ErrorPicker(status));
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

  private class LbResponseObserver implements StreamObserver<LoadBalanceResponse> {
    boolean dismissed;

    @Override public void onNext(final LoadBalanceResponse response) {
      helper.runSerialized(new Runnable() {
          @Override
          public void run() {
            handleResponse(response);
          }
        });
    }

    private void handleResponse(LoadBalanceResponse response) {
      if (dismissed) {
        return;
      }
      logger.log(Level.FINE, "[{0}] Got an LB response: {1}", new Object[] {logId, response});
      // TODO(zhangkun83): make use of initialResponse
      // InitialLoadBalanceResponse initialResponse = response.getInitialResponse();
      ServerList serverList = response.getServerList();
      HashMap<EquivalentAddressGroup, Subchannel> newSubchannelMap =
          new HashMap<EquivalentAddressGroup, Subchannel>();
      List<RoundRobinEntry> newRoundRobinList = new ArrayList<RoundRobinEntry>();
      // TODO(zhangkun83): honor expiration_interval
      // Construct the new collections. Create new Subchannels when necessary.
      for (Server server : serverList.getServersList()) {
        if (server.getDropRequest()) {
          newRoundRobinList.add(DROP_ENTRY);
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
          newRoundRobinList.add(new RoundRobinEntry(subchannel, token));
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
      helper.updatePicker(makePicker());
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
            handleStreamClosed(Status.UNAVAILABLE.augmentDescription(
                    "Stream to GRPCLB LoadBalancer was closed"));
          }
        });
    }

    private void handleStreamClosed(Status status) {
      if (dismissed) {
        return;
      }
      lbRequestWriter = null;
      handleGrpclbError(status);
      shutdownLbComm();
      currentLbIndex = (currentLbIndex + 1) % lbAddressGroups.size();
      startLbComm();
    }
  }

  /**
   * Make a picker out of the current roundRobinList and the states of subchannels.
   */
  private SubchannelPicker makePicker() {
    List<RoundRobinEntry> resultList = new ArrayList<RoundRobinEntry>();
    Status error = null;
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
        return new ErrorPicker(error);
      } else {
        logger.log(Level.FINE, "[{0}] No ready Subchannel and no error", logId);
        return BUFFER_PICKER;
      }
    } else {
      logger.log(Level.FINE, "[{0}] Using list {1}", new Object[] {logId, resultList});
      return new RoundRobinPicker(resultList);
    }
  }

  @VisibleForTesting
  LoadBalancer getDelegate() {
    return delegate;
  }

  @VisibleForTesting
  LbPolicy getLbPolicy() {
    return lbPolicy;
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
    @Nullable
    private final String token;

    /**
     * A non-drop result.
     */
    RoundRobinEntry(Subchannel subchannel, String token) {
      this.result = PickResult.withSubchannel(subchannel);
      this.token = token;
    }

    /**
     * A drop result.
     */
    RoundRobinEntry(Status dropStatus) {
      this.result = PickResult.withError(dropStatus);
      this.token = null;
    }

    void updateHeaders(Metadata headers) {
      if (token != null) {
        headers.discardAll(TOKEN_KEY);
        headers.put(TOKEN_KEY, token);
      }
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("result", result)
          .add("token", token)
          .toString();
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(result, token);
    }

    @Override
    public boolean equals(Object other) {
      if (!(other instanceof RoundRobinEntry)) {
        return false;
      }
      RoundRobinEntry that = (RoundRobinEntry) other;
      return Objects.equal(result, that.result) && Objects.equal(token, that.token);
    }
  }

  @VisibleForTesting
  static final class RoundRobinPicker extends SubchannelPicker {
    final List<RoundRobinEntry> list;
    private int index;

    RoundRobinPicker(List<RoundRobinEntry> resultList) {
      checkArgument(!resultList.isEmpty(), "resultList is empty");
      list = resultList;
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
        return result.result;
      }
    }
  }
}
