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

package io.grpc;

import static org.junit.Assert.assertSame;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.base.Supplier;

import io.grpc.TransportManager.InterimTransport;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.List;

/** Unit test for {@link DummyLoadBalancerFactory}. */
@RunWith(JUnit4.class)
public class DummyLoadBalancerTest {
  private LoadBalancer<Transport> loadBalancer;

  private List<List<ResolvedServerInfo>> servers;
  private EquivalentAddressGroup addressGroup;

  @Mock private TransportManager<Transport> mockTransportManager;
  @Mock private Transport mockTransport;
  @Mock private InterimTransport<Transport> mockInterimTransport;
  @Mock private Transport mockInterimTransportAsTransport;
  @Captor private ArgumentCaptor<Supplier<Transport>> transportSupplierCaptor;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    loadBalancer = DummyLoadBalancerFactory.getInstance().newLoadBalancer(
        "fakeservice", mockTransportManager);
    servers = new ArrayList<List<ResolvedServerInfo>>();
    servers.add(new ArrayList<ResolvedServerInfo>());
    ArrayList<SocketAddress> addresses = new ArrayList<SocketAddress>();
    for (int i = 0; i < 3; i++) {
      SocketAddress addr = new FakeSocketAddress("server" + i);
      servers.get(0).add(new ResolvedServerInfo(addr, Attributes.EMPTY));
      addresses.add(addr);
    }
    addressGroup = new EquivalentAddressGroup(addresses);
    when(mockTransportManager.getTransport(eq(addressGroup))).thenReturn(mockTransport);
    when(mockTransportManager.createInterimTransport()).thenReturn(mockInterimTransport);
    when(mockInterimTransport.transport()).thenReturn(mockInterimTransportAsTransport);
  }

  @Test
  public void pickBeforeResolved() throws Exception {
    Transport t1 = loadBalancer.pickTransport(null);
    Transport t2 = loadBalancer.pickTransport(null);
    assertSame(mockInterimTransportAsTransport, t1);
    assertSame(mockInterimTransportAsTransport, t2);
    verify(mockTransportManager).createInterimTransport();
    verify(mockTransportManager, never()).getTransport(any(EquivalentAddressGroup.class));
    verify(mockInterimTransport, times(2)).transport();

    loadBalancer.handleResolvedAddresses(servers, Attributes.EMPTY);
    verify(mockInterimTransport).closeWithRealTransports(transportSupplierCaptor.capture());
    for (int i = 0; i < 2; i++) {
      assertSame(mockTransport, transportSupplierCaptor.getValue().get());
    }
    verify(mockTransportManager, times(2)).getTransport(eq(addressGroup));
    verifyNoMoreInteractions(mockTransportManager);
    verifyNoMoreInteractions(mockInterimTransport);
  }

  @Test
  public void pickAfterResolved() throws Exception {
    loadBalancer.handleResolvedAddresses(servers, Attributes.EMPTY);
    Transport t = loadBalancer.pickTransport(null);
    assertSame(mockTransport, t);
    verify(mockTransportManager).getTransport(addressGroup);
    verifyNoMoreInteractions(mockTransportManager);
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

  private static class Transport {}
}
