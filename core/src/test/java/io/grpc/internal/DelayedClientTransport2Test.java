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
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.any;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.same;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.IntegerMarshaller;
import io.grpc.LoadBalancer2.PickResult;
import io.grpc.LoadBalancer2.SubchannelPicker;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.StringMarshaller;

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

import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * Unit tests for {@link DelayedClientTransport2}.
 */
@RunWith(JUnit4.class)
public class DelayedClientTransport2Test {
  @Mock private ManagedClientTransport.Listener transportListener;
  @Mock private SubchannelPicker mockPicker;
  @Mock private SubchannelImpl mockSubchannel;
  @Mock private ClientTransport mockRealTransport;
  @Mock private ClientTransport mockRealTransport2;
  @Mock private ClientStream mockRealStream;
  @Mock private ClientStream mockRealStream2;
  @Mock private ClientStreamListener streamListener;
  @Mock private Executor mockExecutor;
  @Captor private ArgumentCaptor<Status> statusCaptor;
  @Captor private ArgumentCaptor<ClientStreamListener> listenerCaptor;

  private static final Attributes.Key<Integer> SHARD_ID = Attributes.Key.of("shard-id");

  private final MethodDescriptor<String, Integer> method = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "/service/method",
      new StringMarshaller(), new IntegerMarshaller());

  private final MethodDescriptor<String, Integer> method2 = MethodDescriptor.create(
      MethodDescriptor.MethodType.UNKNOWN, "/service/method2",
      new StringMarshaller(), new IntegerMarshaller());

  private final Metadata headers = new Metadata();
  private final Metadata headers2 = new Metadata();
  private final Metadata headers3 = new Metadata();

  private final CallOptions callOptions = CallOptions.DEFAULT.withAuthority("dummy_value");
  private final CallOptions callOptions2 = CallOptions.DEFAULT.withAuthority("dummy_value2");
  private final StatsTraceContext statsTraceCtx = StatsTraceContext.newClientContext(
      method.getFullMethodName(), NoopCensusContextFactory.INSTANCE,
      GrpcUtil.STOPWATCH_SUPPLIER);
  private final StatsTraceContext statsTraceCtx2 = StatsTraceContext.newClientContext(
      method2.getFullMethodName(), NoopCensusContextFactory.INSTANCE,
      GrpcUtil.STOPWATCH_SUPPLIER);

  private final FakeClock fakeExecutor = new FakeClock();

  private final DelayedClientTransport2 delayedTransport = new DelayedClientTransport2(
      fakeExecutor.getScheduledExecutorService(), new ChannelExecutor());

  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(mockPicker.pickSubchannel(any(Attributes.class), any(Metadata.class)))
        .thenReturn(PickResult.withSubchannel(mockSubchannel));
    when(mockSubchannel.obtainActiveTransport()).thenReturn(mockRealTransport);
    when(mockRealTransport.newStream(same(method), same(headers), same(callOptions),
            same(statsTraceCtx)))
        .thenReturn(mockRealStream);
    when(mockRealTransport2.newStream(same(method2), same(headers2), same(callOptions2),
            same(statsTraceCtx2)))
        .thenReturn(mockRealStream2);
    delayedTransport.start(transportListener);
  }

  @After public void noMorePendingTasks() {
    assertEquals(0, fakeExecutor.numPendingTasks());
  }

  @Test public void streamStartThenAssignTransport() {
    assertFalse(delayedTransport.hasPendingStreams());
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions, statsTraceCtx);
    stream.start(streamListener);
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    assertTrue(delayedTransport.hasPendingStreams());
    assertTrue(stream instanceof DelayedStream);
    assertEquals(0, fakeExecutor.numPendingTasks());
    delayedTransport.reprocess(mockPicker);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    assertFalse(delayedTransport.hasPendingStreams());
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockRealTransport).newStream(same(method), same(headers), same(callOptions),
        same(statsTraceCtx));
    verify(mockRealStream).start(listenerCaptor.capture());
    verifyNoMoreInteractions(streamListener);
    listenerCaptor.getValue().onReady();
    verify(streamListener).onReady();
    verifyNoMoreInteractions(streamListener);
  }

  @Test public void newStreamThenAssignTransportThenShutdown() {
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions, statsTraceCtx);
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    assertTrue(stream instanceof DelayedStream);
    delayedTransport.reprocess(mockPicker);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockRealTransport).newStream(same(method), same(headers), same(callOptions),
        same(statsTraceCtx));
    stream.start(streamListener);
    verify(mockRealStream).start(same(streamListener));
  }

  @Test public void transportTerminatedThenAssignTransport() {
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    delayedTransport.reprocess(mockPicker);
    verifyNoMoreInteractions(transportListener);
  }

  @Test public void assignTransportThenShutdownThenNewStream() {
    delayedTransport.reprocess(mockPicker);
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions, statsTraceCtx);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    assertTrue(stream instanceof FailingClientStream);
    verify(mockRealTransport, never()).newStream(any(MethodDescriptor.class), any(Metadata.class),
        any(CallOptions.class), any(StatsTraceContext.class));
  }

  @Test public void assignTransportThenShutdownNowThenNewStream() {
    delayedTransport.reprocess(mockPicker);
    delayedTransport.shutdownNow(Status.UNAVAILABLE);
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions, statsTraceCtx);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    assertTrue(stream instanceof FailingClientStream);
    verify(mockRealTransport, never()).newStream(any(MethodDescriptor.class), any(Metadata.class),
        any(CallOptions.class), any(StatsTraceContext.class));
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

  @Test public void newStreamThenShutdownTransportThenAssignTransport() {
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions, statsTraceCtx);
    stream.start(streamListener);
    delayedTransport.shutdown();

    // Stream is still buffered
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener, times(0)).transportTerminated();
    assertEquals(1, delayedTransport.getPendingStreamsCount());

    // ... and will proceed if a real transport is available
    delayedTransport.reprocess(mockPicker);
    fakeExecutor.runDueTasks();
    verify(mockRealTransport).newStream(method, headers, callOptions, statsTraceCtx);
    verify(mockRealStream).start(any(ClientStreamListener.class));

    // Since no more streams are pending, delayed transport is now terminated
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verify(transportListener).transportTerminated();

    // Further newStream() will return a failing stream
    stream = delayedTransport.newStream(method, new Metadata());
    verify(streamListener, never()).closed(any(Status.class), any(Metadata.class));
    stream.start(streamListener);
    verify(streamListener).closed(statusCaptor.capture(), any(Metadata.class));
    assertEquals(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());

    assertEquals(0, delayedTransport.getPendingStreamsCount());
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

  @Test public void reprocessSemantics() {
    Attributes affinity1 = Attributes.newBuilder().set(SHARD_ID, 1).build();
    Attributes affinity2 = Attributes.newBuilder().set(SHARD_ID, 2).build();
    CallOptions failFastCallOptions = CallOptions.DEFAULT.withAffinity(affinity1);
    CallOptions waitForReadyCallOptions =
        CallOptions.DEFAULT.withWaitForReady().withAffinity(affinity2);

    SubchannelImpl subchannel1 = mock(SubchannelImpl.class);
    SubchannelImpl subchannel2 = mock(SubchannelImpl.class);
    SubchannelImpl subchannel3 = mock(SubchannelImpl.class);
    when(mockRealTransport.newStream(any(MethodDescriptor.class), any(Metadata.class),
            any(CallOptions.class), same(statsTraceCtx))).thenReturn(mockRealStream);
    when(mockRealTransport2.newStream(any(MethodDescriptor.class), any(Metadata.class),
            any(CallOptions.class), same(statsTraceCtx))).thenReturn(mockRealStream2);
    when(subchannel1.obtainActiveTransport()).thenReturn(mockRealTransport);
    when(subchannel2.obtainActiveTransport()).thenReturn(mockRealTransport2);
    when(subchannel3.obtainActiveTransport()).thenReturn(null);

    // Fail-fast streams
    DelayedStream ff1 = (DelayedStream) delayedTransport.newStream(
        method, headers, failFastCallOptions, statsTraceCtx);
    verify(transportListener).transportInUse(true);
    DelayedStream ff2 = (DelayedStream) delayedTransport.newStream(
        method2, headers2, failFastCallOptions, statsTraceCtx);
    DelayedStream ff3 = (DelayedStream) delayedTransport.newStream(
        method, headers, failFastCallOptions, statsTraceCtx);
    DelayedStream ff4 = (DelayedStream) delayedTransport.newStream(
        method2, headers2, failFastCallOptions, statsTraceCtx);

    // Wait-for-ready streams
    FakeClock wfr3Executor = new FakeClock();
    DelayedStream wfr1 = (DelayedStream) delayedTransport.newStream(
        method, headers, waitForReadyCallOptions, statsTraceCtx);
    DelayedStream wfr2 = (DelayedStream) delayedTransport.newStream(
        method2, headers2, waitForReadyCallOptions, statsTraceCtx);
    DelayedStream wfr3 = (DelayedStream) delayedTransport.newStream(
        method, headers,
        waitForReadyCallOptions.withExecutor(wfr3Executor.getScheduledExecutorService()),
        statsTraceCtx);
    DelayedStream wfr4 = (DelayedStream) delayedTransport.newStream(
        method2, headers2, waitForReadyCallOptions, statsTraceCtx);
    assertEquals(8, delayedTransport.getPendingStreamsCount());

    // First reprocess(). Some will proceed, some will fail and the rest will stay buffered.
    SubchannelPicker picker = mock(SubchannelPicker.class);
    when(picker.pickSubchannel(any(Attributes.class), any(Metadata.class))).thenReturn(
        // For the fail-fast streams
        PickResult.withSubchannel(subchannel1),    // ff1: proceed
        PickResult.withError(Status.UNAVAILABLE),  // ff2: fail
        PickResult.withSubchannel(subchannel3),    // ff3: stay
        PickResult.withNoResult(),                 // ff4: stay
        // For the wait-for-ready streams
        PickResult.withSubchannel(subchannel2),           // wfr1: proceed
        PickResult.withError(Status.RESOURCE_EXHAUSTED),  // wfr2: stay
        PickResult.withSubchannel(subchannel3));          // wfr3: stay
    InOrder inOrder = inOrder(picker);
    delayedTransport.reprocess(picker);

    assertEquals(5, delayedTransport.getPendingStreamsCount());
    inOrder.verify(picker).pickSubchannel(affinity1, headers);  // ff1
    inOrder.verify(picker).pickSubchannel(affinity1, headers2);  // ff2
    inOrder.verify(picker).pickSubchannel(affinity1, headers);  // ff3
    inOrder.verify(picker).pickSubchannel(affinity1, headers2);  // ff4
    inOrder.verify(picker).pickSubchannel(affinity2, headers);  // wfr1
    inOrder.verify(picker).pickSubchannel(affinity2, headers2);  // wfr2
    inOrder.verify(picker).pickSubchannel(affinity2, headers);  // wfr3
    inOrder.verify(picker).pickSubchannel(affinity2, headers2);  // wfr4
    inOrder.verifyNoMoreInteractions();
    // Make sure that real transport creates streams in the executor
    verify(mockRealTransport, never()).newStream(any(MethodDescriptor.class),
        any(Metadata.class), any(CallOptions.class), any(StatsTraceContext.class));
    verify(mockRealTransport2, never()).newStream(any(MethodDescriptor.class),
        any(Metadata.class), any(CallOptions.class), any(StatsTraceContext.class));
    fakeExecutor.runDueTasks();
    assertEquals(0, fakeExecutor.numPendingTasks());
    // ff1 and wfr1 went through
    verify(mockRealTransport).newStream(method, headers, failFastCallOptions, statsTraceCtx);
    verify(mockRealTransport2).newStream(method, headers, waitForReadyCallOptions, statsTraceCtx);
    assertSame(mockRealStream, ff1.getRealStream());
    assertSame(mockRealStream2, wfr1.getRealStream());
    // The ff2 has failed due to picker returning an error
    assertSame(Status.UNAVAILABLE, ((FailingClientStream) ff2.getRealStream()).getError());
    // Other streams are still buffered
    assertNull(ff3.getRealStream());
    assertNull(ff4.getRealStream());
    assertNull(wfr2.getRealStream());
    assertNull(wfr3.getRealStream());
    assertNull(wfr4.getRealStream());

    // Second reprocess(). All existing streams will proceed.
    picker = mock(SubchannelPicker.class);
    when(picker.pickSubchannel(any(Attributes.class), any(Metadata.class))).thenReturn(
        PickResult.withSubchannel(subchannel1),  // ff3
        PickResult.withSubchannel(subchannel2),  // ff4
        PickResult.withSubchannel(subchannel2),  // wfr2
        PickResult.withSubchannel(subchannel1),  // wfr3
        PickResult.withSubchannel(subchannel2),  // wfr4
        PickResult.withNoResult());              // wfr5 (not yet created)
    inOrder = inOrder(picker);
    assertEquals(0, wfr3Executor.numPendingTasks());
    verify(transportListener, never()).transportInUse(false);

    delayedTransport.reprocess(picker);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verify(transportListener).transportInUse(false);
    inOrder.verify(picker).pickSubchannel(affinity1, headers);  // ff3
    inOrder.verify(picker).pickSubchannel(affinity1, headers2);  // ff4
    inOrder.verify(picker).pickSubchannel(affinity2, headers2);  // wfr2
    inOrder.verify(picker).pickSubchannel(affinity2, headers);  // wfr3
    inOrder.verify(picker).pickSubchannel(affinity2, headers2);  // wfr4
    inOrder.verifyNoMoreInteractions();
    fakeExecutor.runDueTasks();
    assertEquals(0, fakeExecutor.numPendingTasks());
    assertSame(mockRealStream, ff3.getRealStream());
    assertSame(mockRealStream2, ff4.getRealStream());
    assertSame(mockRealStream2, wfr2.getRealStream());
    assertSame(mockRealStream2, wfr4.getRealStream());

    // If there is an executor in the CallOptions, it will be used to create the real tream.
    assertNull(wfr3.getRealStream());
    wfr3Executor.runDueTasks();
    assertSame(mockRealStream, wfr3.getRealStream());

    // New streams will use the last picker
    DelayedStream wfr5 = (DelayedStream) delayedTransport.newStream(
        method, headers, waitForReadyCallOptions, statsTraceCtx);
    assertNull(wfr5.getRealStream());
    inOrder.verify(picker).pickSubchannel(affinity2, headers);
    inOrder.verifyNoMoreInteractions();
    assertEquals(1, delayedTransport.getPendingStreamsCount());

    // wfr5 will stop delayed transport from terminating
    delayedTransport.shutdown();
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener, never()).transportTerminated();
    // ... until it's gone
    picker = mock(SubchannelPicker.class);
    when(picker.pickSubchannel(any(Attributes.class), any(Metadata.class))).thenReturn(
        PickResult.withSubchannel(subchannel1));
    delayedTransport.reprocess(picker);
    verify(picker).pickSubchannel(affinity2, headers);
    fakeExecutor.runDueTasks();
    assertSame(mockRealStream, wfr5.getRealStream());
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verify(transportListener).transportTerminated();
  }

  @Test
  public void reprocess_NoPendingStream() {
    SubchannelPicker picker = mock(SubchannelPicker.class);
    SubchannelImpl subchannel = mock(SubchannelImpl.class);
    when(subchannel.obtainActiveTransport()).thenReturn(mockRealTransport);
    when(picker.pickSubchannel(any(Attributes.class), any(Metadata.class))).thenReturn(
        PickResult.withSubchannel(subchannel));
    when(mockRealTransport.newStream(any(MethodDescriptor.class), any(Metadata.class),
            any(CallOptions.class), same(statsTraceCtx))).thenReturn(mockRealStream);
    delayedTransport.reprocess(picker);
    verifyNoMoreInteractions(picker);
    verifyNoMoreInteractions(transportListener);

    // Though picker was not originally used, it will be saved and serve future streams.
    ClientStream stream = delayedTransport.newStream(
        method, headers, CallOptions.DEFAULT, statsTraceCtx);
    verify(picker).pickSubchannel(CallOptions.DEFAULT.getAffinity(), headers);
    verify(subchannel).obtainActiveTransport();
    assertSame(mockRealStream, stream);
  }

  @Test
  public void reprocess_newStreamRacesWithReprocess() throws Exception {
    final CyclicBarrier barrier = new CyclicBarrier(2);
    // In both phases, we only expect the first pickSubchannel() call to block on the barrier.
    final AtomicBoolean nextPickShouldWait = new AtomicBoolean(true);
    ///////// Phase 1: reprocess() twice with the same picker
    SubchannelPicker picker = mock(SubchannelPicker.class);

    doAnswer(new Answer<PickResult>() {
        @Override
        public PickResult answer(InvocationOnMock invocation) throws Throwable {
          if (nextPickShouldWait.compareAndSet(true, false)) {
            try {
              barrier.await();
              return PickResult.withNoResult();
            } catch (Exception e) {
              e.printStackTrace();
            }
          }
          return PickResult.withNoResult();
        }
      }).when(picker).pickSubchannel(any(Attributes.class), any(Metadata.class));

    // Because there is no pending stream yet, it will do nothing but save the picker.
    delayedTransport.reprocess(picker);
    verify(picker, never()).pickSubchannel(any(Attributes.class), any(Metadata.class));

    Thread sideThread = new Thread("sideThread") {
        @Override
        public void run() {
          // Will call pickSubchannel and wait on barrier
          delayedTransport.newStream(method, headers, callOptions, statsTraceCtx);
        }
      };
    sideThread.start();

    // Is called from sideThread
    verify(picker, timeout(5000)).pickSubchannel(callOptions.getAffinity(), headers);

    // Because stream has not been buffered (it's still stuck in newStream()), this will do nothing,
    // but incrementing the picker version.
    delayedTransport.reprocess(picker);
    verify(picker).pickSubchannel(callOptions.getAffinity(), headers);

    // Now let the stuck newStream() through
    barrier.await(5, TimeUnit.SECONDS);

    sideThread.join(5000);
    assertFalse("sideThread should've exited", sideThread.isAlive());
    // newStream() detects that there has been a new picker while it's stuck, thus will pick again.
    verify(picker, times(2)).pickSubchannel(callOptions.getAffinity(), headers);

    barrier.reset();
    nextPickShouldWait.set(true);

    ////////// Phase 2: reprocess() with a different picker
    // Create the second stream
    Thread sideThread2 = new Thread("sideThread2") {
        @Override
        public void run() {
          // Will call pickSubchannel and wait on barrier
          delayedTransport.newStream(method, headers2, callOptions, statsTraceCtx);
        }
      };
    sideThread2.start();
    // The second stream will see the first picker
    verify(picker, timeout(5000)).pickSubchannel(callOptions.getAffinity(), headers2);
    // While the first stream won't use the first picker any more.
    verify(picker, times(2)).pickSubchannel(callOptions.getAffinity(), headers);

    // Now use a different picker
    SubchannelPicker picker2 = mock(SubchannelPicker.class);
    when(picker2.pickSubchannel(any(Attributes.class), any(Metadata.class)))
        .thenReturn(PickResult.withNoResult());
    delayedTransport.reprocess(picker2);
    // The pending first stream uses the new picker
    verify(picker2).pickSubchannel(callOptions.getAffinity(), headers);
    // The second stream is still pending in creation, doesn't use the new picker.
    verify(picker2, never()).pickSubchannel(callOptions.getAffinity(), headers2);

    // Now let the second stream finish creation
    barrier.await(5, TimeUnit.SECONDS);

    sideThread2.join(5000);
    assertFalse("sideThread2 should've exited", sideThread2.isAlive());
    // The second stream should see the new picker
    verify(picker2, timeout(5000)).pickSubchannel(callOptions.getAffinity(), headers2);

    // Wrapping up
    verify(picker, times(2)).pickSubchannel(callOptions.getAffinity(), headers);
    verify(picker).pickSubchannel(callOptions.getAffinity(), headers2);
    verify(picker2).pickSubchannel(callOptions.getAffinity(), headers);
    verify(picker2).pickSubchannel(callOptions.getAffinity(), headers2);
  }
}
