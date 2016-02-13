/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;
import static org.mockito.Mockito.withSettings;

import com.google.common.base.Supplier;

import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.ResolvedServerInfo;
import io.grpc.Status;
import io.grpc.TransportManager.InterimTransport;
import io.grpc.TransportManager;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.net.InetSocketAddress;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

/** Unit tests for {@link GrpclbLoadBalancer}. */
@RunWith(JUnit4.class)
public class GrpclbLoadBalancerTest {

  private static final String serviceName = "testlbservice";

  @Mock private TransportManager<Transport> mockTransportManager;
  @Mock private InterimTransport<Transport> interimTransport;
  @Mock private Transport interimTransportAsTransport;
  @Mock private Transport failingTransport;
  @Captor private ArgumentCaptor<Supplier<Transport>> transportSupplierCaptor;

  // The test subject
  private TestGrpclbLoadBalancer loadBalancer;

  // Current addresses of the LB server
  private EquivalentAddressGroup lbAddressGroup;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(mockTransportManager.createInterimTransport()).thenReturn(interimTransport);
    when(mockTransportManager.createFailingTransport(any(Status.class)))
        .thenReturn(failingTransport);
    when(interimTransport.transport()).thenReturn(interimTransportAsTransport);
    loadBalancer = new TestGrpclbLoadBalancer();
  }

  @Test
  public void balancing() throws Exception {
    List<ResolvedServerInfo> servers = createResolvedServerInfoList(4000, 4001);

    // Set up mocks
    List<Transport> transports = new ArrayList<Transport>(servers.size());
    for (ResolvedServerInfo server : servers) {
      Transport transport = mock(Transport.class, withSettings().name("Transport for "  + server));
      transports.add(transport);
      when(mockTransportManager.getTransport(eq(new EquivalentAddressGroup(server.getAddress()))))
          .thenReturn(transport);
    }

    Transport pick0;
    Transport pick1;
    Transport pick2;

    // Pick before name resolved
    pick0 = loadBalancer.pickTransport(null);

    // Name resolved
    Transport lbTransport = simulateLbAddressResolved(30001);

    // Pick after name resolved
    pick1 = loadBalancer.pickTransport(null);
    pick2 = loadBalancer.pickTransport(null);

    // Both picks end up with interimTransport
    verify(mockTransportManager).createInterimTransport();
    assertSame(interimTransportAsTransport, pick0);
    assertSame(interimTransportAsTransport, pick1);
    assertSame(interimTransportAsTransport, pick2);

    // An LB request is sent
    SendLbRequestArgs sentLbRequest = loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS);
    assertNotNull(sentLbRequest);
    assertSame(lbTransport, sentLbRequest.transport);

    // Simulate an initial LB response
    loadBalancer.getLbResponseObserver().onNext(
        LoadBalanceResponse.newBuilder().setInitialResponse(
            InitialLoadBalanceResponse.getDefaultInstance())
        .build());

    // Simulate that the LB server reponses, with servers 0, 1, 1
    List<ResolvedServerInfo> serverList1 = new ArrayList<ResolvedServerInfo>();
    Collections.addAll(serverList1, servers.get(0), servers.get(1), servers.get(1));
    assertNotNull(loadBalancer.getLbResponseObserver());
    loadBalancer.getLbResponseObserver().onNext(buildLbResponse(serverList1));

    verify(mockTransportManager).updateRetainedTransports(eq(buildRetainedAddressSet(servers)));

    verify(interimTransport).closeWithRealTransports(transportSupplierCaptor.capture());
    assertSame(transports.get(0), transportSupplierCaptor.getValue().get());
    assertSame(transports.get(1), transportSupplierCaptor.getValue().get());
    assertSame(transports.get(1), transportSupplierCaptor.getValue().get());
    verify(mockTransportManager).getTransport(eq(buildAddressGroup(servers.get(0))));
    verify(mockTransportManager, times(2)).getTransport(eq(buildAddressGroup(servers.get(1))));

    // Pick beyond the end of the list. Go back to the beginning.
    pick0 = loadBalancer.pickTransport(null);
    assertSame(transports.get(0), pick0);

    // Only one LB request has ever been sent at this point
    assertEquals(0, loadBalancer.sentLbRequests.size());
  }

  @Test public void serverListUpdated() throws Exception {
    // Simulate the initial set of LB addresses resolved
    Transport lbTransport = simulateLbAddressResolved(30001);

    // An LB request is sent
    SendLbRequestArgs sentLbRequest = loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS);
    assertNotNull(sentLbRequest);
    assertSame(lbTransport, sentLbRequest.transport);

    // Simulate LB server responds a server list
    List<ResolvedServerInfo> serverList = createResolvedServerInfoList(4000, 4001);
    loadBalancer.getLbResponseObserver().onNext(buildLbResponse(serverList));
    verify(mockTransportManager).updateRetainedTransports(eq(buildRetainedAddressSet(serverList)));

    // The server list is in effect
    assertEquals(buildRoundRobinList(serverList), loadBalancer.getRoundRobinServerList().getList());

    // Simulate LB server responds another server list
    serverList = createResolvedServerInfoList(4002, 4003);
    loadBalancer.getLbResponseObserver().onNext(buildLbResponse(serverList));
    verify(mockTransportManager).updateRetainedTransports(eq(buildRetainedAddressSet(serverList)));

    // The new list is in effect
    assertEquals(buildRoundRobinList(serverList), loadBalancer.getRoundRobinServerList().getList());
  }

  @Test public void newLbAddressesResolved() throws Exception {
    // Simulate the initial set of LB addresses resolved
    Transport lbTransport = simulateLbAddressResolved(30001);

    EquivalentAddressGroup lbAddress1 = lbAddressGroup;
    verify(mockTransportManager).updateRetainedTransports(eq(Collections.singleton(lbAddress1)));
    verify(mockTransportManager).getTransport(eq(lbAddressGroup));

    // An LB request is sent
    SendLbRequestArgs sentLbRequest = loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS);
    assertNotNull(sentLbRequest);
    assertSame(lbTransport, sentLbRequest.transport);

    // Simulate a second set of LB addresses resolved
    lbTransport = simulateLbAddressResolved(30002);
    EquivalentAddressGroup lbAddress2 = lbAddressGroup;
    assertNotEquals(lbAddress1, lbAddress2);
    verify(mockTransportManager).updateRetainedTransports(eq(Collections.singleton(lbAddress2)));
    verify(mockTransportManager).getTransport(eq(lbAddressGroup));

    // Another LB request is sent
    sentLbRequest = loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS);
    assertNotNull(sentLbRequest);
    assertSame(lbTransport, sentLbRequest.transport);

    // Simulate that an identical set of LB addresses is resolved
    simulateLbAddressResolved(30002);
    EquivalentAddressGroup lbAddress3 = lbAddressGroup;
    verify(mockTransportManager).getTransport(eq(lbAddressGroup));

    // Only when LB address changes, getTransport is called.
    verify(mockTransportManager, times(2)).getTransport(any(EquivalentAddressGroup.class));
  }

  @Test public void lbStreamErrorAfterResponse() throws Exception {
    // Simulate the initial set of LB addresses resolved
    simulateLbAddressResolved(30001);

    // An LB request is sent
    assertNotNull(loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS));

    // Simulate LB server responds a server list
    List<ResolvedServerInfo> serverList = createResolvedServerInfoList(4000, 4001);
    loadBalancer.getLbResponseObserver().onNext(buildLbResponse(serverList));
    assertEquals(buildRoundRobinList(serverList), loadBalancer.getRoundRobinServerList().getList());

    // Simulate a stream error
    loadBalancer.getLbResponseObserver().onError(
        Status.UNAVAILABLE.withDescription("simulated").asException());

    // Another LB request is sent
    assertNotNull(loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS));

    // Simulate LB server responds a new list
    serverList = createResolvedServerInfoList(4002, 4003);
    loadBalancer.getLbResponseObserver().onNext(buildLbResponse(serverList));
    assertEquals(buildRoundRobinList(serverList), loadBalancer.getRoundRobinServerList().getList());
  }

  @Test public void lbStreamErrorWithoutResponse() throws Exception {
    // Simulate the initial set of LB addresses resolved
    Transport lbTransport = simulateLbAddressResolved(30001);

    // First pick, will be pending
    Transport pick = loadBalancer.pickTransport(null);
    assertSame(interimTransportAsTransport, pick);

    // An LB request is sent
    SendLbRequestArgs sentLbRequest = loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS);
    assertNotNull(sentLbRequest);
    assertSame(lbTransport, sentLbRequest.transport);

    // Simulate that the LB stream fails
    loadBalancer.getLbResponseObserver().onError(
        Status.UNAVAILABLE.withDescription("simulated").asException());

    // The pending pick will fail
    verifyInterimTransportClosedWithError(Status.Code.UNAVAILABLE, "simulated",
        "Stream to GRPCLB LoadBalancer had an error");

    // Another LB request is sent
    sentLbRequest = loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS);
    assertNotNull(sentLbRequest);

    // Round-robin list not available at this point
    assertNull(loadBalancer.getRoundRobinServerList());
  }

  @Test public void lbStreamUnimplemented() throws Exception {
    // Simulate the initial set of LB addresses resolved
    Transport lbTransport = simulateLbAddressResolved(30001);

    // First pick, will be pending
    Transport pick = loadBalancer.pickTransport(null);
    assertSame(interimTransportAsTransport, pick);

    // An LB request is sent
    SendLbRequestArgs sentLbRequest = loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS);
    assertNotNull(sentLbRequest);
    assertSame(lbTransport, sentLbRequest.transport);

    // Simulate that the LB stream fails with UNIMPLEMENTED
    loadBalancer.getLbResponseObserver().onError(Status.UNIMPLEMENTED.asException());

    // The pending pick will succeed with lbTransport
    verify(interimTransport).closeWithRealTransports(transportSupplierCaptor.capture());
    assertSame(lbTransport, transportSupplierCaptor.getValue().get());

    // Subsequent picks will also get lbTransport
    pick = loadBalancer.pickTransport(null);
    assertSame(lbTransport, pick);

    // Round-robin list NOT available at this point
    assertNull(loadBalancer.getRoundRobinServerList());

    verify(mockTransportManager, times(1)).getTransport(eq(lbAddressGroup));

    // Didn't send additional requests other than the initial one
    assertEquals(0, loadBalancer.sentLbRequests.size());

    // Shut down the transport
    loadBalancer.handleTransportShutdown(lbAddressGroup,
        Status.UNAVAILABLE.withDescription("simulated"));

    // Subsequent pick will result in a failing transport because an error has occurred
    pick = loadBalancer.pickTransport(null);
    assertSame(failingTransport, pick);
    verifyCreateFailingTransport(Status.Code.UNAVAILABLE,
        "simulated", "Transport to LB server closed");

    // Will get another lbTransport, and send another LB request
    verify(mockTransportManager, times(2)).getTransport(eq(lbAddressGroup));
    assertNotNull(loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS));
  }

  @Test public void lbConnectionClosedAfterResponse() throws Exception {
    // Simulate the initial set of LB addresses resolved
    simulateLbAddressResolved(30001);

    // An LB request is sent
    assertNotNull(loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS));

    // Simulate LB server responds a server list
    List<ResolvedServerInfo> serverList = createResolvedServerInfoList(4000, 4001);
    loadBalancer.getLbResponseObserver().onNext(buildLbResponse(serverList));
    assertEquals(buildRoundRobinList(serverList), loadBalancer.getRoundRobinServerList().getList());

    // Simulate transport closes
    loadBalancer.handleTransportShutdown(lbAddressGroup, Status.UNAVAILABLE);

    // Will get another transport
    verify(mockTransportManager, times(2)).getTransport(eq(lbAddressGroup));

    // Another LB request is sent
    assertNotNull(loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS));
  }

  @Test public void lbConnectionClosedWithoutResponse() throws Exception {
    // Simulate the initial set of LB addresses resolved
    Transport lbTransport = simulateLbAddressResolved(30001);

    // First pick, will be pending
    Transport pick = loadBalancer.pickTransport(null);
    assertSame(interimTransportAsTransport, pick);

    // An LB request is sent
    SendLbRequestArgs sentLbRequest = loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS);
    assertNotNull(sentLbRequest);
    assertSame(lbTransport, sentLbRequest.transport);

    // Simulate that the transport closed
    loadBalancer.handleTransportShutdown(
        lbAddressGroup, Status.UNAVAILABLE.withDescription("simulated"));

    // The interim transport will close with error
    verifyInterimTransportClosedWithError(Status.Code.UNAVAILABLE,
        "simulated", "Transport to LB server closed");

    // Will try to get another transport
    verify(mockTransportManager, times(2)).getTransport(eq(lbAddressGroup));

    // Round-robin list not available at this point
    assertNull(loadBalancer.getRoundRobinServerList());
  }

  @Test public void nameResolutionFailed() throws Exception {
    Transport pick0 = loadBalancer.pickTransport(null);
    assertSame(interimTransportAsTransport, pick0);

    loadBalancer.handleNameResolutionError(Status.UNAVAILABLE);
    verifyInterimTransportClosedWithError(Status.Code.UNAVAILABLE, "Name resolution failed");
    Transport pick1 = loadBalancer.pickTransport(null);
    assertSame(failingTransport, pick1);
    verifyCreateFailingTransport(Status.Code.UNAVAILABLE, "Name resolution failed");
  }

  @Test public void shutdown() throws Exception {
    // Simulate the initial set of LB addresses resolved
    simulateLbAddressResolved(30001);

    // An LB request is sent
    assertNotNull(loadBalancer.sentLbRequests.poll(1000, TimeUnit.SECONDS));

    // Simulate LB server responds a server list
    List<ResolvedServerInfo> serverList = createResolvedServerInfoList(4000, 4001);
    loadBalancer.getLbResponseObserver().onNext(buildLbResponse(serverList));
    verify(mockTransportManager).getTransport(eq(lbAddressGroup));
    assertEquals(buildRoundRobinList(serverList), loadBalancer.getRoundRobinServerList().getList());

    // Shut down the LoadBalancer
    loadBalancer.shutdown();

    // Simulate a stream error
    loadBalancer.getLbResponseObserver().onError(Status.CANCELLED.asException());

    // Won't send a request
    assertEquals(0, loadBalancer.sentLbRequests.size());

    // Simulate transport closure
    loadBalancer.handleTransportShutdown(lbAddressGroup, Status.CANCELLED);

    // Won't get a new transport. getTransport() was call once before.
    verify(mockTransportManager).getTransport(any(EquivalentAddressGroup.class));
  }

  /**
   * Simulates a single LB address is resolved and sets up lbAddressGroup. Returns the transport
   * to LB.
   */
  private Transport simulateLbAddressResolved(int lbPort) {
    ResolvedServerInfo lbServerInfo = new ResolvedServerInfo(
        new InetSocketAddress("127.0.0.1", lbPort), Attributes.EMPTY);
    lbAddressGroup = buildAddressGroup(lbServerInfo);
    Transport lbTransport = new Transport();
    when(mockTransportManager.getTransport(eq(lbAddressGroup))).thenReturn(lbTransport);
    loadBalancer.handleResolvedAddresses(Collections.singletonList(lbServerInfo), Attributes.EMPTY);
    verify(mockTransportManager).getTransport(eq(lbAddressGroup));
    return lbTransport;
  }

  private HashSet<EquivalentAddressGroup> buildRetainedAddressSet(
      Collection<ResolvedServerInfo> serverInfos) {
    HashSet<EquivalentAddressGroup> addrs = new HashSet<EquivalentAddressGroup>();
    for (ResolvedServerInfo serverInfo : serverInfos) {
      addrs.add(new EquivalentAddressGroup(serverInfo.getAddress()));
    }
    addrs.add(lbAddressGroup);
    return addrs;
  }

  /**
   * A slightly modified {@link GrpclbLoadBalancerTest} that saves LB requests in a queue instead of
   * sending them out.
   */
  private class TestGrpclbLoadBalancer extends GrpclbLoadBalancer<Transport> {
    final LinkedBlockingQueue<SendLbRequestArgs> sentLbRequests =
        new LinkedBlockingQueue<SendLbRequestArgs>();

    TestGrpclbLoadBalancer() {
      super(serviceName, mockTransportManager);
    }

    @Override void sendLbRequest(Transport transport, LoadBalanceRequest request) {
      sentLbRequests.add(new SendLbRequestArgs(transport, request));
    }
  }

  private static class SendLbRequestArgs {
    final Transport transport;
    final LoadBalanceRequest request;

    SendLbRequestArgs(Transport transport, LoadBalanceRequest request) {
      this.transport = transport;
      this.request = request;
    }
  }

  private static LoadBalanceResponse buildLbResponse(List<ResolvedServerInfo> servers) {
    ServerList.Builder serverListBuilder = ServerList.newBuilder();
    for (ResolvedServerInfo server : servers) {
      InetSocketAddress addr = (InetSocketAddress) server.getAddress();
      serverListBuilder.addServers(Server.newBuilder()
          .setIpAddress(addr.getHostString())
          .setPort(addr.getPort())
          .build());
    }
    return LoadBalanceResponse.newBuilder()
        .setServerList(serverListBuilder.build())
        .build();
  }

  private static EquivalentAddressGroup buildAddressGroup(ResolvedServerInfo serverInfo) {
    return new EquivalentAddressGroup(serverInfo.getAddress());
  }

  private static List<EquivalentAddressGroup> buildRoundRobinList(
      List<ResolvedServerInfo> serverList) {
    ArrayList<EquivalentAddressGroup> roundRobinList = new ArrayList<EquivalentAddressGroup>();
    for (ResolvedServerInfo serverInfo : serverList) {
      roundRobinList.add(new EquivalentAddressGroup(serverInfo.getAddress()));
    }
    return roundRobinList;
  }

  private static List<ResolvedServerInfo> createResolvedServerInfoList(int ... ports) {
    List<ResolvedServerInfo> result = new ArrayList<ResolvedServerInfo>(ports.length);
    for (int port : ports) {
      InetSocketAddress inetSocketAddress = new InetSocketAddress("127.0.0.1", port);
      result.add(new ResolvedServerInfo(inetSocketAddress, Attributes.EMPTY));
    }
    return result;
  }

  private void verifyInterimTransportClosedWithError(
      Status.Code statusCode, String ... descriptions) {
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(interimTransport).closeWithError(statusCaptor.capture());
    assertError(statusCaptor.getValue(), statusCode, descriptions);
  }

  private void verifyCreateFailingTransport(
      Status.Code statusCode, String ... descriptions) {
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(mockTransportManager).createFailingTransport(statusCaptor.capture());
    assertError(statusCaptor.getValue(), statusCode, descriptions);
  }


  private static void assertError(Status s, Status.Code statusCode, String ... descriptions) {
    assertEquals(statusCode, s.getCode());
    for (String desc : descriptions) {
      assertTrue("'" + s.getDescription() + "' contains '" + desc + "'",
          s.getDescription().contains(desc));
    }
  }

  public static class Transport {}
}
