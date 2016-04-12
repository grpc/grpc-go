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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.any;
import static org.mockito.Mockito.same;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.base.Supplier;

import io.grpc.IntegerMarshaller;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.internal.ClientTransport;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.util.Arrays;
import java.util.Iterator;
import java.util.concurrent.Executor;

/**
 * Unit tests for {@link DelayedClientTransport}.
 */
@RunWith(JUnit4.class)
public class DelayedClientTransportTest {
  @Mock private ManagedClientTransport.Listener transportListener;
  @Mock private ClientTransport mockRealTransport;
  @Mock private ClientTransport mockRealTransport2;
  @Mock private ClientStream mockRealStream;
  @Mock private ClientStream mockRealStream2;
  @Mock private ClientStreamListener streamListener;
  @Mock private ClientTransport.PingCallback pingCallback;
  @Mock private Executor mockExecutor;
  @Captor private ArgumentCaptor<Status> statusCaptor;

  private final MethodDescriptor<String, Integer> method = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "/service/method",
      new StringMarshaller(), new IntegerMarshaller());

  private final MethodDescriptor<String, Integer> method2 = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "/service/method2",
      new StringMarshaller(), new IntegerMarshaller());

  private final Metadata headers = new Metadata();
  private final Metadata headers2 = new Metadata();

  private final FakeClock fakeExecutor = new FakeClock();
  private final DelayedClientTransport delayedTransport = new DelayedClientTransport(
      fakeExecutor.scheduledExecutorService);

  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(mockRealTransport.newStream(same(method), same(headers))).thenReturn(mockRealStream);
    when(mockRealTransport2.newStream(same(method2), same(headers2))).thenReturn(mockRealStream2);
    delayedTransport.start(transportListener);
  }

  @After public void noMorePendingTasks() {
    assertEquals(0, fakeExecutor.numPendingTasks());
  }

  @Test public void transportsAreUsedInOrder() {
    delayedTransport.newStream(method, headers);
    delayedTransport.newStream(method2, headers2);
    assertEquals(0, fakeExecutor.numPendingTasks());
    delayedTransport.setTransportSupplier(new Supplier<ClientTransport>() {
        final Iterator<ClientTransport> it =
            Arrays.asList(mockRealTransport, mockRealTransport2).iterator();

        @Override public ClientTransport get() {
          return it.next();
        }
      });
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockRealTransport).newStream(same(method), same(headers));
    verify(mockRealTransport2).newStream(same(method2), same(headers2));
  }

  @Test public void streamStartThenSetTransport() {
    ClientStream stream = delayedTransport.newStream(method, headers);
    stream.start(streamListener);
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    assertTrue(stream instanceof DelayedStream);
    assertEquals(0, fakeExecutor.numPendingTasks());
    delayedTransport.setTransport(mockRealTransport);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockRealTransport).newStream(same(method), same(headers));
    verify(mockRealStream).start(same(streamListener));
  }

  @Test public void newStreamThenSetTransportThenShutdown() {
    ClientStream stream = delayedTransport.newStream(method, headers);
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    assertTrue(stream instanceof DelayedStream);
    delayedTransport.setTransport(mockRealTransport);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockRealTransport).newStream(same(method), same(headers));
    stream.start(streamListener);
    verify(mockRealStream).start(same(streamListener));
  }

  @Test public void transportTerminatedThenSetTransport() {
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    delayedTransport.setTransport(mockRealTransport);
    verifyNoMoreInteractions(transportListener);
  }

  @Test public void setTransportThenShutdownThenNewStream() {
    delayedTransport.setTransport(mockRealTransport);
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    ClientStream stream = delayedTransport.newStream(method, headers);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    stream.start(streamListener);
    assertFalse(stream instanceof DelayedStream);
    verify(mockRealTransport).newStream(same(method), same(headers));
    verify(mockRealStream).start(same(streamListener));
  }

  @Test public void setTransportThenShutdownNowThenNewStream() {
    delayedTransport.setTransport(mockRealTransport);
    delayedTransport.shutdownNow(Status.UNAVAILABLE);
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    ClientStream stream = delayedTransport.newStream(method, headers);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    stream.start(streamListener);
    assertFalse(stream instanceof DelayedStream);
    verify(mockRealTransport).newStream(same(method), same(headers));
    verify(mockRealStream).start(same(streamListener));
  }

  @Test public void cancelStreamWithoutSetTransport() {
    ClientStream stream = delayedTransport.newStream(method, new Metadata());
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    stream.cancel(Status.CANCELLED);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verifyNoMoreInteractions(mockRealTransport);
    verifyNoMoreInteractions(mockRealStream);
  }

  @Test public void startThenCancelStreamWithoutSetTransport() {
    ClientStream stream = delayedTransport.newStream(method, new Metadata());
    stream.start(streamListener);
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    stream.cancel(Status.CANCELLED);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verify(streamListener).closed(same(Status.CANCELLED), any(Metadata.class));
    verifyNoMoreInteractions(mockRealTransport);
    verifyNoMoreInteractions(mockRealStream);
  }

  @Test public void newStreamThenShutdownTransportThenCancelStream() {
    ClientStream stream = delayedTransport.newStream(method, new Metadata());
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener, times(0)).transportTerminated();
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    stream.cancel(Status.CANCELLED);
    verify(transportListener).transportTerminated();
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verifyNoMoreInteractions(mockRealTransport);
    verifyNoMoreInteractions(mockRealStream);
  }

  @Test public void setTransportThenShutdownThenPing() {
    delayedTransport.setTransport(mockRealTransport);
    delayedTransport.shutdown();
    delayedTransport.ping(pingCallback, mockExecutor);
    verify(mockRealTransport).ping(same(pingCallback), same(mockExecutor));
  }

  @Test public void pingThenSetTransport() {
    delayedTransport.ping(pingCallback, mockExecutor);
    delayedTransport.setTransport(mockRealTransport);
    verify(mockRealTransport).ping(same(pingCallback), same(mockExecutor));
  }

  @Test public void shutdownThenPing() {
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    delayedTransport.ping(pingCallback, mockExecutor);
    verifyNoMoreInteractions(pingCallback);
    ArgumentCaptor<Runnable> runnableCaptor = ArgumentCaptor.forClass(Runnable.class);
    verify(mockExecutor).execute(runnableCaptor.capture());
    runnableCaptor.getValue().run();
    verify(pingCallback).onFailure(any(Throwable.class));
  }

  @Test public void shutdownThenNewStream() {
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    ClientStream stream = delayedTransport.newStream(method, new Metadata());
    stream.start(streamListener);
    verify(streamListener).closed(statusCaptor.capture(), any(Metadata.class));
    assertEquals(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());
  }

  @Test public void startStreamThenShutdownNow() {
    ClientStream stream = delayedTransport.newStream(method, new Metadata());
    stream.start(streamListener);
    delayedTransport.shutdownNow(Status.UNAVAILABLE);
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    verify(streamListener).closed(statusCaptor.capture(), any(Metadata.class));
    assertEquals(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());
  }

  @Test public void shutdownNowThenNewStream() {
    delayedTransport.shutdownNow(Status.UNAVAILABLE);
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    ClientStream stream = delayedTransport.newStream(method, new Metadata());
    stream.start(streamListener);
    verify(streamListener).closed(statusCaptor.capture(), any(Metadata.class));
    assertEquals(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());
  }
}
