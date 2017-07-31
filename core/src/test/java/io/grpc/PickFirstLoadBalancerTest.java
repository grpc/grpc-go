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

package io.grpc;

import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyListOf;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.Lists;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.PickFirstBalancerFactory.PickFirstBalancer;
import java.net.SocketAddress;
import java.util.List;
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


/** Unit test for {@link PickFirstBalancerFactory}. */
@RunWith(JUnit4.class)
public class PickFirstLoadBalancerTest {
  private PickFirstBalancer loadBalancer;
  private List<EquivalentAddressGroup> servers = Lists.newArrayList();
  private List<SocketAddress> socketAddresses = Lists.newArrayList();

  private static final Attributes.Key<String> FOO = Attributes.Key.create("foo");
  private Attributes affinity = Attributes.newBuilder().set(FOO, "bar").build();

  @Captor
  private ArgumentCaptor<SubchannelPicker> pickerCaptor;
  @Captor
  private ArgumentCaptor<Attributes> attrsCaptor;
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
      servers.add(new EquivalentAddressGroup(addr));
      socketAddresses.add(addr);
    }

    when(mockSubchannel.getAddresses()).thenThrow(new UnsupportedOperationException());
    when(mockHelper.createSubchannel(
        anyListOf(EquivalentAddressGroup.class), any(Attributes.class)))
        .thenReturn(mockSubchannel);

    loadBalancer = (PickFirstBalancer) PickFirstBalancerFactory.getInstance().newLoadBalancer(
        mockHelper);
  }

  @After
  public void tearDown() throws Exception {
    verifyNoMoreInteractions(mockArgs);
  }

  @Test
  public void pickAfterResolved() throws Exception {
    loadBalancer.handleResolvedAddressGroups(servers, affinity);

    verify(mockHelper).createSubchannel(eq(servers), attrsCaptor.capture());
    verify(mockHelper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    verify(mockSubchannel).requestConnection();

    assertEquals(pickerCaptor.getValue().pickSubchannel(mockArgs),
        pickerCaptor.getValue().pickSubchannel(mockArgs));

    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void pickAfterResolvedAndUnchanged() throws Exception {
    loadBalancer.handleResolvedAddressGroups(servers, affinity);
    verify(mockSubchannel).requestConnection();
    loadBalancer.handleResolvedAddressGroups(servers, affinity);
    verifyNoMoreInteractions(mockSubchannel);

    verify(mockHelper).createSubchannel(anyListOf(EquivalentAddressGroup.class),
        any(Attributes.class));
    verify(mockHelper)
        .updateBalancingState(isA(ConnectivityState.class), isA(SubchannelPicker.class));
    // Updating the subchannel addresses is unnecessary, but doesn't hurt anything
    verify(mockHelper).updateSubchannelAddresses(
        eq(mockSubchannel), anyListOf(EquivalentAddressGroup.class));

    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void pickAfterResolvedAndChanged() throws Exception {
    SocketAddress socketAddr = new FakeSocketAddress("newserver");
    List<EquivalentAddressGroup> newServers =
        Lists.newArrayList(new EquivalentAddressGroup(socketAddr));

    InOrder inOrder = inOrder(mockHelper);

    loadBalancer.handleResolvedAddressGroups(servers, affinity);
    inOrder.verify(mockHelper).createSubchannel(eq(servers), any(Attributes.class));
    inOrder.verify(mockHelper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    verify(mockSubchannel).requestConnection();
    assertEquals(mockSubchannel, pickerCaptor.getValue().pickSubchannel(mockArgs).getSubchannel());

    loadBalancer.handleResolvedAddressGroups(newServers, affinity);
    inOrder.verify(mockHelper).updateSubchannelAddresses(eq(mockSubchannel), eq(newServers));

    verifyNoMoreInteractions(mockSubchannel);
    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void stateChangeBeforeResolution() throws Exception {
    loadBalancer.handleSubchannelState(mockSubchannel, ConnectivityStateInfo.forNonError(READY));

    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void pickAfterStateChangeAfterResolution() throws Exception {
    loadBalancer.handleResolvedAddressGroups(servers, affinity);
    verify(mockHelper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    Subchannel subchannel = pickerCaptor.getValue().pickSubchannel(mockArgs).getSubchannel();
    reset(mockHelper);

    InOrder inOrder = inOrder(mockHelper);

    Status error = Status.UNAVAILABLE.withDescription("boom!");
    loadBalancer.handleSubchannelState(subchannel,
        ConnectivityStateInfo.forTransientFailure(error));
    inOrder.verify(mockHelper).updateBalancingState(eq(TRANSIENT_FAILURE), pickerCaptor.capture());
    assertEquals(error, pickerCaptor.getValue().pickSubchannel(mockArgs).getStatus());

    loadBalancer.handleSubchannelState(subchannel, ConnectivityStateInfo.forNonError(IDLE));
    inOrder.verify(mockHelper).updateBalancingState(eq(IDLE), pickerCaptor.capture());
    assertEquals(Status.OK, pickerCaptor.getValue().pickSubchannel(mockArgs).getStatus());

    loadBalancer.handleSubchannelState(subchannel, ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(mockHelper).updateBalancingState(eq(READY), pickerCaptor.capture());
    assertEquals(subchannel, pickerCaptor.getValue().pickSubchannel(mockArgs).getSubchannel());

    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void nameResolutionError() throws Exception {
    Status error = Status.NOT_FOUND.withDescription("nameResolutionError");
    loadBalancer.handleNameResolutionError(error);
    verify(mockHelper).updateBalancingState(eq(TRANSIENT_FAILURE), pickerCaptor.capture());
    PickResult pickResult = pickerCaptor.getValue().pickSubchannel(mockArgs);
    assertEquals(null, pickResult.getSubchannel());
    assertEquals(error, pickResult.getStatus());
    verify(mockSubchannel, never()).requestConnection();
    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void nameResolutionSuccessAfterError() throws Exception {
    InOrder inOrder = inOrder(mockHelper);

    loadBalancer.handleNameResolutionError(Status.NOT_FOUND.withDescription("nameResolutionError"));
    inOrder.verify(mockHelper)
        .updateBalancingState(any(ConnectivityState.class), any(SubchannelPicker.class));
    verify(mockSubchannel, never()).requestConnection();

    loadBalancer.handleResolvedAddressGroups(servers, affinity);
    inOrder.verify(mockHelper).createSubchannel(eq(servers), eq(Attributes.EMPTY));
    inOrder.verify(mockHelper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    verify(mockSubchannel).requestConnection();

    assertEquals(mockSubchannel, pickerCaptor.getValue().pickSubchannel(mockArgs)
        .getSubchannel());

    assertEquals(pickerCaptor.getValue().pickSubchannel(mockArgs),
        pickerCaptor.getValue().pickSubchannel(mockArgs));

    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void nameResolutionErrorWithStateChanges() throws Exception {
    InOrder inOrder = inOrder(mockHelper);

    loadBalancer.handleSubchannelState(mockSubchannel,
        ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE));
    Status error = Status.NOT_FOUND.withDescription("nameResolutionError");
    loadBalancer.handleNameResolutionError(error);
    inOrder.verify(mockHelper).updateBalancingState(eq(TRANSIENT_FAILURE), pickerCaptor.capture());

    PickResult pickResult = pickerCaptor.getValue().pickSubchannel(mockArgs);
    assertEquals(null, pickResult.getSubchannel());
    assertEquals(error, pickResult.getStatus());

    loadBalancer.handleSubchannelState(mockSubchannel, ConnectivityStateInfo.forNonError(READY));
    Status error2 = Status.NOT_FOUND.withDescription("nameResolutionError2");
    loadBalancer.handleNameResolutionError(error2);
    inOrder.verify(mockHelper).updateBalancingState(eq(TRANSIENT_FAILURE), pickerCaptor.capture());

    pickResult = pickerCaptor.getValue().pickSubchannel(mockArgs);
    assertEquals(null, pickResult.getSubchannel());
    assertEquals(error2, pickResult.getStatus());

    verifyNoMoreInteractions(mockHelper);
  }

  @Test
  public void requestConnection() {
    loadBalancer.handleResolvedAddressGroups(servers, affinity);
    loadBalancer.handleSubchannelState(mockSubchannel, ConnectivityStateInfo.forNonError(IDLE));
    verify(mockHelper).updateBalancingState(eq(IDLE), pickerCaptor.capture());
    SubchannelPicker picker = pickerCaptor.getValue();

    verify(mockSubchannel).requestConnection();
    picker.requestConnection();
    verify(mockSubchannel, times(2)).requestConnection();
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
