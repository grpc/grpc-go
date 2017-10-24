/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

package io.grpc.internal;

import static junit.framework.TestCase.assertFalse;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import com.google.common.base.Supplier;
import com.google.common.collect.ImmutableList;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.PasswordAuthentication;
import java.net.Proxy;
import java.net.ProxySelector;
import java.net.SocketAddress;
import java.net.URI;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

@RunWith(JUnit4.class)
public class ProxyDetectorImplTest {
  private InetSocketAddress destination = InetSocketAddress.createUnresolved(
      "destination",
      5678
  );

  @Mock private ProxySelector proxySelector;
  @Mock private ProxyDetectorImpl.AuthenticationProvider authenticator;
  private Supplier<ProxySelector> proxySelectorSupplier;
  private ProxyDetector proxyDetector;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    proxySelectorSupplier = new Supplier<ProxySelector>() {
      @Override
      public ProxySelector get() {
        return proxySelector;
      }
    };
    proxyDetector = new ProxyDetectorImpl(proxySelectorSupplier, authenticator, null);
  }

  @Test
  public void override_hostPort() throws Exception {
    final String overrideHost = "override";
    final int overridePort = 1234;
    final String overrideHostWithPort = overrideHost + ":" + overridePort;
    ProxyDetectorImpl proxyDetector = new ProxyDetectorImpl(
        proxySelectorSupplier,
        authenticator,
        overrideHostWithPort);
    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(
        new ProxyParameters(
            InetSocketAddress.createUnresolved(overrideHost, overridePort), null, null),
        detected);
  }

  @Test
  public void override_hostOnly() throws Exception {
    final String overrideHostWithoutPort = "override";
    final int defaultPort = 80;
    ProxyDetectorImpl proxyDetector = new ProxyDetectorImpl(
        proxySelectorSupplier,
        authenticator,
        overrideHostWithoutPort);
    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(
        new ProxyParameters(
            InetSocketAddress.createUnresolved(overrideHostWithoutPort, defaultPort), null, null),
        detected);
  }

  @Test
  public void returnNullWhenNoProxy() throws Exception {
    when(proxySelector.select(any(URI.class)))
        .thenReturn(ImmutableList.of(java.net.Proxy.NO_PROXY));
    assertNull(proxyDetector.proxyFor(destination));
  }

  @Test
  public void detectProxyForUnresolved() throws Exception {
    final InetSocketAddress proxyAddress = InetSocketAddress.createUnresolved("proxy", 1234);
    Proxy proxy = new Proxy(Proxy.Type.HTTP, proxyAddress);
    when(proxySelector.select(any(URI.class))).thenReturn(ImmutableList.of(proxy));

    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(new ProxyParameters(proxyAddress, null, null), detected);
  }

  @Test
  public void detectProxyForResolved() throws Exception {
    InetSocketAddress resolved =
        new InetSocketAddress(InetAddress.getByAddress(new byte[]{10, 0, 0, 1}), 10);
    assertFalse(resolved.isUnresolved());
    destination = resolved;

    final InetSocketAddress proxyAddress = InetSocketAddress.createUnresolved("proxy", 1234);
    Proxy proxy = new Proxy(Proxy.Type.HTTP, proxyAddress);
    when(proxySelector.select(any(URI.class))).thenReturn(ImmutableList.of(proxy));

    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(new ProxyParameters(proxyAddress, null, null), detected);
  }

  @Test
  public void pickFirstHttpProxy() throws Exception {
    final InetSocketAddress proxyAddress = InetSocketAddress.createUnresolved("proxy1", 1111);
    InetSocketAddress otherProxy = InetSocketAddress.createUnresolved("proxy2", 2222);
    Proxy proxy1 = new java.net.Proxy(java.net.Proxy.Type.HTTP, proxyAddress);
    Proxy proxy2 = new java.net.Proxy(java.net.Proxy.Type.HTTP, otherProxy);
    when(proxySelector.select(any(URI.class))).thenReturn(ImmutableList.of(proxy1, proxy2));

    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(new ProxyParameters(proxyAddress, null, null), detected);
  }

  // Mainly for InProcessSocketAddress
  @Test
  public void noProxyForNonInetSocket() throws Exception {
    assertNull(proxyDetector.proxyFor(mock(SocketAddress.class)));
  }

  @Test
  public void authRequired() throws Exception {
    final String proxyHost = "proxyhost";
    final int proxyPort = 1234;
    final InetSocketAddress proxyAddress = InetSocketAddress.createUnresolved(proxyHost, proxyPort);
    Proxy proxy = new java.net.Proxy(java.net.Proxy.Type.HTTP, proxyAddress);
    final String proxyUser = "testuser";
    final String proxyPassword = "testpassword";
    PasswordAuthentication auth = new PasswordAuthentication(
        proxyUser,
        proxyPassword.toCharArray());
    when(authenticator.requestPasswordAuthentication(
        any(String.class),
        any(InetAddress.class),
        any(Integer.class),
        any(String.class),
        any(String.class),
        any(String.class))).thenReturn(auth);
    when(proxySelector.select(any(URI.class))).thenReturn(ImmutableList.of(proxy));

    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(new ProxyParameters(proxyAddress, proxyUser, proxyPassword), detected);
  }
}
