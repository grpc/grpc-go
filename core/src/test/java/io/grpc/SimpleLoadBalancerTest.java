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

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertSame;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;

import io.grpc.internal.ClientTransport;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.net.SocketAddress;
import java.util.ArrayList;

/** Unit test for {@link SimpleLoadBalancerFactory}. */
@RunWith(JUnit4.class)
public class SimpleLoadBalancerTest {
  private LoadBalancer loadBalancer;

  private ArrayList<ResolvedServerInfo> servers;
  private EquivalentAddressGroup addressGroup;

  @Mock
  private TransportManager mockTransportManager;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    loadBalancer = SimpleLoadBalancerFactory.getInstance().newLoadBalancer(
        "fakeservice", mockTransportManager);
    servers = new ArrayList<ResolvedServerInfo>();
    ArrayList<SocketAddress> addresses = new ArrayList<SocketAddress>();
    for (int i = 0; i < 3; i++) {
      SocketAddress addr = new FakeSocketAddress("server" + i);
      servers.add(new ResolvedServerInfo(addr, Attributes.EMPTY));
      addresses.add(addr);
    }
    addressGroup = new EquivalentAddressGroup(addresses);
  }

  @Test
  public void pickBeforeResolved() throws Exception {
    ClientTransport mockTransport = mock(ClientTransport.class);
    SettableFuture<ClientTransport> sourceFuture = SettableFuture.create();
    when(mockTransportManager.getTransport(eq(addressGroup)))
        .thenReturn(sourceFuture);
    ListenableFuture<ClientTransport> f1 = loadBalancer.pickTransport(null);
    ListenableFuture<ClientTransport> f2 = loadBalancer.pickTransport(null);
    assertNotNull(f1);
    assertNotNull(f2);
    assertNotSame(f1, f2);
    assertFalse(f1.isDone());
    assertFalse(f2.isDone());
    verify(mockTransportManager, never()).getTransport(any(EquivalentAddressGroup.class));
    loadBalancer.handleResolvedAddresses(servers, Attributes.EMPTY);
    verify(mockTransportManager, times(2)).getTransport(eq(addressGroup));
    assertFalse(f1.isDone());
    assertFalse(f2.isDone());
    assertNotSame(sourceFuture, f1);
    assertNotSame(sourceFuture, f2);
    sourceFuture.set(mockTransport);
    assertSame(mockTransport, f1.get());
    assertSame(mockTransport, f2.get());
    verifyNoMoreInteractions(mockTransportManager);
  }

  @Test
  public void pickAfterResolved() throws Exception {
    ClientTransport mockTransport = mock(ClientTransport.class);
    SettableFuture<ClientTransport> sourceFuture = SettableFuture.create();
    when(mockTransportManager.getTransport(eq(addressGroup)))
        .thenReturn(sourceFuture);
    loadBalancer.handleResolvedAddresses(servers, Attributes.EMPTY);
    ListenableFuture<ClientTransport> f = loadBalancer.pickTransport(null);
    assertSame(sourceFuture, f);
    assertFalse(f.isDone());
    sourceFuture.set(mockTransport);
    assertSame(mockTransport, f.get());
    verify(mockTransportManager).getTransport(addressGroup);
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
