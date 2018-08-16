/*
 * Copyright 2017 The gRPC Authors
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
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
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
  private static final String NO_USER = null;
  private static final String NO_PW = null;
  @Mock private ProxySelector proxySelector;
  @Mock private ProxyDetectorImpl.AuthenticationProvider authenticator;
  private InetSocketAddress destination = InetSocketAddress.createUnresolved("10.10.10.10", 5678);
  private Supplier<ProxySelector> proxySelectorSupplier;
  private ProxyDetector proxyDetector;
  private InetSocketAddress unresolvedProxy;
  private ProxyParameters proxyParmeters;

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    proxySelectorSupplier = new Supplier<ProxySelector>() {
      @Override
      public ProxySelector get() {
        return proxySelector;
      }
    };
    proxyDetector = new ProxyDetectorImpl(proxySelectorSupplier, authenticator, null);
    int proxyPort = 1234;
    unresolvedProxy = InetSocketAddress.createUnresolved("10.0.0.1", proxyPort);
    proxyParmeters = new ProxyParameters(
        new InetSocketAddress(InetAddress.getByName(unresolvedProxy.getHostName()), proxyPort),
        NO_USER,
        NO_PW);
  }

  @Test
  public void override_hostPort() throws Exception {
    final String overrideHost = "10.99.99.99";
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
            new InetSocketAddress(InetAddress.getByName(overrideHost), overridePort),
            NO_USER,
            NO_PW),
        detected);
  }

  @Test
  public void override_hostOnly() throws Exception {
    final String overrideHostWithoutPort = "10.99.99.99";
    final int defaultPort = 80;
    ProxyDetectorImpl proxyDetector = new ProxyDetectorImpl(
        proxySelectorSupplier,
        authenticator,
        overrideHostWithoutPort);
    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(
        new ProxyParameters(
            new InetSocketAddress(
                InetAddress.getByName(overrideHostWithoutPort), defaultPort),
            NO_USER,
            NO_PW),
        detected);
  }

  @Test
  public void returnNullWhenNoProxy() throws Exception {
    when(proxySelector.select(any(URI.class)))
        .thenReturn(ImmutableList.of(java.net.Proxy.NO_PROXY));
    assertNull(proxyDetector.proxyFor(destination));
  }

  @Test
  public void detectProxyForUnresolvedDestination() throws Exception {
    Proxy proxy = new Proxy(Proxy.Type.HTTP, unresolvedProxy);
    when(proxySelector.select(any(URI.class))).thenReturn(ImmutableList.of(proxy));

    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(proxyParmeters, detected);
  }

  @Test
  public void detectProxyForResolvedDestination() throws Exception {
    InetSocketAddress resolved = new InetSocketAddress(InetAddress.getByName("10.1.2.3"), 10);
    assertFalse(resolved.isUnresolved());
    destination = resolved;

    Proxy proxy = new Proxy(Proxy.Type.HTTP, unresolvedProxy);
    when(proxySelector.select(any(URI.class))).thenReturn(ImmutableList.of(proxy));

    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(proxyParmeters, detected);
  }

  @Test
  public void unresolvedProxyAddressBecomesResolved() throws Exception {
    InetSocketAddress unresolvedProxy = InetSocketAddress.createUnresolved("10.0.0.100", 1234);
    assertTrue(unresolvedProxy.isUnresolved());
    Proxy proxy1 = new java.net.Proxy(java.net.Proxy.Type.HTTP, unresolvedProxy);
    when(proxySelector.select(any(URI.class))).thenReturn(ImmutableList.of(proxy1));
    ProxyParameters proxy = proxyDetector.proxyFor(destination);
    assertFalse(proxy.proxyAddress.isUnresolved());
  }

  @Test
  public void pickFirstHttpProxy() throws Exception {
    InetSocketAddress otherProxy = InetSocketAddress.createUnresolved("10.0.0.2", 11111);
    assertNotEquals(unresolvedProxy, otherProxy);
    Proxy proxy1 = new java.net.Proxy(java.net.Proxy.Type.HTTP, unresolvedProxy);
    Proxy proxy2 = new java.net.Proxy(java.net.Proxy.Type.HTTP, otherProxy);
    when(proxySelector.select(any(URI.class))).thenReturn(ImmutableList.of(proxy1, proxy2));

    ProxyParameters detected = proxyDetector.proxyFor(destination);
    assertNotNull(detected);
    assertEquals(proxyParmeters, detected);
  }

  // Mainly for InProcessSocketAddress
  @Test
  public void noProxyForNonInetSocket() throws Exception {
    assertNull(proxyDetector.proxyFor(mock(SocketAddress.class)));
  }

  @Test
  public void authRequired() throws Exception {
    Proxy proxy = new java.net.Proxy(java.net.Proxy.Type.HTTP, unresolvedProxy);
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
    assertEquals(
        new ProxyParameters(
            new InetSocketAddress(
                InetAddress.getByName(unresolvedProxy.getHostName()),
                unresolvedProxy.getPort()),
            proxyUser,
            proxyPassword),
        detected);
  }

  @Test
  public void proxySelectorReturnsNull() throws Exception {
    ProxyDetectorImpl proxyDetector = new ProxyDetectorImpl(
        new Supplier<ProxySelector>() {
          @Override
          public ProxySelector get() {
            return null;
          }
        },
        authenticator,
        null);
    assertNull(proxyDetector.proxyFor(destination));
  }
}
