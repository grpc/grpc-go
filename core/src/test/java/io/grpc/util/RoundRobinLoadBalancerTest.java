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

package io.grpc.util;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;
import static io.grpc.util.RoundRobinLoadBalancerFactory.RoundRobinLoadBalancer.STATE_INFO;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.atLeast;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doReturn;
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
import io.grpc.Metadata;
import io.grpc.Metadata.Key;
import io.grpc.Status;
import io.grpc.internal.GrpcAttributes;
import io.grpc.util.RoundRobinLoadBalancerFactory.Picker;
import io.grpc.util.RoundRobinLoadBalancerFactory.Ref;
import io.grpc.util.RoundRobinLoadBalancerFactory.RoundRobinLoadBalancer;
import java.net.SocketAddress;
import java.util.Collections;
import java.util.HashMap;
import java.util.Iterator;
import java.util.List;
import java.util.Map;
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
  private static final Attributes.Key<String> MAJOR_KEY = Attributes.Key.create("major-key");
  private Attributes affinity = Attributes.newBuilder().set(MAJOR_KEY, "I got the keys").build();

  @Captor
  private ArgumentCaptor<Picker> pickerCaptor;
  @Captor
  private ArgumentCaptor<ConnectivityState> stateCaptor;
  @Captor
  private ArgumentCaptor<EquivalentAddressGroup> eagCaptor;
  @Mock
  private Helper mockHelper;

  @Mock // This LoadBalancer doesn't use any of the arg fields, as verified in tearDown().
  private PickSubchannelArgs mockArgs;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    for (int i = 0; i < 3; i++) {
      SocketAddress addr = new FakeSocketAddress("server" + i);
      EquivalentAddressGroup eag = new EquivalentAddressGroup(addr);
      servers.add(eag);
      Subchannel sc = mock(Subchannel.class);
      when(sc.getAddresses()).thenReturn(eag);
      subchannels.put(eag, sc);
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

    verify(mockHelper, times(2))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());

    assertEquals(CONNECTING, stateCaptor.getAllValues().get(0));
    assertEquals(READY, stateCaptor.getAllValues().get(1));
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
          new Ref<ConnectivityStateInfo>(
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

    inOrder.verify(mockHelper).updateBalancingState(eq(READY), pickerCaptor.capture());
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
    inOrder.verify(mockHelper).updateBalancingState(eq(READY), pickerCaptor.capture());

    picker = pickerCaptor.getValue();
    assertNull(picker.getStatus());
    assertThat(picker.getList()).containsExactly(oldSubchannel, newSubchannel);

    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void pickAfterStateChange() throws Exception {
    InOrder inOrder = inOrder(mockHelper);
    loadBalancer.handleResolvedAddressGroups(servers, Attributes.EMPTY);
    Subchannel subchannel = loadBalancer.getSubchannels().iterator().next();
    Ref<ConnectivityStateInfo> subchannelStateInfo = subchannel.getAttributes().get(
        STATE_INFO);

    inOrder.verify(mockHelper).updateBalancingState(eq(CONNECTING), isA(Picker.class));
    assertThat(subchannelStateInfo.value).isEqualTo(ConnectivityStateInfo.forNonError(IDLE));

    loadBalancer.handleSubchannelState(subchannel,
        ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(mockHelper).updateBalancingState(eq(READY), pickerCaptor.capture());
    assertNull(pickerCaptor.getValue().getStatus());
    assertThat(subchannelStateInfo.value).isEqualTo(
        ConnectivityStateInfo.forNonError(READY));

    Status error = Status.UNKNOWN.withDescription("¯\\_(ツ)_//¯");
    loadBalancer.handleSubchannelState(subchannel,
        ConnectivityStateInfo.forTransientFailure(error));
    assertThat(subchannelStateInfo.value).isEqualTo(
        ConnectivityStateInfo.forTransientFailure(error));
    inOrder.verify(mockHelper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    assertNull(pickerCaptor.getValue().getStatus());

    loadBalancer.handleSubchannelState(subchannel,
        ConnectivityStateInfo.forNonError(IDLE));
    inOrder.verify(mockHelper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    assertNull(pickerCaptor.getValue().getStatus());
    assertThat(subchannelStateInfo.value).isEqualTo(
        ConnectivityStateInfo.forNonError(IDLE));

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

    Picker picker = new Picker(Collections.unmodifiableList(Lists.<Subchannel>newArrayList(
        subchannel, subchannel1, subchannel2)), null /* status */, null /* stickinessState */);

    assertThat(picker.getList()).containsExactly(subchannel, subchannel1, subchannel2);

    assertEquals(subchannel, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(subchannel1, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(subchannel2, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(subchannel, picker.pickSubchannel(mockArgs).getSubchannel());
  }

  @Test
  public void pickerEmptyList() throws Exception {
    Picker picker =
        new Picker(Lists.<Subchannel>newArrayList(), Status.UNKNOWN, null /* stickinessState */);

    assertEquals(null, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(Status.UNKNOWN,
        picker.pickSubchannel(mockArgs).getStatus());
  }

  @Test
  public void nameResolutionErrorWithNoChannels() throws Exception {
    Status error = Status.NOT_FOUND.withDescription("nameResolutionError");
    loadBalancer.handleNameResolutionError(error);
    verify(mockHelper).updateBalancingState(eq(TRANSIENT_FAILURE), pickerCaptor.capture());
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
    verify(mockHelper, times(3))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());

    Iterator<ConnectivityState> stateIterator = stateCaptor.getAllValues().iterator();
    assertEquals(CONNECTING, stateIterator.next());
    assertEquals(READY, stateIterator.next());
    assertEquals(TRANSIENT_FAILURE, stateIterator.next());

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

    verify(mockHelper, times(6))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    Iterator<ConnectivityState> stateIterator = stateCaptor.getAllValues().iterator();
    Iterator<Picker> pickers = pickerCaptor.getAllValues().iterator();
    // The picker is incrementally updated as subchannels become READY
    assertEquals(CONNECTING, stateIterator.next());
    assertThat(pickers.next().getList()).isEmpty();
    assertEquals(READY, stateIterator.next());
    assertThat(pickers.next().getList()).containsExactly(sc1);
    assertEquals(READY, stateIterator.next());
    assertThat(pickers.next().getList()).containsExactly(sc1, sc2);
    assertEquals(READY, stateIterator.next());
    assertThat(pickers.next().getList()).containsExactly(sc1, sc2, sc3);
    // The IDLE subchannel is dropped from the picker, but a reconnection is requested
    assertEquals(READY, stateIterator.next());
    assertThat(pickers.next().getList()).containsExactly(sc1, sc3);
    verify(sc2, times(2)).requestConnection();
    // The failing subchannel is dropped from the picker, with no requested reconnect
    assertEquals(READY, stateIterator.next());
    assertThat(pickers.next().getList()).containsExactly(sc1);
    verify(sc3, times(1)).requestConnection();
    assertThat(stateIterator.hasNext()).isFalse();
    assertThat(pickers.hasNext()).isFalse();
  }

  @Test
  public void noStickinessEnabled_withStickyHeader() {
    loadBalancer.handleResolvedAddressGroups(servers, Attributes.EMPTY);
    for (Subchannel subchannel : subchannels.values()) {
      loadBalancer.handleSubchannelState(subchannel, ConnectivityStateInfo.forNonError(READY));
    }
    verify(mockHelper, times(4))
        .updateBalancingState(any(ConnectivityState.class), pickerCaptor.capture());
    Picker picker = pickerCaptor.getValue();

    Key<String> stickinessKey = Key.of("my-sticky-key", Metadata.ASCII_STRING_MARSHALLER);
    Metadata headerWithStickinessValue = new Metadata();
    headerWithStickinessValue.put(stickinessKey, "my-sticky-value");
    doReturn(headerWithStickinessValue).when(mockArgs).getHeaders();

    Iterator<Subchannel> subchannelIterator = loadBalancer.getSubchannels().iterator();
    Subchannel sc1 = subchannelIterator.next();
    Subchannel sc2 = subchannelIterator.next();
    Subchannel sc3 = subchannelIterator.next();
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(sc2, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(sc3, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());

    assertNull(loadBalancer.getStickinessMapForTest());
  }

  @Test
  public void stickinessEnabled_withoutStickyHeader() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("stickinessMetadataKey", "my-sticky-key");
    Attributes attributes = Attributes.newBuilder()
        .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig).build();
    loadBalancer.handleResolvedAddressGroups(servers, attributes);
    for (Subchannel subchannel : subchannels.values()) {
      loadBalancer.handleSubchannelState(subchannel, ConnectivityStateInfo.forNonError(READY));
    }
    verify(mockHelper, times(4))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    Picker picker = pickerCaptor.getValue();

    doReturn(new Metadata()).when(mockArgs).getHeaders();

    Iterator<Subchannel> subchannelIterator = loadBalancer.getSubchannels().iterator();
    Subchannel sc1 = subchannelIterator.next();
    Subchannel sc2 = subchannelIterator.next();
    Subchannel sc3 = subchannelIterator.next();
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(sc2, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(sc3, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());

    verify(mockArgs, times(4)).getHeaders();
    assertNotNull(loadBalancer.getStickinessMapForTest());
    assertThat(loadBalancer.getStickinessMapForTest()).isEmpty();
  }

  @Test
  public void stickinessEnabled_withStickyHeader() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("stickinessMetadataKey", "my-sticky-key");
    Attributes attributes = Attributes.newBuilder()
        .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig).build();
    loadBalancer.handleResolvedAddressGroups(servers, attributes);
    for (Subchannel subchannel : subchannels.values()) {
      loadBalancer.handleSubchannelState(subchannel, ConnectivityStateInfo.forNonError(READY));
    }
    verify(mockHelper, times(4))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    Picker picker = pickerCaptor.getValue();

    Key<String> stickinessKey = Key.of("my-sticky-key", Metadata.ASCII_STRING_MARSHALLER);
    Metadata headerWithStickinessValue = new Metadata();
    headerWithStickinessValue.put(stickinessKey, "my-sticky-value");
    doReturn(headerWithStickinessValue).when(mockArgs).getHeaders();

    Subchannel sc1 = loadBalancer.getSubchannels().iterator().next();
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());

    verify(mockArgs, atLeast(4)).getHeaders();
    assertNotNull(loadBalancer.getStickinessMapForTest());
    assertThat(loadBalancer.getStickinessMapForTest()).hasSize(1);
  }

  @Test
  public void stickinessEnabled_withDifferentStickyHeaders() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("stickinessMetadataKey", "my-sticky-key");
    Attributes attributes = Attributes.newBuilder()
        .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig).build();
    loadBalancer.handleResolvedAddressGroups(servers, attributes);
    for (Subchannel subchannel : subchannels.values()) {
      loadBalancer.handleSubchannelState(subchannel, ConnectivityStateInfo.forNonError(READY));
    }
    verify(mockHelper, times(4))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    Picker picker = pickerCaptor.getValue();

    Key<String> stickinessKey = Key.of("my-sticky-key", Metadata.ASCII_STRING_MARSHALLER);
    Metadata headerWithStickinessValue1 = new Metadata();
    headerWithStickinessValue1.put(stickinessKey, "my-sticky-value");

    Metadata headerWithStickinessValue2 = new Metadata();
    headerWithStickinessValue2.put(stickinessKey, "my-sticky-value2");

    Iterator<Subchannel> subchannelIterator = loadBalancer.getSubchannels().iterator();
    Subchannel sc1 = subchannelIterator.next();
    Subchannel sc2 = subchannelIterator.next();

    doReturn(headerWithStickinessValue1).when(mockArgs).getHeaders();
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());

    doReturn(headerWithStickinessValue2).when(mockArgs).getHeaders();
    assertEquals(sc2, picker.pickSubchannel(mockArgs).getSubchannel());

    doReturn(headerWithStickinessValue1).when(mockArgs).getHeaders();
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());

    doReturn(headerWithStickinessValue2).when(mockArgs).getHeaders();
    assertEquals(sc2, picker.pickSubchannel(mockArgs).getSubchannel());

    verify(mockArgs, atLeast(4)).getHeaders();
    assertNotNull(loadBalancer.getStickinessMapForTest());
    assertThat(loadBalancer.getStickinessMapForTest()).hasSize(2);
  }

  @Test
  public void stickiness_goToTransientFailure_pick_backToReady() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("stickinessMetadataKey", "my-sticky-key");
    Attributes attributes = Attributes.newBuilder()
        .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig).build();
    loadBalancer.handleResolvedAddressGroups(servers, attributes);
    for (Subchannel subchannel : subchannels.values()) {
      loadBalancer.handleSubchannelState(subchannel, ConnectivityStateInfo.forNonError(READY));
    }
    verify(mockHelper, times(4))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    Picker picker = pickerCaptor.getValue();

    Key<String> stickinessKey = Key.of("my-sticky-key", Metadata.ASCII_STRING_MARSHALLER);
    Metadata headerWithStickinessValue = new Metadata();
    headerWithStickinessValue.put(stickinessKey, "my-sticky-value");
    doReturn(headerWithStickinessValue).when(mockArgs).getHeaders();

    Iterator<Subchannel> subchannelIterator = loadBalancer.getSubchannels().iterator();
    Subchannel sc1 = subchannelIterator.next();
    Subchannel sc2 = subchannelIterator.next();

    // first pick
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());

    // go to transient failure
    loadBalancer
        .handleSubchannelState(sc1, ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE));

    verify(mockHelper, times(5))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    picker = pickerCaptor.getValue();
    // second pick
    assertEquals(sc2, picker.pickSubchannel(mockArgs).getSubchannel());

    // go back to ready
    loadBalancer.handleSubchannelState(sc1, ConnectivityStateInfo.forNonError(READY));

    verify(mockHelper, times(6))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    picker = pickerCaptor.getValue();
    // third pick
    assertEquals(sc2, picker.pickSubchannel(mockArgs).getSubchannel());

    verify(mockArgs, atLeast(3)).getHeaders();
    assertNotNull(loadBalancer.getStickinessMapForTest());
    assertThat(loadBalancer.getStickinessMapForTest()).hasSize(1);
  }

  @Test
  public void stickiness_goToTransientFailure_backToReady_pick() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("stickinessMetadataKey", "my-sticky-key");
    Attributes attributes = Attributes.newBuilder()
        .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig).build();
    loadBalancer.handleResolvedAddressGroups(servers, attributes);
    for (Subchannel subchannel : subchannels.values()) {
      loadBalancer.handleSubchannelState(subchannel, ConnectivityStateInfo.forNonError(READY));
    }
    verify(mockHelper, times(4))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    Picker picker = pickerCaptor.getValue();

    Key<String> stickinessKey = Key.of("my-sticky-key", Metadata.ASCII_STRING_MARSHALLER);
    Metadata headerWithStickinessValue1 = new Metadata();
    headerWithStickinessValue1.put(stickinessKey, "my-sticky-value");
    doReturn(headerWithStickinessValue1).when(mockArgs).getHeaders();

    Iterator<Subchannel> subchannelIterator = loadBalancer.getSubchannels().iterator();
    Subchannel sc1 = subchannelIterator.next();
    Subchannel sc2 = subchannelIterator.next();

    // first pick
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());

    // go to transient failure
    loadBalancer
        .handleSubchannelState(sc1, ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE));

    Metadata headerWithStickinessValue2 = new Metadata();
    headerWithStickinessValue2.put(stickinessKey, "my-sticky-value2");
    doReturn(headerWithStickinessValue2).when(mockArgs).getHeaders();
    verify(mockHelper, times(5))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    picker = pickerCaptor.getValue();
    // second pick with a different stickiness value
    assertEquals(sc2, picker.pickSubchannel(mockArgs).getSubchannel());

    // go back to ready
    loadBalancer.handleSubchannelState(sc1, ConnectivityStateInfo.forNonError(READY));

    doReturn(headerWithStickinessValue1).when(mockArgs).getHeaders();
    verify(mockHelper, times(6))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    picker = pickerCaptor.getValue();
    // third pick with my-sticky-value1
    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());

    verify(mockArgs, atLeast(3)).getHeaders();
    assertNotNull(loadBalancer.getStickinessMapForTest());
    assertThat(loadBalancer.getStickinessMapForTest()).hasSize(2);
  }

  @Test
  public void stickiness_oneSubchannelShutdown() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("stickinessMetadataKey", "my-sticky-key");
    Attributes attributes = Attributes.newBuilder()
        .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig).build();
    loadBalancer.handleResolvedAddressGroups(servers, attributes);
    for (Subchannel subchannel : subchannels.values()) {
      loadBalancer.handleSubchannelState(subchannel, ConnectivityStateInfo.forNonError(READY));
    }
    verify(mockHelper, times(4))
        .updateBalancingState(stateCaptor.capture(), pickerCaptor.capture());
    Picker picker = pickerCaptor.getValue();

    Key<String> stickinessKey = Key.of("my-sticky-key", Metadata.ASCII_STRING_MARSHALLER);
    Metadata headerWithStickinessValue = new Metadata();
    headerWithStickinessValue.put(stickinessKey, "my-sticky-value");
    doReturn(headerWithStickinessValue).when(mockArgs).getHeaders();

    Iterator<Subchannel> subchannelIterator = loadBalancer.getSubchannels().iterator();
    Subchannel sc1 = subchannelIterator.next();
    Subchannel sc2 = subchannelIterator.next();

    assertEquals(sc1, picker.pickSubchannel(mockArgs).getSubchannel());

    loadBalancer
        .handleSubchannelState(sc1, ConnectivityStateInfo.forNonError(ConnectivityState.SHUTDOWN));

    assertNull(loadBalancer.getStickinessMapForTest().get("my-sticky-value").value);

    assertEquals(sc2, picker.pickSubchannel(mockArgs).getSubchannel());
    assertThat(loadBalancer.getStickinessMapForTest()).hasSize(1);
    verify(mockArgs, atLeast(2)).getHeaders();
  }

  @Test
  public void stickiness_resolveTwice_metadataKeyChanged() {
    Map<String, Object> serviceConfig1 = new HashMap<String, Object>();
    serviceConfig1.put("stickinessMetadataKey", "my-sticky-key1");
    Attributes attributes1 = Attributes.newBuilder()
        .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig1).build();
    loadBalancer.handleResolvedAddressGroups(servers, attributes1);
    Map<String, ?> stickinessMap1 = loadBalancer.getStickinessMapForTest();

    Map<String, Object> serviceConfig2 = new HashMap<String, Object>();
    serviceConfig2.put("stickinessMetadataKey", "my-sticky-key2");
    Attributes attributes2 = Attributes.newBuilder()
        .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig2).build();
    loadBalancer.handleResolvedAddressGroups(servers, attributes2);
    Map<String, ?> stickinessMap2 = loadBalancer.getStickinessMapForTest();

    assertNotSame(stickinessMap1, stickinessMap2);
  }

  @Test
  public void stickiness_resolveTwice_metadataKeyUnChanged() {
    Map<String, Object> serviceConfig1 = new HashMap<String, Object>();
    serviceConfig1.put("stickinessMetadataKey", "my-sticky-key1");
    Attributes attributes1 = Attributes.newBuilder()
        .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig1).build();
    loadBalancer.handleResolvedAddressGroups(servers, attributes1);
    Map<String, ?> stickinessMap1 = loadBalancer.getStickinessMapForTest();

    loadBalancer.handleResolvedAddressGroups(servers, attributes1);
    Map<String, ?> stickinessMap2 = loadBalancer.getStickinessMapForTest();

    assertSame(stickinessMap1, stickinessMap2);
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
