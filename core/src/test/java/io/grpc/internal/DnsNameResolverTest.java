/*
 * Copyright 2015 The gRPC Authors
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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.base.Stopwatch;
import com.google.common.collect.Iterables;
import com.google.common.net.InetAddresses;
import com.google.common.testing.FakeTicker;
import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.NameResolver;
import io.grpc.internal.DnsNameResolver.AddressResolver;
import io.grpc.internal.DnsNameResolver.ResolutionResults;
import io.grpc.internal.DnsNameResolver.ResourceResolver;
import io.grpc.internal.DnsNameResolver.ResourceResolverFactory;
import io.grpc.internal.SharedResourceHolder.Resource;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.net.URI;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Random;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.DisableOnDebug;
import org.junit.rules.ExpectedException;
import org.junit.rules.TestRule;
import org.junit.rules.Timeout;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/** Unit tests for {@link DnsNameResolver}. */
@RunWith(JUnit4.class)
public class DnsNameResolverTest {

  @Rule public final TestRule globalTimeout = new DisableOnDebug(Timeout.seconds(10));

  @Rule
  public final ExpectedException thrown = ExpectedException.none();

  private final Map<String, Object> serviceConfig = new LinkedHashMap<String, Object>();

  private static final int DEFAULT_PORT = 887;
  private static final Attributes NAME_RESOLVER_PARAMS =
      Attributes.newBuilder().set(NameResolver.Factory.PARAMS_DEFAULT_PORT, DEFAULT_PORT).build();

  private final DnsNameResolverProvider provider = new DnsNameResolverProvider();
  private final FakeClock fakeClock = new FakeClock();
  private final FakeClock fakeExecutor = new FakeClock();

  private final Resource<ExecutorService> fakeExecutorResource =
      new Resource<ExecutorService>() {
        @Override
        public ExecutorService create() {
          return fakeExecutor.getScheduledExecutorService();
        }

        @Override
        public void close(ExecutorService instance) {
        }
      };

  @Mock
  private NameResolver.Listener mockListener;
  @Captor
  private ArgumentCaptor<List<EquivalentAddressGroup>> resultCaptor;
  @Nullable
  private String networkaddressCacheTtlPropertyValue;

  private DnsNameResolver newResolver(String name, int port) {
    return newResolver(name, port, GrpcUtil.NOOP_PROXY_DETECTOR, Stopwatch.createUnstarted());
  }

  private DnsNameResolver newResolver(
      String name,
      int port,
      ProxyDetector proxyDetector,
      Stopwatch stopwatch) {
    DnsNameResolver dnsResolver = new DnsNameResolver(
        null,
        name,
        Attributes.newBuilder().set(NameResolver.Factory.PARAMS_DEFAULT_PORT, port).build(),
        fakeExecutorResource,
        proxyDetector,
        stopwatch);
    return dnsResolver;
  }

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    DnsNameResolver.enableJndi = true;
    networkaddressCacheTtlPropertyValue =
        System.getProperty(DnsNameResolver.NETWORKADDRESS_CACHE_TTL_PROPERTY);
  }

  @After
  public void restoreSystemProperty() {
    if (networkaddressCacheTtlPropertyValue == null) {
      System.clearProperty(DnsNameResolver.NETWORKADDRESS_CACHE_TTL_PROPERTY);
    } else {
      System.setProperty(
          DnsNameResolver.NETWORKADDRESS_CACHE_TTL_PROPERTY,
          networkaddressCacheTtlPropertyValue);
    }
  }

  @After
  public void noMorePendingTasks() {
    assertEquals(0, fakeClock.numPendingTasks());
    assertEquals(0, fakeExecutor.numPendingTasks());
  }

  @Test
  public void invalidDnsName() throws Exception {
    testInvalidUri(new URI("dns", null, "/[invalid]", null));
  }

  @Test
  public void validIpv6() throws Exception {
    testValidUri(new URI("dns", null, "/[::1]", null), "[::1]", DEFAULT_PORT);
  }

  @Test
  public void validDnsNameWithoutPort() throws Exception {
    testValidUri(new URI("dns", null, "/foo.googleapis.com", null),
        "foo.googleapis.com", DEFAULT_PORT);
  }

  @Test
  public void validDnsNameWithPort() throws Exception {
    testValidUri(new URI("dns", null, "/foo.googleapis.com:456", null),
        "foo.googleapis.com:456", 456);
  }

  @Test
  public void nullDnsName() {
    try {
      newResolver(null, DEFAULT_PORT);
      fail("Expected NullPointerException");
    } catch (NullPointerException e) {
      // expected
    }
  }

  @Test
  public void invalidDnsName_containsUnderscore() {
    try {
      newResolver("host_1", DEFAULT_PORT);
      fail("Expected IllegalArgumentException");
    } catch (IllegalArgumentException e) {
      // expected
    }
  }

  @Test
  public void resolve_neverCache() throws Exception {
    System.setProperty(DnsNameResolver.NETWORKADDRESS_CACHE_TTL_PROPERTY, "0");
    final List<InetAddress> answer1 = createAddressList(2);
    final List<InetAddress> answer2 = createAddressList(1);
    String name = "foo.googleapis.com";

    DnsNameResolver resolver = newResolver(name, 81);
    AddressResolver mockResolver = mock(AddressResolver.class);
    when(mockResolver.resolveAddress(Matchers.anyString())).thenReturn(answer1).thenReturn(answer2);
    resolver.setAddressResolver(mockResolver);

    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertAnswerMatches(answer1, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    resolver.refresh();
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener, times(2)).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertAnswerMatches(answer2, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    resolver.shutdown();

    verify(mockResolver, times(2)).resolveAddress(Matchers.anyString());
  }

  @Test
  public void resolve_cacheForever() throws Exception {
    System.setProperty(DnsNameResolver.NETWORKADDRESS_CACHE_TTL_PROPERTY, "-1");
    final List<InetAddress> answer1 = createAddressList(2);
    String name = "foo.googleapis.com";
    FakeTicker fakeTicker = new FakeTicker();

    DnsNameResolver resolver =
        newResolver(name, 81, GrpcUtil.NOOP_PROXY_DETECTOR, Stopwatch.createUnstarted(fakeTicker));
    AddressResolver mockResolver = mock(AddressResolver.class);
    when(mockResolver.resolveAddress(Matchers.anyString()))
        .thenReturn(answer1)
        .thenThrow(new AssertionError("should not called twice"));
    resolver.setAddressResolver(mockResolver);

    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertAnswerMatches(answer1, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    fakeTicker.advance(1, TimeUnit.DAYS);
    resolver.refresh();
    assertEquals(1, fakeExecutor.runDueTasks());
    verifyNoMoreInteractions(mockListener);
    assertAnswerMatches(answer1, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    resolver.shutdown();

    verify(mockResolver).resolveAddress(Matchers.anyString());
  }

  @Test
  public void resolve_usingCache() throws Exception {
    long ttl = 60;
    System.setProperty(DnsNameResolver.NETWORKADDRESS_CACHE_TTL_PROPERTY, Long.toString(ttl));
    final List<InetAddress> answer = createAddressList(2);
    String name = "foo.googleapis.com";
    FakeTicker fakeTicker = new FakeTicker();

    DnsNameResolver resolver =
        newResolver(name, 81, GrpcUtil.NOOP_PROXY_DETECTOR, Stopwatch.createUnstarted(fakeTicker));
    AddressResolver mockResolver = mock(AddressResolver.class);
    when(mockResolver.resolveAddress(Matchers.anyString()))
        .thenReturn(answer)
        .thenThrow(new AssertionError("should not reach here."));
    resolver.setAddressResolver(mockResolver);

    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertAnswerMatches(answer, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    // this refresh should return cached result
    fakeTicker.advance(ttl - 1, TimeUnit.SECONDS);
    resolver.refresh();
    assertEquals(1, fakeExecutor.runDueTasks());
    verifyNoMoreInteractions(mockListener);
    assertAnswerMatches(answer, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    resolver.shutdown();

    verify(mockResolver).resolveAddress(Matchers.anyString());
  }

  @Test
  public void resolve_cacheExpired() throws Exception {
    long ttl = 60;
    System.setProperty(DnsNameResolver.NETWORKADDRESS_CACHE_TTL_PROPERTY, Long.toString(ttl));
    final List<InetAddress> answer1 = createAddressList(2);
    final List<InetAddress> answer2 = createAddressList(1);
    String name = "foo.googleapis.com";
    FakeTicker fakeTicker = new FakeTicker();

    DnsNameResolver resolver =
        newResolver(name, 81, GrpcUtil.NOOP_PROXY_DETECTOR, Stopwatch.createUnstarted(fakeTicker));
    AddressResolver mockResolver = mock(AddressResolver.class);
    when(mockResolver.resolveAddress(Matchers.anyString())).thenReturn(answer1).thenReturn(answer2);
    resolver.setAddressResolver(mockResolver);

    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertAnswerMatches(answer1, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    fakeTicker.advance(ttl + 1, TimeUnit.SECONDS);
    resolver.refresh();
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener, times(2)).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertAnswerMatches(answer2, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    resolver.shutdown();

    verify(mockResolver, times(2)).resolveAddress(Matchers.anyString());
  }

  @Test
  public void resolve_invalidTtlPropertyValue() throws Exception {
    System.setProperty(DnsNameResolver.NETWORKADDRESS_CACHE_TTL_PROPERTY, "not_a_number");
    resolveDefaultValue();
  }

  @Test
  public void resolve_noPropertyValue() throws Exception {
    System.clearProperty(DnsNameResolver.NETWORKADDRESS_CACHE_TTL_PROPERTY);
    resolveDefaultValue();
  }

  private void resolveDefaultValue() throws Exception {
    final List<InetAddress> answer1 = createAddressList(2);
    final List<InetAddress> answer2 = createAddressList(1);
    String name = "foo.googleapis.com";
    FakeTicker fakeTicker = new FakeTicker();

    DnsNameResolver resolver =
        newResolver(name, 81, GrpcUtil.NOOP_PROXY_DETECTOR, Stopwatch.createUnstarted(fakeTicker));
    AddressResolver mockResolver = mock(AddressResolver.class);
    when(mockResolver.resolveAddress(Matchers.anyString())).thenReturn(answer1).thenReturn(answer2);
    resolver.setAddressResolver(mockResolver);

    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertAnswerMatches(answer1, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    fakeTicker.advance(DnsNameResolver.DEFAULT_NETWORK_CACHE_TTL_SECONDS, TimeUnit.SECONDS);
    resolver.refresh();
    assertEquals(1, fakeExecutor.runDueTasks());
    verifyNoMoreInteractions(mockListener);
    assertAnswerMatches(answer1, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    fakeTicker.advance(1, TimeUnit.SECONDS);
    resolver.refresh();
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener, times(2)).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertAnswerMatches(answer2, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    resolver.shutdown();

    verify(mockResolver, times(2)).resolveAddress(Matchers.anyString());
  }

  @Test
  public void resolveAll_nullResourceResolver() throws Exception {
    final String hostname = "addr.fake";
    final Inet4Address backendAddr = InetAddresses.fromInteger(0x7f000001);

    AddressResolver mockResolver = mock(AddressResolver.class);
    when(mockResolver.resolveAddress(Matchers.anyString()))
        .thenReturn(Collections.<InetAddress>singletonList(backendAddr));
    ResourceResolver resourceResolver = null;
    boolean resovleSrv = true;
    boolean resolveTxt = true;

    ResolutionResults res = DnsNameResolver.resolveAll(
        mockResolver, resourceResolver, resovleSrv, resolveTxt, hostname);
    assertThat(res.addresses).containsExactly(backendAddr);
    assertThat(res.balancerAddresses).isEmpty();
    assertThat(res.txtRecords).isEmpty();
    verify(mockResolver).resolveAddress(hostname);
  }

  @Test
  public void resolveAll_presentResourceResolver() throws Exception {
    final String hostname = "addr.fake";
    final Inet4Address backendAddr = InetAddresses.fromInteger(0x7f000001);
    final EquivalentAddressGroup balancerAddr = new EquivalentAddressGroup(new SocketAddress() {});

    AddressResolver mockAddressResolver = mock(AddressResolver.class);
    when(mockAddressResolver.resolveAddress(Matchers.anyString()))
        .thenReturn(Collections.<InetAddress>singletonList(backendAddr));
    ResourceResolver mockResourceResolver = mock(ResourceResolver.class);
    when(mockResourceResolver.resolveTxt(Matchers.anyString()))
        .thenReturn(Collections.singletonList("service config"));
    when(mockResourceResolver.resolveSrv(Matchers.any(AddressResolver.class), Matchers.anyString()))
        .thenReturn(Collections.singletonList(balancerAddr));
    boolean resovleSrv = true;
    boolean resolveTxt = true;

    ResolutionResults res = DnsNameResolver.resolveAll(
        mockAddressResolver, mockResourceResolver, resovleSrv, resolveTxt, hostname);
    assertThat(res.addresses).containsExactly(backendAddr);
    assertThat(res.balancerAddresses).containsExactly(balancerAddr);
    assertThat(res.txtRecords).containsExactly("service config");
    verify(mockAddressResolver).resolveAddress(hostname);
    verify(mockResourceResolver).resolveTxt("_grpc_config." + hostname);
    verify(mockResourceResolver).resolveSrv(mockAddressResolver, "_grpclb._tcp." + hostname);
  }

  @Test
  public void resolveAll_onlyBalancers() throws Exception {
    String hostname = "addr.fake";
    EquivalentAddressGroup balancerAddr = new EquivalentAddressGroup(new SocketAddress() {});

    AddressResolver mockAddressResolver = mock(AddressResolver.class);
    when(mockAddressResolver.resolveAddress(Matchers.anyString()))
        .thenThrow(new UnknownHostException("I really tried"));
    ResourceResolver mockResourceResolver = mock(ResourceResolver.class);
    when(mockResourceResolver.resolveTxt(Matchers.anyString()))
        .thenReturn(Collections.<String>emptyList());
    when(mockResourceResolver.resolveSrv(Matchers.any(AddressResolver.class), Matchers.anyString()))
        .thenReturn(Collections.singletonList(balancerAddr));
    boolean resovleSrv = true;
    boolean resolveTxt = true;

    ResolutionResults res = DnsNameResolver.resolveAll(
        mockAddressResolver, mockResourceResolver, resovleSrv, resolveTxt, hostname);
    assertThat(res.addresses).isEmpty();
    assertThat(res.balancerAddresses).containsExactly(balancerAddr);
    assertThat(res.txtRecords).isEmpty();
    verify(mockAddressResolver).resolveAddress(hostname);
    verify(mockResourceResolver).resolveTxt("_grpc_config." + hostname);
    verify(mockResourceResolver).resolveSrv(mockAddressResolver, "_grpclb._tcp." + hostname);
  }

  @Test
  public void resolveAll_balancerLookupFails() throws Exception {
    final String hostname = "addr.fake";
    final Inet4Address backendAddr = InetAddresses.fromInteger(0x7f000001);
    AddressResolver mockAddressResolver = mock(AddressResolver.class);
    when(mockAddressResolver.resolveAddress(Matchers.anyString()))
        .thenReturn(Collections.<InetAddress>singletonList(backendAddr));
    ResourceResolver mockResourceResolver = mock(ResourceResolver.class);
    when(mockResourceResolver.resolveTxt(Matchers.anyString()))
        .thenReturn(Collections.singletonList("service config"));
    when(mockResourceResolver.resolveSrv(Matchers.any(AddressResolver.class), Matchers.anyString()))
        .thenThrow(new Exception("something like javax.naming.NamingException"));
    boolean resovleSrv = true;
    boolean resolveTxt = true;

    ResolutionResults res = DnsNameResolver.resolveAll(
        mockAddressResolver, mockResourceResolver, resovleSrv, resolveTxt, hostname);
    assertThat(res.addresses).containsExactly(backendAddr);
    assertThat(res.balancerAddresses).isEmpty();
    assertThat(res.txtRecords).containsExactly("service config");
    verify(mockAddressResolver).resolveAddress(hostname);
    verify(mockResourceResolver).resolveTxt("_grpc_config." + hostname);
    verify(mockResourceResolver).resolveSrv(mockAddressResolver, "_grpclb._tcp." + hostname);
  }

  @Test
  public void skipMissingJndiResolverResolver() throws Exception {
    ClassLoader cl = new ClassLoader() {
      @Override
      protected Class<?> loadClass(String name, boolean resolve) throws ClassNotFoundException {
        if ("io.grpc.internal.JndiResourceResolverFactory".equals(name)) {
          throw new ClassNotFoundException();
        }
        return super.loadClass(name, resolve);
      }
    };

    ResourceResolverFactory factory = DnsNameResolver.getResourceResolverFactory(cl);

    assertThat(factory).isNull();
  }

  @Test
  public void doNotResolveWhenProxyDetected() throws Exception {
    final String name = "foo.googleapis.com";
    final int port = 81;
    ProxyDetector alwaysDetectProxy = mock(ProxyDetector.class);
    ProxyParameters proxyParameters = new ProxyParameters(
        new InetSocketAddress(InetAddress.getByName("10.0.0.1"), 1000),
        "username",
        "password");
    when(alwaysDetectProxy.proxyFor(any(SocketAddress.class)))
        .thenReturn(proxyParameters);
    DnsNameResolver resolver =
        newResolver(name, port, alwaysDetectProxy, Stopwatch.createUnstarted());
    AddressResolver mockAddressResolver = mock(AddressResolver.class);
    when(mockAddressResolver.resolveAddress(Matchers.anyString())).thenThrow(new AssertionError());
    resolver.setAddressResolver(mockAddressResolver);
    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());

    verify(mockListener).onAddresses(resultCaptor.capture(), any(Attributes.class));
    List<EquivalentAddressGroup> result = resultCaptor.getValue();
    assertThat(result).hasSize(1);
    EquivalentAddressGroup eag = result.get(0);
    assertThat(eag.getAddresses()).hasSize(1);

    ProxySocketAddress socketAddress = (ProxySocketAddress) eag.getAddresses().get(0);
    assertSame(proxyParameters, socketAddress.getProxyParameters());
    assertTrue(((InetSocketAddress) socketAddress.getAddress()).isUnresolved());
  }

  @Test
  public void maybeChooseServiceConfig_failsOnMisspelling() {
    Map<String, Object> bad = new LinkedHashMap<String, Object>();
    bad.put("parcentage", 1.0);
    thrown.expectMessage("Bad key");

    DnsNameResolver.maybeChooseServiceConfig(bad, new Random(), "host");
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageMatchesJava() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<>();
    langs.add("java");
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageDoesntMatchGo() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<>();
    langs.add("go");
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageCaseInsensitive() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<>();
    langs.add("JAVA");
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageMatchesEmtpy() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<>();
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageMatchesMulti() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<>();
    langs.add("go");
    langs.add("java");
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageZeroAlwaysFails() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 0D);
    choice.put("serviceConfig", serviceConfig);

    assertNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageHundredAlwaysSucceeds() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 100D);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAboveMatches50() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 50D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 49;
      }
    };

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAtFails50() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 50D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 50;
      }
    };

    assertNull(DnsNameResolver.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAboveMatches99() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 99D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 98;
      }
    };

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAtFails99() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 99D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 99;
      }
    };

    assertNull(DnsNameResolver.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAboveMatches1() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 1D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 0;
      }
    };

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAtFails1() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 1D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 1;
      }
    };

    assertNull(DnsNameResolver.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_hostnameMatches() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<>();
    hosts.add("localhost");
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "localhost"));
  }

  @Test
  public void maybeChooseServiceConfig_hostnameDoesntMatch() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<>();
    hosts.add("localhorse");
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "localhost"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageCaseSensitive() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<>();
    hosts.add("LOCALHOST");
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "localhost"));
  }

  @Test
  public void maybeChooseServiceConfig_hostnameMatchesEmtpy() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<>();
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_hostnameMatchesMulti() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<>();
    hosts.add("localhorse");
    hosts.add("localhost");
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(DnsNameResolver.maybeChooseServiceConfig(choice, new Random(), "localhost"));
  }

  private void testInvalidUri(URI uri) {
    try {
      provider.newNameResolver(uri, NAME_RESOLVER_PARAMS);
      fail("Should have failed");
    } catch (IllegalArgumentException e) {
      // expected
    }
  }

  private void testValidUri(URI uri, String exportedAuthority, int expectedPort) {
    DnsNameResolver resolver = provider.newNameResolver(uri, NAME_RESOLVER_PARAMS);
    assertNotNull(resolver);
    assertEquals(expectedPort, resolver.getPort());
    assertEquals(exportedAuthority, resolver.getServiceAuthority());
  }

  private byte lastByte = 0;

  private List<InetAddress> createAddressList(int n) throws UnknownHostException {
    List<InetAddress> list = new ArrayList<>(n);
    for (int i = 0; i < n; i++) {
      list.add(InetAddress.getByAddress(new byte[] {127, 0, 0, ++lastByte}));
    }
    return list;
  }

  private static void assertAnswerMatches(
      List<InetAddress> addrs, int port, List<EquivalentAddressGroup> results) {
    assertEquals(addrs.size(), results.size());
    for (int i = 0; i < addrs.size(); i++) {
      EquivalentAddressGroup addrGroup = results.get(i);
      InetSocketAddress socketAddr =
          (InetSocketAddress) Iterables.getOnlyElement(addrGroup.getAddresses());
      assertEquals("Addr " + i, port, socketAddr.getPort());
      assertEquals("Addr " + i, addrs.get(i), socketAddr.getAddress());
    }
  }
}
