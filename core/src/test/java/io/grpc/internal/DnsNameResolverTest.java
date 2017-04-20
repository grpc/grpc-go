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

package io.grpc.internal;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.base.MoreObjects;
import com.google.common.collect.Iterables;
import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.NameResolver;
import io.grpc.Status;
import io.grpc.internal.DnsNameResolver.DelegateResolver;
import io.grpc.internal.DnsNameResolver.ResolutionResults;
import io.grpc.internal.SharedResourceHolder.Resource;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.URI;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedList;
import java.util.List;
import java.util.Queue;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.Assume;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/** Unit tests for {@link DnsNameResolver}. */
@RunWith(JUnit4.class)
public class DnsNameResolverTest {
  private static final int DEFAULT_PORT = 887;
  private static final Attributes NAME_RESOLVER_PARAMS =
      Attributes.newBuilder().set(NameResolver.Factory.PARAMS_DEFAULT_PORT, DEFAULT_PORT).build();

  private final DnsNameResolverProvider provider = new DnsNameResolverProvider();
  private final FakeClock fakeClock = new FakeClock();
  private final FakeClock fakeExecutor = new FakeClock();
  private MockResolver mockResolver = new MockResolver();
  private final Resource<ScheduledExecutorService> fakeTimerServiceResource =
      new Resource<ScheduledExecutorService>() {
        @Override
        public ScheduledExecutorService create() {
          return fakeClock.getScheduledExecutorService();
        }

        @Override
        public void close(ScheduledExecutorService instance) {
          assertSame(fakeClock, instance);
        }
      };

  private final Resource<ExecutorService> fakeExecutorResource =
      new Resource<ExecutorService>() {
        @Override
        public ExecutorService create() {
          return fakeExecutor.getScheduledExecutorService();
        }

        @Override
        public void close(ExecutorService instance) {
          assertSame(fakeExecutor, instance);
        }
      };

  @Mock
  private NameResolver.Listener mockListener;
  @Captor
  private ArgumentCaptor<List<EquivalentAddressGroup>> resultCaptor;
  @Captor
  private ArgumentCaptor<Status> statusCaptor;

  private DnsNameResolver newResolver(String name, int port) {
    DnsNameResolver dnsResolver = new DnsNameResolver(
        null,
        name,
        Attributes.newBuilder().set(NameResolver.Factory.PARAMS_DEFAULT_PORT, port).build(),
        fakeTimerServiceResource,
        fakeExecutorResource);
    dnsResolver.setDelegateResolver(mockResolver);
    return dnsResolver;
  }

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    DnsNameResolver.enableJndi = true;
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
  public void resolve() throws Exception {
    List<InetAddress> answer1 = createAddressList(2);
    List<InetAddress> answer2 = createAddressList(1);
    String name = "foo.googleapis.com";

    DnsNameResolver resolver = newResolver(name, 81);
    mockResolver.addAnswer(answer1).addAnswer(answer2);
    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertEquals(name, mockResolver.invocations.poll());
    assertAnswerMatches(answer1, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    resolver.refresh();
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener, times(2)).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertEquals(name, mockResolver.invocations.poll());
    assertAnswerMatches(answer2, 81, resultCaptor.getValue());
    assertEquals(0, fakeClock.numPendingTasks());

    resolver.shutdown();
  }

  @Test
  public void retry() throws Exception {
    String name = "foo.googleapis.com";
    UnknownHostException error = new UnknownHostException(name);
    List<InetAddress> answer = createAddressList(2);
    DnsNameResolver resolver = newResolver(name, 81);
    mockResolver.addAnswer(error).addAnswer(error).addAnswer(answer);
    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener).onError(statusCaptor.capture());
    assertEquals(name, mockResolver.invocations.poll());
    Status status = statusCaptor.getValue();
    assertEquals(Status.Code.UNAVAILABLE, status.getCode());
    assertSame(error, status.getCause());

    // First retry scheduled
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardNanos(TimeUnit.MINUTES.toNanos(1) - 1);
    assertEquals(1, fakeClock.numPendingTasks());

    // First retry
    fakeClock.forwardNanos(1);
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener, times(2)).onError(statusCaptor.capture());
    assertEquals(name, mockResolver.invocations.poll());
    status = statusCaptor.getValue();
    assertEquals(Status.Code.UNAVAILABLE, status.getCode());
    assertSame(error, status.getCause());

    // Second retry scheduled
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardNanos(TimeUnit.MINUTES.toNanos(1) - 1);
    assertEquals(1, fakeClock.numPendingTasks());

    // Second retry
    fakeClock.forwardNanos(1);
    assertEquals(0, fakeClock.numPendingTasks());
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertEquals(name, mockResolver.invocations.poll());
    assertAnswerMatches(answer, 81, resultCaptor.getValue());

    verifyNoMoreInteractions(mockListener);
  }

  @Test
  public void refreshCancelsScheduledRetry() throws Exception {
    String name = "foo.googleapis.com";
    UnknownHostException error = new UnknownHostException(name);
    List<InetAddress> answer = createAddressList(2);
    DnsNameResolver resolver = newResolver(name,  81);
    mockResolver.addAnswer(error).addAnswer(answer);
    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockListener).onError(statusCaptor.capture());
    assertEquals(name, mockResolver.invocations.poll());
    Status status = statusCaptor.getValue();
    assertEquals(Status.Code.UNAVAILABLE, status.getCode());
    assertSame(error, status.getCause());

    // First retry scheduled
    assertEquals(1, fakeClock.numPendingTasks());

    resolver.refresh();
    assertEquals(1, fakeExecutor.runDueTasks());
    // Refresh cancelled the retry
    assertEquals(0, fakeClock.numPendingTasks());
    verify(mockListener).onAddresses(resultCaptor.capture(), any(Attributes.class));
    assertEquals(name, mockResolver.invocations.poll());
    assertAnswerMatches(answer, 81, resultCaptor.getValue());

    verifyNoMoreInteractions(mockListener);
  }

  @Test
  public void shutdownCancelsScheduledRetry() throws Exception {
    String name = "foo.googleapis.com";
    UnknownHostException error = new UnknownHostException(name);
    DnsNameResolver resolver = newResolver(name, 81);
    mockResolver.addAnswer(error);
    resolver.start(mockListener);
    assertEquals(1, fakeExecutor.runDueTasks());

    verify(mockListener).onError(statusCaptor.capture());
    assertEquals(name, mockResolver.invocations.poll());
    Status status = statusCaptor.getValue();
    assertEquals(Status.Code.UNAVAILABLE, status.getCode());
    assertSame(error, status.getCause());

    // Retry scheduled
    assertEquals(1, fakeClock.numPendingTasks());

    // Shutdown cancelled the retry
    resolver.shutdown();
    assertEquals(0, fakeClock.numPendingTasks());

    verifyNoMoreInteractions(mockListener);
  }

  @Test(timeout = 10000)
  public void jdkResolverWorks() throws Exception {
    DnsNameResolver.DelegateResolver resolver = new DnsNameResolver.JdkResolver();

    ResolutionResults results = resolver.resolve("localhost");
    // Just check that *something* came back.
    assertThat(results.addresses).isNotEmpty();
    assertThat(results.txtRecords).isNotNull();
  }

  @Test(timeout = 10000)
  public void jndiResolverWorks() throws Exception {
    Assume.assumeTrue(DnsNameResolver.jndiAvailable());
    DnsNameResolver.DelegateResolver resolver = new DnsNameResolver.JndiResolver();
    ResolutionResults results = null;
    try {
      results = resolver.resolve("localhost");
    } catch (javax.naming.NameNotFoundException e) {
      Assume.assumeNoException(e);
    }

    assertThat(results.addresses).isEmpty();
    assertThat(results.txtRecords).isNotNull();
  }

  @Test
  public void compositeResolverPrefersJdkAddressJndiTxt() throws Exception {
    MockResolver jdkDelegate = new MockResolver();
    MockResolver jndiDelegate = new MockResolver();
    DelegateResolver resolver = new DnsNameResolver.CompositeResolver(jdkDelegate, jndiDelegate);

    List<InetAddress> jdkAnswer = createAddressList(2);
    jdkDelegate.addAnswer(jdkAnswer, Arrays.asList("jdktxt"));

    List<InetAddress> jdniAnswer = createAddressList(2);
    jndiDelegate.addAnswer(jdniAnswer, Arrays.asList("jnditxt"));

    ResolutionResults results = resolver.resolve("abc");

    assertThat(results.addresses).containsExactlyElementsIn(jdkAnswer).inOrder();
    assertThat(results.txtRecords).containsExactly("jnditxt");
  }

  @Test
  public void compositeResolverSkipsAbsentJndi() throws Exception {
    MockResolver jdkDelegate = new MockResolver();
    MockResolver jndiDelegate = null;
    DelegateResolver resolver = new DnsNameResolver.CompositeResolver(jdkDelegate, jndiDelegate);

    List<InetAddress> jdkAnswer = createAddressList(2);
    jdkDelegate.addAnswer(jdkAnswer);

    ResolutionResults results = resolver.resolve("abc");

    assertThat(results.addresses).containsExactlyElementsIn(jdkAnswer).inOrder();
    assertThat(results.txtRecords).isEmpty();
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
    List<InetAddress> list = new ArrayList<InetAddress>(n);
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

  private static class MockResolver extends DnsNameResolver.DelegateResolver {
    private final Queue<Object> answers = new LinkedList<Object>();
    private final Queue<String> invocations = new LinkedList<String>();

    MockResolver addAnswer(List<InetAddress> addresses) {
      return addAnswer(addresses, null);
    }

    MockResolver addAnswer(List<InetAddress> addresses, List<String> txtRecords) {
      answers.add(
          new ResolutionResults(
              addresses,
              MoreObjects.firstNonNull(txtRecords, Collections.<String>emptyList())));
      return this;
    }

    MockResolver addAnswer(UnknownHostException ex) {
      answers.add(ex);
      return this;
    }

    @SuppressWarnings("unchecked") // explosions acceptable.
    @Override
    ResolutionResults resolve(String host) throws Exception {
      invocations.add(host);
      Object answer = answers.poll();
      if (answer instanceof UnknownHostException) {
        throw (UnknownHostException) answer;
      }
      return (ResolutionResults) answer;
    }
  }
}
