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
import static io.grpc.util.RoundRobinLoadBalancerFactory.RoundRobinLoadBalancer.STATE_INFO;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.doAnswer;
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
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.Status;
import io.grpc.util.RoundRobinLoadBalancerFactory.Picker;
import io.grpc.util.RoundRobinLoadBalancerFactory.RoundRobinLoadBalancer;
import java.net.SocketAddress;
import java.util.Collections;
import java.util.Iterator;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.After;
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

/** Unit test for {@link RoundRobinLoadBalancerFactory}. */
@RunWith(JUnit4.class)
public class RoundRobinLoadBalancerTest {
  private RoundRobinLoadBalancer loadBalancer;
  private List<EquivalentAddressGroup> servers = Lists.newArrayList();
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
  @Mock // This LoadBalancer doesn't use any of the arg fields, as verified in tearDown().
  private PickSubchannelArgs mockArgs;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    for (int i = 0; i < 3; i++) {
      SocketAddress addr = new FakeSocketAddress("server" + i);
      EquivalentAddressGroup eag = new EquivalentAddressGroup(addr);
      servers.add(eag);
      subchannels.put(eag, mock(Subchannel.class));
    }

    when(mockHelper.createSubchannel(any(EquivalentAddressGroup.class), any(Attributes.class)))
        .then(new Answer<Subchannel>() {
          @Override
          public Subchannel answer(InvocationOnMock invocation) throws Throwable {
            Object[] args = invocation.getArguments();
            Subchannel subchannel = subchannels.get(args[0]);
            when(subchannel.getAttributes()).thenReturn((Attributes) args[1]);
            return subchannel;
          }
        });

    loadBalancer = (RoundRobinLoadBalancer) RoundRobinLoadBalancerFactory.getInstance()
        .newLoadBalancer(mockHelper);
  }

  @After
  public void tearDown() throws Exception {
    verifyNoMoreInteractions(mockArgs);
  }

  @Test
  public void pickAfterResolved() throws Exception {
    final Subchannel readySubchannel = subchannels.values().iterator().next();
    loadBalancer.handleResolvedAddressGroups(servers, affinity);
    loadBalancer.handleSubchannelState(readySubchannel, ConnectivityStateInfo.forNonError(READY));

    verify(mockHelper, times(3)).createSubchannel(eagCaptor.capture(),
        any(Attributes.class));

    assertThat(eagCaptor.getAllValues()).containsAllIn(subchannels.keySet());
    for (Subchannel subchannel : subchannels.values()) {
      verify(subchannel).requestConnection();
      verify(subchannel, never()).shutdown();
    }

    verify(mockHelper, times(2)).updatePicker(pickerCaptor.capture());

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

    List<EquivalentAddressGroup> currentServers =
        Lists.newArrayList(
            new EquivalentAddressGroup(removedAddr),
            new EquivalentAddressGroup(oldAddr));

    doAnswer(new Answer<Subchannel>() {
      @Override
      public Subchannel answer(InvocationOnMock invocation) throws Throwable {
        Object[] args = invocation.getArguments();
        return subchannels2.get(args[0]);
      }
    }).when(mockHelper).createSubchannel(any(EquivalentAddressGroup.class), any(Attributes.class));

    loadBalancer.handleResolvedAddressGroups(currentServers, affinity);

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

    List<EquivalentAddressGroup> latestServers =
        Lists.newArrayList(
            new EquivalentAddressGroup(oldAddr),
            new EquivalentAddressGroup(newAddr));

    loadBalancer.handleResolvedAddressGroups(latestServers, affinity);

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
    loadBalancer.handleResolvedAddressGroups(servers, Attributes.EMPTY);
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

    assertEquals(subchannel, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(subchannel1, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(subchannel2, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(subchannel, picker.pickSubchannel(mockArgs).getSubchannel());
  }

  @Test
  public void pickerEmptyList() throws Exception {
    Picker picker = new Picker(Lists.<Subchannel>newArrayList(), Status.UNKNOWN);

    assertEquals(null, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(Status.UNKNOWN,
        picker.pickSubchannel(mockArgs).getStatus());
  }

  @Test
  public void nameResolutionErrorWithNoChannels() throws Exception {
    Status error = Status.NOT_FOUND.withDescription("nameResolutionError");
    loadBalancer.handleNameResolutionError(error);
    verify(mockHelper).updatePicker(pickerCaptor.capture());
    LoadBalancer.PickResult pickResult = pickerCaptor.getValue().pickSubchannel(mockArgs);
    assertNull(pickResult.getSubchannel());
    assertEquals(error, pickResult.getStatus());
    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void nameResolutionErrorWithActiveChannels() throws Exception {
    final Subchannel readySubchannel = subchannels.values().iterator().next();
    loadBalancer.handleResolvedAddressGroups(servers, affinity);
    loadBalancer.handleSubchannelState(readySubchannel, ConnectivityStateInfo.forNonError(READY));
    loadBalancer.handleNameResolutionError(Status.NOT_FOUND.withDescription("nameResolutionError"));

    verify(mockHelper, times(3)).createSubchannel(any(EquivalentAddressGroup.class),
        any(Attributes.class));
    verify(mockHelper, times(3)).updatePicker(pickerCaptor.capture());

    LoadBalancer.PickResult pickResult = pickerCaptor.getValue().pickSubchannel(mockArgs);
    assertEquals(readySubchannel, pickResult.getSubchannel());
    assertEquals(Status.OK.getCode(), pickResult.getStatus().getCode());

    LoadBalancer.PickResult pickResult2 = pickerCaptor.getValue().pickSubchannel(mockArgs);
    assertEquals(readySubchannel, pickResult2.getSubchannel());
    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void subchannelStateIsolation() throws Exception {
    Iterator<Subchannel> subchannelIterator = subchannels.values().iterator();
    Subchannel sc1 = subchannelIterator.next();
    Subchannel sc2 = subchannelIterator.next();
    Subchannel sc3 = subchannelIterator.next();

    loadBalancer.handleResolvedAddressGroups(servers, Attributes.EMPTY);
    verify(sc1, times(1)).requestConnection();
    verify(sc2, times(1)).requestConnection();
    verify(sc3, times(1)).requestConnection();

    loadBalancer.handleSubchannelState(sc1, ConnectivityStateInfo.forNonError(READY));
    loadBalancer.handleSubchannelState(sc2, ConnectivityStateInfo.forNonError(READY));
    loadBalancer.handleSubchannelState(sc3, ConnectivityStateInfo.forNonError(READY));
    loadBalancer.handleSubchannelState(sc2, ConnectivityStateInfo.forNonError(IDLE));
    loadBalancer
        .handleSubchannelState(sc3, ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE));

    verify(mockHelper, times(6)).updatePicker(pickerCaptor.capture());
    Iterator<Picker> pickers = pickerCaptor.getAllValues().iterator();
    // The picker is incrementally updated as subchannels become READY
    assertThat(pickers.next().getList()).isEmpty();
    assertThat(pickers.next().getList()).containsExactly(sc1);
    assertThat(pickers.next().getList()).containsExactly(sc1, sc2);
    assertThat(pickers.next().getList()).containsExactly(sc1, sc2, sc3);
    // The IDLE subchannel is dropped from the picker, but a reconnection is requested
    assertThat(pickers.next().getList()).containsExactly(sc1, sc3);
    verify(sc2, times(2)).requestConnection();
    // The failing subchannel is dropped from the picker, with no requested reconnect
    assertThat(pickers.next().getList()).containsExactly(sc1);
    verify(sc3, times(1)).requestConnection();
    assertThat(pickers.hasNext()).isFalse();
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
