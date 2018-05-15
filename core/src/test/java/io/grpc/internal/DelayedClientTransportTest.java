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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.CallOptions;
import io.grpc.IntegerMarshaller;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.internal.ClientStreamListener.RpcProgress;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
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

/**
 * Unit tests for {@link DelayedClientTransport}.
 */
@RunWith(JUnit4.class)
public class DelayedClientTransportTest {
  @Mock private ManagedClientTransport.Listener transportListener;
  @Mock private SubchannelPicker mockPicker;
  @Mock private AbstractSubchannel mockSubchannel;
  @Mock private ClientTransport mockRealTransport;
  @Mock private ClientTransport mockRealTransport2;
  @Mock private ClientStream mockRealStream;
  @Mock private ClientStream mockRealStream2;
  @Mock private ClientStreamListener streamListener;
  @Mock private Executor mockExecutor;
  @Captor private ArgumentCaptor<Status> statusCaptor;
  @Captor private ArgumentCaptor<ClientStreamListener> listenerCaptor;

  private static final CallOptions.Key<Integer> SHARD_ID
      = CallOptions.Key.createWithDefault("shard-id", -1);
  private static final Status SHUTDOWN_STATUS =
      Status.UNAVAILABLE.withDescription("shutdown called");

  private final MethodDescriptor<String, Integer> method =
      MethodDescriptor.<String, Integer>newBuilder()
          .setType(MethodType.UNKNOWN)
          .setFullMethodName("service/method")
          .setRequestMarshaller(new StringMarshaller())
          .setResponseMarshaller(new IntegerMarshaller())
          .build();
  private final MethodDescriptor<String, Integer> method2 =
      method.toBuilder().setFullMethodName("service/method").build();
  private final Metadata headers = new Metadata();
  private final Metadata headers2 = new Metadata();

  private final CallOptions callOptions = CallOptions.DEFAULT.withAuthority("dummy_value");
  private final CallOptions callOptions2 = CallOptions.DEFAULT.withAuthority("dummy_value2");

  private final FakeClock fakeExecutor = new FakeClock();

  private final DelayedClientTransport delayedTransport = new DelayedClientTransport(
      fakeExecutor.getScheduledExecutorService(), new ChannelExecutor());

  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(mockPicker.pickSubchannel(any(PickSubchannelArgs.class)))
        .thenReturn(PickResult.withSubchannel(mockSubchannel));
    when(mockSubchannel.obtainActiveTransport()).thenReturn(mockRealTransport);
    when(mockRealTransport.newStream(same(method), same(headers), same(callOptions)))
        .thenReturn(mockRealStream);
    when(mockRealTransport2.newStream(same(method2), same(headers2), same(callOptions2)))
        .thenReturn(mockRealStream2);
    delayedTransport.start(transportListener);
  }

  @After public void noMorePendingTasks() {
    assertEquals(0, fakeExecutor.numPendingTasks());
  }

  @Test public void streamStartThenAssignTransport() {
    assertFalse(delayedTransport.hasPendingStreams());
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions);
    stream.start(streamListener);
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    assertTrue(delayedTransport.hasPendingStreams());
    assertTrue(stream instanceof DelayedStream);
    assertEquals(0, fakeExecutor.numPendingTasks());
    delayedTransport.reprocess(mockPicker);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    assertFalse(delayedTransport.hasPendingStreams());
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockRealTransport).newStream(same(method), same(headers), same(callOptions));
    verify(mockRealStream).start(listenerCaptor.capture());
    verifyNoMoreInteractions(streamListener);
    listenerCaptor.getValue().onReady();
    verify(streamListener).onReady();
    verifyNoMoreInteractions(streamListener);
  }

  @Test public void newStreamThenAssignTransportThenShutdown() {
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions);
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    assertTrue(stream instanceof DelayedStream);
    delayedTransport.reprocess(mockPicker);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    delayedTransport.shutdown(SHUTDOWN_STATUS);
    verify(transportListener).transportShutdown(same(SHUTDOWN_STATUS));
    verify(transportListener).transportTerminated();
    assertEquals(1, fakeExecutor.runDueTasks());
    verify(mockRealTransport).newStream(same(method), same(headers), same(callOptions));
    stream.start(streamListener);
    verify(mockRealStream).start(same(streamListener));
  }

  @Test public void transportTerminatedThenAssignTransport() {
    delayedTransport.shutdown(SHUTDOWN_STATUS);
    verify(transportListener).transportShutdown(same(SHUTDOWN_STATUS));
    verify(transportListener).transportTerminated();
    delayedTransport.reprocess(mockPicker);
    verifyNoMoreInteractions(transportListener);
  }

  @Test public void assignTransportThenShutdownThenNewStream() {
    delayedTransport.reprocess(mockPicker);
    delayedTransport.shutdown(SHUTDOWN_STATUS);
    verify(transportListener).transportShutdown(same(SHUTDOWN_STATUS));
    verify(transportListener).transportTerminated();
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    assertTrue(stream instanceof FailingClientStream);
    verify(mockRealTransport, never()).newStream(
        any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class));
  }

  @Test public void assignTransportThenShutdownNowThenNewStream() {
    delayedTransport.reprocess(mockPicker);
    delayedTransport.shutdownNow(Status.UNAVAILABLE);
    verify(transportListener).transportShutdown(any(Status.class));
    verify(transportListener).transportTerminated();
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    assertTrue(stream instanceof FailingClientStream);
    verify(mockRealTransport, never()).newStream(
        any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class));
  }

  @Test public void cancelStreamWithoutSetTransport() {
    ClientStream stream = delayedTransport.newStream(method, new Metadata(), CallOptions.DEFAULT);
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    stream.cancel(Status.CANCELLED);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verifyNoMoreInteractions(mockRealTransport);
    verifyNoMoreInteractions(mockRealStream);
  }

  @Test public void startThenCancelStreamWithoutSetTransport() {
    ClientStream stream = delayedTransport.newStream(method, new Metadata(), CallOptions.DEFAULT);
    stream.start(streamListener);
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    stream.cancel(Status.CANCELLED);
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verify(streamListener).closed(same(Status.CANCELLED), any(Metadata.class));
    verifyNoMoreInteractions(mockRealTransport);
    verifyNoMoreInteractions(mockRealStream);
  }

  @Test public void newStreamThenShutdownTransportThenAssignTransport() {
    ClientStream stream = delayedTransport.newStream(method, headers, callOptions);
    stream.start(streamListener);
    delayedTransport.shutdown(SHUTDOWN_STATUS);

    // Stream is still buffered
    verify(transportListener).transportShutdown(same(SHUTDOWN_STATUS));
    verify(transportListener, times(0)).transportTerminated();
    assertEquals(1, delayedTransport.getPendingStreamsCount());

    // ... and will proceed if a real transport is available
    delayedTransport.reprocess(mockPicker);
    fakeExecutor.runDueTasks();
    verify(mockRealTransport).newStream(method, headers, callOptions);
    verify(mockRealStream).start(any(ClientStreamListener.class));

    // Since no more streams are pending, delayed transport is now terminated
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verify(transportListener).transportTerminated();

    // Further newStream() will return a failing stream
    stream = delayedTransport.newStream(method, new Metadata(), CallOptions.DEFAULT);
    verify(streamListener, never()).closed(any(Status.class), any(Metadata.class));
    stream.start(streamListener);
    verify(streamListener).closed(
        statusCaptor.capture(), any(RpcProgress.class), any(Metadata.class));
    assertEquals(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());

    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verifyNoMoreInteractions(mockRealTransport);
    verifyNoMoreInteractions(mockRealStream);
  }

  @Test public void newStreamThenShutdownTransportThenCancelStream() {
    ClientStream stream = delayedTransport.newStream(method, new Metadata(), CallOptions.DEFAULT);
    delayedTransport.shutdown(SHUTDOWN_STATUS);
    verify(transportListener).transportShutdown(same(SHUTDOWN_STATUS));
    verify(transportListener, times(0)).transportTerminated();
    assertEquals(1, delayedTransport.getPendingStreamsCount());
    stream.cancel(Status.CANCELLED);
    verify(transportListener).transportTerminated();
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verifyNoMoreInteractions(mockRealTransport);
    verifyNoMoreInteractions(mockRealStream);
  }

  @Test public void shutdownThenNewStream() {
    delayedTransport.shutdown(SHUTDOWN_STATUS);
    verify(transportListener).transportShutdown(same(SHUTDOWN_STATUS));
    verify(transportListener).transportTerminated();
    ClientStream stream = delayedTransport.newStream(method, new Metadata(), CallOptions.DEFAULT);
    stream.start(streamListener);
    verify(streamListener).closed(
        statusCaptor.capture(), any(RpcProgress.class), any(Metadata.class));
    assertEquals(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());
  }

  @Test public void startStreamThenShutdownNow() {
    ClientStream stream = delayedTransport.newStream(method, new Metadata(), CallOptions.DEFAULT);
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
    ClientStream stream = delayedTransport.newStream(method, new Metadata(), CallOptions.DEFAULT);
    stream.start(streamListener);
    verify(streamListener).closed(
        statusCaptor.capture(), any(RpcProgress.class), any(Metadata.class));
    assertEquals(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());
  }

  @Test public void reprocessSemantics() {
    CallOptions failFastCallOptions = CallOptions.DEFAULT.withOption(SHARD_ID, 1);
    CallOptions waitForReadyCallOptions = CallOptions.DEFAULT.withOption(SHARD_ID, 2)
        .withWaitForReady();

    AbstractSubchannel subchannel1 = mock(AbstractSubchannel.class);
    AbstractSubchannel subchannel2 = mock(AbstractSubchannel.class);
    AbstractSubchannel subchannel3 = mock(AbstractSubchannel.class);
    when(mockRealTransport.newStream(any(MethodDescriptor.class), any(Metadata.class),
        any(CallOptions.class))).thenReturn(mockRealStream);
    when(mockRealTransport2.newStream(any(MethodDescriptor.class), any(Metadata.class),
        any(CallOptions.class))).thenReturn(mockRealStream2);
    when(subchannel1.obtainActiveTransport()).thenReturn(mockRealTransport);
    when(subchannel2.obtainActiveTransport()).thenReturn(mockRealTransport2);
    when(subchannel3.obtainActiveTransport()).thenReturn(null);

    // Fail-fast streams
    DelayedStream ff1 = (DelayedStream) delayedTransport.newStream(
        method, headers, failFastCallOptions);
    PickSubchannelArgsImpl ff1args = new PickSubchannelArgsImpl(method, headers,
        failFastCallOptions);
    verify(transportListener).transportInUse(true);
    DelayedStream ff2 = (DelayedStream) delayedTransport.newStream(
        method2, headers2, failFastCallOptions);
    PickSubchannelArgsImpl ff2args = new PickSubchannelArgsImpl(method2, headers2,
        failFastCallOptions);
    DelayedStream ff3 = (DelayedStream) delayedTransport.newStream(
        method, headers, failFastCallOptions);
    PickSubchannelArgsImpl ff3args = new PickSubchannelArgsImpl(method, headers,
        failFastCallOptions);
    DelayedStream ff4 = (DelayedStream) delayedTransport.newStream(
        method2, headers2, failFastCallOptions);
    PickSubchannelArgsImpl ff4args = new PickSubchannelArgsImpl(method2, headers2,
        failFastCallOptions);

    // Wait-for-ready streams
    FakeClock wfr3Executor = new FakeClock();
    DelayedStream wfr1 = (DelayedStream) delayedTransport.newStream(
        method, headers, waitForReadyCallOptions);
    PickSubchannelArgsImpl wfr1args = new PickSubchannelArgsImpl(method, headers,
        waitForReadyCallOptions);
    DelayedStream wfr2 = (DelayedStream) delayedTransport.newStream(
        method2, headers2, waitForReadyCallOptions);
    PickSubchannelArgsImpl wfr2args = new PickSubchannelArgsImpl(method2, headers2,
        waitForReadyCallOptions);
    CallOptions wfr3callOptions = waitForReadyCallOptions.withExecutor(
        wfr3Executor.getScheduledExecutorService());
    DelayedStream wfr3 = (DelayedStream) delayedTransport.newStream(
        method, headers, wfr3callOptions);
    PickSubchannelArgsImpl wfr3args = new PickSubchannelArgsImpl(method, headers,
        wfr3callOptions);
    DelayedStream wfr4 = (DelayedStream) delayedTransport.newStream(
        method2, headers2, waitForReadyCallOptions);
    PickSubchannelArgsImpl wfr4args = new PickSubchannelArgsImpl(method2, headers2,
        waitForReadyCallOptions);

    assertEquals(8, delayedTransport.getPendingStreamsCount());

    // First reprocess(). Some will proceed, some will fail and the rest will stay buffered.
    SubchannelPicker picker = mock(SubchannelPicker.class);
    when(picker.pickSubchannel(any(PickSubchannelArgs.class))).thenReturn(
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
    inOrder.verify(picker).pickSubchannel(ff1args);
    inOrder.verify(picker).pickSubchannel(ff2args);
    inOrder.verify(picker).pickSubchannel(ff3args);
    inOrder.verify(picker).pickSubchannel(ff4args);
    inOrder.verify(picker).pickSubchannel(wfr1args);
    inOrder.verify(picker).pickSubchannel(wfr2args);
    inOrder.verify(picker).pickSubchannel(wfr3args);
    inOrder.verify(picker).pickSubchannel(wfr4args);

    inOrder.verifyNoMoreInteractions();
    // Make sure that real transport creates streams in the executor
    verify(mockRealTransport, never()).newStream(
        any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class));
    verify(mockRealTransport2, never()).newStream(
        any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class));
    fakeExecutor.runDueTasks();
    assertEquals(0, fakeExecutor.numPendingTasks());
    // ff1 and wfr1 went through
    verify(mockRealTransport).newStream(method, headers, failFastCallOptions);
    verify(mockRealTransport2).newStream(method, headers, waitForReadyCallOptions);
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
    when(picker.pickSubchannel(any(PickSubchannelArgs.class))).thenReturn(
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
    inOrder.verify(picker).pickSubchannel(ff3args);  // ff3
    inOrder.verify(picker).pickSubchannel(ff4args);  // ff4
    inOrder.verify(picker).pickSubchannel(wfr2args);  // wfr2
    inOrder.verify(picker).pickSubchannel(wfr3args);  // wfr3
    inOrder.verify(picker).pickSubchannel(wfr4args);  // wfr4
    inOrder.verifyNoMoreInteractions();
    fakeExecutor.runDueTasks();
    assertEquals(0, fakeExecutor.numPendingTasks());
    assertSame(mockRealStream, ff3.getRealStream());
    assertSame(mockRealStream2, ff4.getRealStream());
    assertSame(mockRealStream2, wfr2.getRealStream());
    assertSame(mockRealStream2, wfr4.getRealStream());

    // If there is an executor in the CallOptions, it will be used to create the real stream.
    assertNull(wfr3.getRealStream());
    wfr3Executor.runDueTasks();
    assertSame(mockRealStream, wfr3.getRealStream());

    // New streams will use the last picker
    DelayedStream wfr5 = (DelayedStream) delayedTransport.newStream(
        method, headers, waitForReadyCallOptions);
    assertNull(wfr5.getRealStream());
    inOrder.verify(picker).pickSubchannel(
        new PickSubchannelArgsImpl(method, headers, waitForReadyCallOptions));
    inOrder.verifyNoMoreInteractions();
    assertEquals(1, delayedTransport.getPendingStreamsCount());

    // wfr5 will stop delayed transport from terminating
    delayedTransport.shutdown(SHUTDOWN_STATUS);
    verify(transportListener).transportShutdown(same(SHUTDOWN_STATUS));
    verify(transportListener, never()).transportTerminated();
    // ... until it's gone
    picker = mock(SubchannelPicker.class);
    when(picker.pickSubchannel(any(PickSubchannelArgs.class))).thenReturn(
        PickResult.withSubchannel(subchannel1));
    delayedTransport.reprocess(picker);
    verify(picker).pickSubchannel(
        new PickSubchannelArgsImpl(method, headers, waitForReadyCallOptions));
    fakeExecutor.runDueTasks();
    assertSame(mockRealStream, wfr5.getRealStream());
    assertEquals(0, delayedTransport.getPendingStreamsCount());
    verify(transportListener).transportTerminated();
  }

  @Test
  public void reprocess_NoPendingStream() {
    SubchannelPicker picker = mock(SubchannelPicker.class);
    AbstractSubchannel subchannel = mock(AbstractSubchannel.class);
    when(subchannel.obtainActiveTransport()).thenReturn(mockRealTransport);
    when(picker.pickSubchannel(any(PickSubchannelArgs.class))).thenReturn(
        PickResult.withSubchannel(subchannel));
    when(mockRealTransport.newStream(any(MethodDescriptor.class), any(Metadata.class),
            any(CallOptions.class))).thenReturn(mockRealStream);
    delayedTransport.reprocess(picker);
    verifyNoMoreInteractions(picker);
    verifyNoMoreInteractions(transportListener);

    // Though picker was not originally used, it will be saved and serve future streams.
    ClientStream stream = delayedTransport.newStream(method, headers, CallOptions.DEFAULT);
    verify(picker).pickSubchannel(new PickSubchannelArgsImpl(method, headers, CallOptions.DEFAULT));
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
    }).when(picker).pickSubchannel(any(PickSubchannelArgs.class));

    // Because there is no pending stream yet, it will do nothing but save the picker.
    delayedTransport.reprocess(picker);
    verify(picker, never()).pickSubchannel(any(PickSubchannelArgs.class));

    Thread sideThread = new Thread("sideThread") {
        @Override
        public void run() {
          // Will call pickSubchannel and wait on barrier
          delayedTransport.newStream(method, headers, callOptions);
        }
      };
    sideThread.start();

    PickSubchannelArgsImpl args = new PickSubchannelArgsImpl(method, headers, callOptions);
    PickSubchannelArgsImpl args2 = new PickSubchannelArgsImpl(method, headers2, callOptions);

    // Is called from sideThread
    verify(picker, timeout(5000)).pickSubchannel(args);

    // Because stream has not been buffered (it's still stuck in newStream()), this will do nothing,
    // but incrementing the picker version.
    delayedTransport.reprocess(picker);
    verify(picker).pickSubchannel(args);

    // Now let the stuck newStream() through
    barrier.await(5, TimeUnit.SECONDS);

    sideThread.join(5000);
    assertFalse("sideThread should've exited", sideThread.isAlive());
    // newStream() detects that there has been a new picker while it's stuck, thus will pick again.
    verify(picker, times(2)).pickSubchannel(args);

    barrier.reset();
    nextPickShouldWait.set(true);

    ////////// Phase 2: reprocess() with a different picker
    // Create the second stream
    Thread sideThread2 = new Thread("sideThread2") {
        @Override
        public void run() {
          // Will call pickSubchannel and wait on barrier
          delayedTransport.newStream(method, headers2, callOptions);
        }
      };
    sideThread2.start();
    // The second stream will see the first picker
    verify(picker, timeout(5000)).pickSubchannel(args2);
    // While the first stream won't use the first picker any more.
    verify(picker, times(2)).pickSubchannel(args);

    // Now use a different picker
    SubchannelPicker picker2 = mock(SubchannelPicker.class);
    when(picker2.pickSubchannel(any(PickSubchannelArgs.class)))
        .thenReturn(PickResult.withNoResult());
    delayedTransport.reprocess(picker2);
    // The pending first stream uses the new picker
    verify(picker2).pickSubchannel(args);
    // The second stream is still pending in creation, doesn't use the new picker.
    verify(picker2, never()).pickSubchannel(args2);

    // Now let the second stream finish creation
    barrier.await(5, TimeUnit.SECONDS);

    sideThread2.join(5000);
    assertFalse("sideThread2 should've exited", sideThread2.isAlive());
    // The second stream should see the new picker
    verify(picker2, timeout(5000)).pickSubchannel(args2);

    // Wrapping up
    verify(picker, times(2)).pickSubchannel(args);
    verify(picker).pickSubchannel(args2);
    verify(picker2).pickSubchannel(args);
    verify(picker2).pickSubchannel(args);
  }
}
