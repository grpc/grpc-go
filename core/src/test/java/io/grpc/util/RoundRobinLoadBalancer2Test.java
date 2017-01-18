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

package io.grpc.util;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.util.RoundRobinLoadBalancerFactory2.RoundRobinLoadBalancer.STATE_INFO;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.Lists;
import com.google.common.collect.Maps;

import io.grpc.Attributes;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer2;
import io.grpc.LoadBalancer2.Helper;
import io.grpc.LoadBalancer2.Subchannel;
import io.grpc.Metadata;
import io.grpc.ResolvedServerInfo;
import io.grpc.ResolvedServerInfoGroup;
import io.grpc.Status;
import io.grpc.util.RoundRobinLoadBalancerFactory2.Picker;
import io.grpc.util.RoundRobinLoadBalancerFactory2.RoundRobinLoadBalancer;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.InOrder;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.net.SocketAddress;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;

/** Unit test for {@link RoundRobinLoadBalancerFactory2}. */
@RunWith(JUnit4.class)
public class RoundRobinLoadBalancer2Test {
  private RoundRobinLoadBalancer loadBalancer;
  private Map<ResolvedServerInfoGroup, EquivalentAddressGroup> servers = Maps.newHashMap();
  private Map<EquivalentAddressGroup, Subchannel> subchannels = Maps.newLinkedHashMap();
  private static final Attributes.Key<String> MAJOR_KEY = Attributes.Key.of("major-key");
  private Attributes affinity = Attributes.newBuilder().set(MAJOR_KEY, "I got the keys").build();

  @Captor
  private ArgumentCaptor<Picker> pickerCaptor;
  @Captor
  private ArgumentCaptor<EquivalentAddressGroup> eagCaptor;
  @Mock
  private Helper mockHelper;
  @Mock
  private Subchannel mockSubchannel;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    for (int i = 0; i < 3; i++) {
      SocketAddress addr = new FakeSocketAddress("server" + i);
      EquivalentAddressGroup eag = new EquivalentAddressGroup(addr);
      servers.put(ResolvedServerInfoGroup.builder().add(new ResolvedServerInfo(addr)).build(), eag);
      subchannels.put(eag, createMockSubchannel());
    }

    when(mockHelper.createSubchannel(any(EquivalentAddressGroup.class), any(Attributes.class)))
        .then(new Answer<Subchannel>() {
          @Override
          public Subchannel answer(InvocationOnMock invocation) throws Throwable {
            Object[] args = invocation.getArguments();
            return subchannels.get(args[0]);
          }
        });

    loadBalancer = (RoundRobinLoadBalancer) RoundRobinLoadBalancerFactory2.getInstance()
        .newLoadBalancer(mockHelper);
  }

  @Test
  public void pickAfterResolved() throws Exception {
    Subchannel readySubchannel = subchannels.get(servers.get(servers.keySet().iterator().next()));
    when(readySubchannel.getAttributes()).thenReturn(Attributes.newBuilder()
        .set(STATE_INFO, new AtomicReference<ConnectivityStateInfo>(
            ConnectivityStateInfo.forNonError(READY)))
        .build());
    loadBalancer.handleResolvedAddresses(Lists.newArrayList(servers.keySet()), affinity);

    verify(mockHelper, times(3)).createSubchannel(eagCaptor.capture(),
        any(Attributes.class));

    assertThat(eagCaptor.getAllValues()).containsAllIn(subchannels.keySet());
    for (Subchannel subchannel : subchannels.values()) {
      verify(subchannel).requestConnection();
      verify(subchannel, never()).shutdown();
    }

    verify(mockHelper, times(1)).updatePicker(pickerCaptor.capture());

    assertThat(pickerCaptor.getValue().getList()).containsExactly(readySubchannel);

    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void pickAfterResolvedUpdatedHosts() throws Exception {
    Subchannel removedSubchannel = mock(Subchannel.class);
    Subchannel oldSubchannel = mock(Subchannel.class);
    Subchannel newSubchannel = mock(Subchannel.class);

    for (Subchannel subchannel : Lists.newArrayList(removedSubchannel, oldSubchannel,
        newSubchannel)) {
      when(subchannel.getAttributes()).thenReturn(Attributes.newBuilder().set(STATE_INFO,
          new AtomicReference<ConnectivityStateInfo>(
              ConnectivityStateInfo.forNonError(READY))).build());
    }

    FakeSocketAddress removedAddr = new FakeSocketAddress("removed");
    FakeSocketAddress oldAddr = new FakeSocketAddress("old");
    FakeSocketAddress newAddr = new FakeSocketAddress("new");

    final Map<EquivalentAddressGroup, Subchannel> subchannels2 = Maps.newHashMap();
    subchannels2.put(new EquivalentAddressGroup(removedAddr), removedSubchannel);
    subchannels2.put(new EquivalentAddressGroup(oldAddr), oldSubchannel);

    List<ResolvedServerInfoGroup> currentServers = Lists.newArrayList(
        ResolvedServerInfoGroup.builder()
            .add(new ResolvedServerInfo(removedAddr))
            .add(new ResolvedServerInfo(oldAddr))
            .build());

    when(mockHelper.createSubchannel(any(EquivalentAddressGroup.class), any(Attributes.class)))
        .then(new Answer<Subchannel>() {
          @Override
          public Subchannel answer(InvocationOnMock invocation) throws Throwable {
            Object[] args = invocation.getArguments();
            return subchannels2.get(args[0]);
          }
        });

    loadBalancer.handleResolvedAddresses(currentServers, affinity);

    InOrder inOrder = inOrder(mockHelper);

    inOrder.verify(mockHelper).updatePicker(pickerCaptor.capture());
    Picker picker = pickerCaptor.getValue();
    assertNull(picker.getStatus());
    assertThat(picker.getList()).containsExactly(removedSubchannel, oldSubchannel);

    verify(removedSubchannel, times(1)).requestConnection();
    verify(oldSubchannel, times(1)).requestConnection();

    assertThat(loadBalancer.getSubchannels()).containsExactly(removedSubchannel,
        oldSubchannel);

    subchannels2.clear();
    subchannels2.put(new EquivalentAddressGroup(oldAddr), oldSubchannel);
    subchannels2.put(new EquivalentAddressGroup(newAddr), newSubchannel);

    List<ResolvedServerInfoGroup> latestServers = Lists.newArrayList(
        ResolvedServerInfoGroup.builder()
            .add(new ResolvedServerInfo(oldAddr))
            .add(new ResolvedServerInfo(newAddr))
            .build());

    loadBalancer.handleResolvedAddresses(latestServers, affinity);

    verify(newSubchannel, times(1)).requestConnection();
    verify(removedSubchannel, times(1)).shutdown();

    assertThat(loadBalancer.getSubchannels()).containsExactly(oldSubchannel,
        newSubchannel);

    verify(mockHelper, times(3)).createSubchannel(any(EquivalentAddressGroup.class),
        any(Attributes.class));
    inOrder.verify(mockHelper).updatePicker(pickerCaptor.capture());

    picker = pickerCaptor.getValue();
    assertNull(picker.getStatus());
    assertThat(picker.getList()).containsExactly(oldSubchannel, newSubchannel);

    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void pickAfterStateChangeBeforeResolution() throws Exception {
    loadBalancer.handleSubchannelState(mockSubchannel,
        ConnectivityStateInfo.forNonError(ConnectivityState.READY));
    verifyNoMoreInteractions(mockSubchannel);
    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void pickAfterStateChangeAndResolutionError() throws Exception {
    loadBalancer.handleSubchannelState(mockSubchannel,
        ConnectivityStateInfo.forNonError(ConnectivityState.READY));
    verifyNoMoreInteractions(mockSubchannel);
    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void pickAfterStateChange() throws Exception {
    InOrder inOrder = inOrder(mockHelper);
    when(mockHelper.createSubchannel(any(EquivalentAddressGroup.class), any(Attributes.class)))
        .then(new Answer<Subchannel>() {
          @Override
          public Subchannel answer(InvocationOnMock invocation) throws Throwable {
            return createMockSubchannel();
          }
        });
    loadBalancer.handleResolvedAddresses(Lists.newArrayList(servers.keySet()), Attributes.EMPTY);
    Subchannel subchannel = loadBalancer.getSubchannels().iterator().next();
    AtomicReference<ConnectivityStateInfo> subchannelStateInfo = subchannel.getAttributes().get(
        STATE_INFO);

    inOrder.verify(mockHelper).updatePicker(isA(Picker.class));
    assertThat(subchannelStateInfo.get()).isEqualTo(ConnectivityStateInfo.forNonError(IDLE));

    loadBalancer.handleSubchannelState(subchannel,
        ConnectivityStateInfo.forNonError(ConnectivityState.READY));
    inOrder.verify(mockHelper).updatePicker(pickerCaptor.capture());
    assertNull(pickerCaptor.getValue().getStatus());
    assertThat(subchannelStateInfo.get()).isEqualTo(
        ConnectivityStateInfo.forNonError(ConnectivityState.READY));

    Status error = Status.UNKNOWN.withDescription("¯\\_(ツ)_//¯");
    loadBalancer.handleSubchannelState(subchannel,
        ConnectivityStateInfo.forTransientFailure(error));
    assertThat(subchannelStateInfo.get()).isEqualTo(
        ConnectivityStateInfo.forTransientFailure(error));
    inOrder.verify(mockHelper).updatePicker(pickerCaptor.capture());
    assertNull(pickerCaptor.getValue().getStatus());

    loadBalancer.handleSubchannelState(subchannel,
        ConnectivityStateInfo.forNonError(ConnectivityState.IDLE));
    inOrder.verify(mockHelper).updatePicker(pickerCaptor.capture());
    assertNull(pickerCaptor.getValue().getStatus());
    assertThat(subchannelStateInfo.get()).isEqualTo(
        ConnectivityStateInfo.forNonError(ConnectivityState.IDLE));

    verify(subchannel, times(2)).requestConnection();
    verify(mockHelper, times(3)).createSubchannel(any(EquivalentAddressGroup.class),
        any(Attributes.class));
    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void pickerRoundRobin() throws Exception {
    Subchannel subchannel = mock(Subchannel.class);
    Subchannel subchannel1 = mock(Subchannel.class);
    Subchannel subchannel2 = mock(Subchannel.class);

    Picker picker = new Picker(Collections.unmodifiableList(
        Lists.<Subchannel>newArrayList(subchannel, subchannel1, subchannel2)), null);

    assertThat(picker.getList()).containsExactly(subchannel, subchannel1, subchannel2);

    assertEquals(subchannel,
        picker.pickSubchannel(Attributes.EMPTY, new Metadata()).getSubchannel());
    assertEquals(subchannel1,
        picker.pickSubchannel(Attributes.EMPTY, new Metadata()).getSubchannel());
    assertEquals(subchannel2,
        picker.pickSubchannel(Attributes.EMPTY, new Metadata()).getSubchannel());
    assertEquals(subchannel,
        picker.pickSubchannel(Attributes.EMPTY, new Metadata()).getSubchannel());
  }

  @Test
  public void pickerEmptyList() throws Exception {
    Picker picker = new Picker(Lists.<Subchannel>newArrayList(), Status.UNKNOWN);

    assertEquals(null, picker.pickSubchannel(Attributes.EMPTY, new Metadata()).getSubchannel());
    assertEquals(Status.UNKNOWN,
        picker.pickSubchannel(Attributes.EMPTY, new Metadata()).getStatus());
  }

  @Test
  public void nameResolutionErrorWithNoChannels() throws Exception {
    Status error = Status.NOT_FOUND.withDescription("nameResolutionError");
    loadBalancer.handleNameResolutionError(error);
    verify(mockHelper).updatePicker(pickerCaptor.capture());
    LoadBalancer2.PickResult pickResult = pickerCaptor.getValue().pickSubchannel(Attributes.EMPTY,
        new Metadata());
    assertNull(pickResult.getSubchannel());
    assertEquals(error, pickResult.getStatus());
    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void nameResolutionErrorWithActiveChannels() throws Exception {
    Subchannel readySubchannel = subchannels.values().iterator().next();
    readySubchannel.getAttributes().get(STATE_INFO).set(ConnectivityStateInfo.forNonError(READY));
    loadBalancer.handleResolvedAddresses(Lists.newArrayList(servers.keySet()), affinity);
    loadBalancer.handleNameResolutionError(Status.NOT_FOUND.withDescription("nameResolutionError"));

    verify(mockHelper, times(3)).createSubchannel(any(EquivalentAddressGroup.class),
        any(Attributes.class));
    verify(mockHelper, times(2)).updatePicker(pickerCaptor.capture());

    LoadBalancer2.PickResult pickResult = pickerCaptor.getValue().pickSubchannel(Attributes.EMPTY,
        new Metadata());
    assertEquals(readySubchannel, pickResult.getSubchannel());
    assertEquals(Status.OK.getCode(), pickResult.getStatus().getCode());

    LoadBalancer2.PickResult pickResult2 = pickerCaptor.getValue().pickSubchannel(Attributes.EMPTY,
        new Metadata());
    assertEquals(readySubchannel, pickResult2.getSubchannel());
    verifyNoMoreInteractions(mockHelper);
  }

  private Subchannel createMockSubchannel() {
    Subchannel subchannel = mock(Subchannel.class);
    when(subchannel.getAttributes()).thenReturn(Attributes.newBuilder().set(STATE_INFO,
        new AtomicReference<ConnectivityStateInfo>(
            ConnectivityStateInfo.forNonError(IDLE))).build());
    return subchannel;
  }

  private static class FakeSocketAddress extends SocketAddress {
    final String name;

    FakeSocketAddress(String name) {
      this.name = name;
    }

    @Override
    public String toString() {
      return "FakeSocketAddress-" + name;
    }
  }
}
