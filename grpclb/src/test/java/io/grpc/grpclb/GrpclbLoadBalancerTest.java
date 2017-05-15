/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.grpclb;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.SHUTDOWN;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.atLeast;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.util.concurrent.MoreExecutors;
import com.google.protobuf.ByteString;
import com.google.protobuf.util.Durations;
import com.google.protobuf.util.Timestamps;
import io.grpc.Attributes;
import io.grpc.ClientStreamTracer;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.grpclb.GrpclbConstants.LbPolicy;
import io.grpc.grpclb.GrpclbLoadBalancer.ErrorPicker;
import io.grpc.grpclb.GrpclbLoadBalancer.RoundRobinEntry;
import io.grpc.grpclb.GrpclbLoadBalancer.RoundRobinPicker;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.FakeClock;
import io.grpc.internal.ObjectPool;
import io.grpc.internal.SerializingExecutor;
import io.grpc.stub.StreamObserver;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedList;
import java.util.List;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
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

/** Unit tests for {@link GrpclbLoadBalancer}. */
@RunWith(JUnit4.class)
public class GrpclbLoadBalancerTest {
  private static final Attributes.Key<String> RESOLUTION_ATTR =
      Attributes.Key.of("resolution-attr");
  private static final String SERVICE_AUTHORITY = "api.google.com";

  @Mock
  private Helper helper;
  private LoadBalancerGrpc.LoadBalancerImplBase mockLbService;
  @Captor
  private ArgumentCaptor<StreamObserver<LoadBalanceResponse>> lbResponseObserverCaptor;
  private final FakeClock fakeClock = new FakeClock();
  private final LinkedList<StreamObserver<LoadBalanceRequest>> lbRequestObservers =
      new LinkedList<StreamObserver<LoadBalanceRequest>>();
  private final LinkedList<Subchannel> mockSubchannels = new LinkedList<Subchannel>();
  private final LinkedList<ManagedChannel> fakeOobChannels = new LinkedList<ManagedChannel>();
  private final ArrayList<Subchannel> subchannelTracker = new ArrayList<Subchannel>();
  private final ArrayList<ManagedChannel> oobChannelTracker = new ArrayList<ManagedChannel>();
  private final ArrayList<String> failingLbAuthorities = new ArrayList<String>();
  private final TimeProvider timeProvider = new TimeProvider() {
      @Override
      public long currentTimeMillis() {
        return fakeClock.currentTimeMillis();
      }
    };

  private io.grpc.Server fakeLbServer;
  @Captor
  private ArgumentCaptor<SubchannelPicker> pickerCaptor;
  private final SerializingExecutor channelExecutor =
      new SerializingExecutor(MoreExecutors.directExecutor());
  @Mock
  private LoadBalancer.Factory pickFirstBalancerFactory;
  @Mock
  private LoadBalancer pickFirstBalancer;
  @Mock
  private LoadBalancer.Factory roundRobinBalancerFactory;
  @Mock
  private LoadBalancer roundRobinBalancer;
  @Mock
  private ObjectPool<ScheduledExecutorService> timerServicePool;
  private GrpclbLoadBalancer balancer;

  @SuppressWarnings("unchecked")
  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    when(pickFirstBalancerFactory.newLoadBalancer(any(Helper.class)))
        .thenReturn(pickFirstBalancer);
    when(roundRobinBalancerFactory.newLoadBalancer(any(Helper.class)))
        .thenReturn(roundRobinBalancer);
    mockLbService = spy(new LoadBalancerGrpc.LoadBalancerImplBase() {
        @Override
        public StreamObserver<LoadBalanceRequest> balanceLoad(
            final StreamObserver<LoadBalanceResponse> responseObserver) {
          StreamObserver<LoadBalanceRequest> requestObserver =
              mock(StreamObserver.class);
          Answer<Void> closeRpc = new Answer<Void>() {
              @Override
              public Void answer(InvocationOnMock invocation) {
                responseObserver.onCompleted();
                return null;
              }
            };
          doAnswer(closeRpc).when(requestObserver).onCompleted();
          lbRequestObservers.add(requestObserver);
          return requestObserver;
        }
      });
    fakeLbServer = InProcessServerBuilder.forName("fakeLb")
        .directExecutor().addService(mockLbService).build().start();
    doAnswer(new Answer<ManagedChannel>() {
        @Override
        public ManagedChannel answer(InvocationOnMock invocation) throws Throwable {
          String authority = (String) invocation.getArguments()[1];
          ManagedChannel channel;
          if (failingLbAuthorities.contains(authority)) {
            channel = InProcessChannelBuilder.forName("nonExistFakeLb").directExecutor()
                .overrideAuthority(authority).build();
          } else {
            channel = InProcessChannelBuilder.forName("fakeLb").directExecutor()
                .overrideAuthority(authority).build();
          }
          fakeOobChannels.add(channel);
          oobChannelTracker.add(channel);
          return channel;
        }
      }).when(helper).createOobChannel(any(EquivalentAddressGroup.class), any(String.class));
    doAnswer(new Answer<Subchannel>() {
        @Override
        public Subchannel answer(InvocationOnMock invocation) throws Throwable {
          Subchannel subchannel = mock(Subchannel.class);
          EquivalentAddressGroup eag = (EquivalentAddressGroup) invocation.getArguments()[0];
          Attributes attrs = (Attributes) invocation.getArguments()[1];
          when(subchannel.getAddresses()).thenReturn(eag);
          when(subchannel.getAttributes()).thenReturn(attrs);
          mockSubchannels.add(subchannel);
          subchannelTracker.add(subchannel);
          return subchannel;
        }
      }).when(helper).createSubchannel(any(EquivalentAddressGroup.class), any(Attributes.class));
    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) throws Throwable {
          Runnable task = (Runnable) invocation.getArguments()[0];
          channelExecutor.execute(task);
          return null;
        }
      }).when(helper).runSerialized(any(Runnable.class));
    when(helper.getAuthority()).thenReturn(SERVICE_AUTHORITY);
    when(timerServicePool.getObject()).thenReturn(fakeClock.getScheduledExecutorService());
    balancer = new GrpclbLoadBalancer(
        helper, pickFirstBalancerFactory, roundRobinBalancerFactory,
        timerServicePool,
        timeProvider);
  }

  @After
  public void tearDown() {
    try {
      if (balancer != null) {
        channelExecutor.execute(new Runnable() {
            @Override
            public void run() {
              balancer.shutdown();
            }
          });
      }
      for (ManagedChannel channel : oobChannelTracker) {
        assertTrue(channel + " is shutdown", channel.isShutdown());
        // balancer should have closed the LB stream, terminating the OOB channel.
        assertTrue(channel + " is terminated", channel.isTerminated());
      }
      for (Subchannel subchannel: subchannelTracker) {
        verify(subchannel).shutdown();
      }
    } finally {
      if (fakeLbServer != null) {
        fakeLbServer.shutdownNow();
      }
    }
  }

  @Test
  public void errorPicker() {
    PickSubchannelArgs mockArgs = mock(PickSubchannelArgs.class);
    Status error = Status.UNAVAILABLE.withDescription("Just don't know why");
    ErrorPicker picker = new ErrorPicker(error);
    assertSame(error, picker.pickSubchannel(mockArgs).getStatus());
    verifyNoMoreInteractions(mockArgs);
  }

  @Test
  public void roundRobinPicker() {
    GrpclbClientLoadRecorder loadRecorder = new GrpclbClientLoadRecorder(timeProvider);
    Subchannel subchannel = mock(Subchannel.class);
    RoundRobinEntry r1 = new RoundRobinEntry(DropType.RATE_LIMITING, loadRecorder);
    RoundRobinEntry r2 = new RoundRobinEntry(subchannel, loadRecorder, "LBTOKEN0001");
    RoundRobinEntry r3 = new RoundRobinEntry(subchannel, loadRecorder, "LBTOKEN0002");

    List<RoundRobinEntry> list = Arrays.asList(r1, r2, r3);
    RoundRobinPicker picker = new RoundRobinPicker(list);

    PickSubchannelArgs args1 = mock(PickSubchannelArgs.class);
    Metadata headers1 = new Metadata();
    when(args1.getHeaders()).thenReturn(headers1);
    assertSame(r1.result, picker.pickSubchannel(args1));
    verify(args1).getHeaders();
    assertFalse(headers1.containsKey(GrpclbConstants.TOKEN_METADATA_KEY));

    PickSubchannelArgs args2 = mock(PickSubchannelArgs.class);
    Metadata headers2 = new Metadata();
    // The existing token on the headers will be replaced
    headers2.put(GrpclbConstants.TOKEN_METADATA_KEY, "LBTOKEN__OLD");
    when(args2.getHeaders()).thenReturn(headers2);
    assertSame(r2.result, picker.pickSubchannel(args2));
    verify(args2).getHeaders();
    assertThat(headers2.getAll(GrpclbConstants.TOKEN_METADATA_KEY)).containsExactly("LBTOKEN0001");

    PickSubchannelArgs args3 = mock(PickSubchannelArgs.class);
    Metadata headers3 = new Metadata();
    when(args3.getHeaders()).thenReturn(headers3);
    assertSame(r3.result, picker.pickSubchannel(args3));
    verify(args3).getHeaders();
    assertThat(headers3.getAll(GrpclbConstants.TOKEN_METADATA_KEY)).containsExactly("LBTOKEN0002");

    PickSubchannelArgs args4 = mock(PickSubchannelArgs.class);
    Metadata headers4 = new Metadata();
    when(args4.getHeaders()).thenReturn(headers4);
    assertSame(r1.result, picker.pickSubchannel(args4));
    verify(args4).getHeaders();
    assertFalse(headers4.containsKey(GrpclbConstants.TOKEN_METADATA_KEY));

    verify(subchannel, never()).getAttributes();
  }

  @Test
  public void bufferPicker() {
    PickSubchannelArgs args = mock(PickSubchannelArgs.class);
    assertEquals(PickResult.withNoResult(),
        GrpclbLoadBalancer.BUFFER_PICKER.pickSubchannel(args));
    verifyNoMoreInteractions(args);
  }

  @Test
  public void loadReporting() {
    Metadata headers = new Metadata();
    PickSubchannelArgs args = mock(PickSubchannelArgs.class);
    when(args.getHeaders()).thenReturn(headers);

    long loadReportIntervalMillis = 1983;
    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);
    assertEquals(1, fakeOobChannels.size());
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    StreamObserver<LoadBalanceRequest> lbRequestObserver = lbRequestObservers.poll();
    InOrder inOrder = inOrder(lbRequestObserver);
    InOrder helperInOrder = inOrder(helper);

    inOrder.verify(lbRequestObserver).onNext(
        eq(LoadBalanceRequest.newBuilder().setInitialRequest(
                InitialLoadBalanceRequest.newBuilder().setName(SERVICE_AUTHORITY).build())
            .build()));

    // Simulate receiving LB response
    assertEquals(0, fakeClock.numPendingTasks());
    lbResponseObserver.onNext(buildInitialResponse(loadReportIntervalMillis));

    // Load reporting task is scheduled
    assertEquals(1, fakeClock.numPendingTasks());
    assertEquals(0, fakeClock.runDueTasks());

    List<ServerEntry> backends = Arrays.asList(
        new ServerEntry("127.0.0.1", 2000, "token0001"),
        new ServerEntry(DropType.RATE_LIMITING),
        new ServerEntry("127.0.0.1", 2010, "token0002"),
        new ServerEntry(DropType.LOAD_BALANCING));

    lbResponseObserver.onNext(buildLbResponse(backends));

    assertEquals(2, mockSubchannels.size());
    Subchannel subchannel1 = mockSubchannels.poll();
    Subchannel subchannel2 = mockSubchannels.poll();
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(CONNECTING));
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(CONNECTING));
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(READY));
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(READY));

    helperInOrder.verify(helper, atLeast(1)).updatePicker(pickerCaptor.capture());
    RoundRobinPicker picker = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker.list).containsExactly(
        new RoundRobinEntry(subchannel1, balancer.getLoadRecorder(), "token0001"),
        new RoundRobinEntry(DropType.RATE_LIMITING, balancer.getLoadRecorder()),
        new RoundRobinEntry(subchannel2, balancer.getLoadRecorder(), "token0002"),
        new RoundRobinEntry(DropType.LOAD_BALANCING, balancer.getLoadRecorder())).inOrder();

    // Report, no data
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder().build());

    PickResult pick1 = picker.pickSubchannel(args);
    assertSame(subchannel1, pick1.getSubchannel());
    assertSame(balancer.getLoadRecorder(), pick1.getStreamTracerFactory());

    // Merely the pick will not be recorded as upstart.
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder().build());

    ClientStreamTracer tracer1 =
        pick1.getStreamTracerFactory().newClientStreamTracer(new Metadata());

    PickResult pick2 = picker.pickSubchannel(args);
    assertNull(pick2.getSubchannel());
    assertSame(GrpclbLoadBalancer.DROP_PICK_RESULTS.get(DropType.RATE_LIMITING), pick2);

    // Report includes upstart of pick1 and the drop of pick2
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder()
            .setNumCallsStarted(2)
            .setNumCallsFinished(1)  // pick2
            .setNumCallsFinishedWithDropForRateLimiting(1)  // pick2
            .build());

    PickResult pick3 = picker.pickSubchannel(args);
    assertSame(subchannel2, pick3.getSubchannel());
    assertSame(balancer.getLoadRecorder(), pick3.getStreamTracerFactory());
    ClientStreamTracer tracer3 =
        pick3.getStreamTracerFactory().newClientStreamTracer(new Metadata());

    // pick3 has sent out headers
    tracer3.outboundHeaders();

    // 3rd report includes pick3's upstart
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder()
            .setNumCallsStarted(1)
            .build());

    PickResult pick4 = picker.pickSubchannel(args);
    assertNull(pick4.getSubchannel());
    assertSame(GrpclbLoadBalancer.DROP_PICK_RESULTS.get(DropType.LOAD_BALANCING), pick4);

    // pick1 ended without sending anything
    tracer1.streamClosed(Status.CANCELLED);

    // 4th report includes end of pick1 and drop of pick4
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder()
            .setNumCallsStarted(1)  // pick4
            .setNumCallsFinished(2)
            .setNumCallsFinishedWithClientFailedToSend(1)   // pick1
            .setNumCallsFinishedWithDropForLoadBalancing(1)   // pick4
            .build());

    PickResult pick5 = picker.pickSubchannel(args);
    assertSame(subchannel1, pick1.getSubchannel());
    assertSame(balancer.getLoadRecorder(), pick5.getStreamTracerFactory());
    ClientStreamTracer tracer5 =
        pick5.getStreamTracerFactory().newClientStreamTracer(new Metadata());

    // pick3 ended without receiving response headers
    tracer3.streamClosed(Status.DEADLINE_EXCEEDED);

    // pick5 sent and received headers
    tracer5.outboundHeaders();
    tracer5.inboundHeaders();

    // 5th report includes pick3's end and pick5's upstart
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder()
            .setNumCallsStarted(1)  // pick5
            .setNumCallsFinished(1)  // pick3
            .build());

    // pick5 ends
    tracer5.streamClosed(Status.OK);

    // 6th report includes pick5's end
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder()
            .setNumCallsFinished(1)
            .setNumCallsFinishedKnownReceived(1)
            .build());

    assertEquals(1, fakeClock.numPendingTasks());
    // Balancer closes the stream, scheduled reporting task cancelled
    lbResponseObserver.onError(Status.UNAVAILABLE.asException());
    assertEquals(0, fakeClock.numPendingTasks());

    // New stream created
    verify(mockLbService, times(2)).balanceLoad(lbResponseObserverCaptor.capture());
    lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    lbRequestObserver = lbRequestObservers.poll();
    inOrder = inOrder(lbRequestObserver);

    inOrder.verify(lbRequestObserver).onNext(
        eq(LoadBalanceRequest.newBuilder().setInitialRequest(
                InitialLoadBalanceRequest.newBuilder().setName(SERVICE_AUTHORITY).build())
            .build()));

    // Load reporting is also requested
    lbResponseObserver.onNext(buildInitialResponse(loadReportIntervalMillis));

    // No picker created because balancer is still using the results from the last stream
    helperInOrder.verify(helper, never()).updatePicker(any(SubchannelPicker.class));

    // Make a new pick on that picker.  It will not show up on the report of the new stream, because
    // that picker is associated with the previous stream.
    PickResult pick6 = picker.pickSubchannel(args);
    assertNull(pick6.getSubchannel());
    assertSame(GrpclbLoadBalancer.DROP_PICK_RESULTS.get(DropType.RATE_LIMITING), pick6);
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder().build());

    // New stream got the list update
    lbResponseObserver.onNext(buildLbResponse(backends));

    // Same backends, thus no new subchannels
    helperInOrder.verify(helper, never()).createSubchannel(
        any(EquivalentAddressGroup.class), any(Attributes.class));
    // But the new RoundRobinEntries have a new loadRecorder, thus considered different from
    // the previous list, thus a new picker is created
    helperInOrder.verify(helper).updatePicker(pickerCaptor.capture());
    picker = (RoundRobinPicker) pickerCaptor.getValue();

    PickResult pick1p = picker.pickSubchannel(args);
    assertSame(subchannel1, pick1p.getSubchannel());
    assertSame(balancer.getLoadRecorder(), pick1p.getStreamTracerFactory());
    pick1p.getStreamTracerFactory().newClientStreamTracer(new Metadata());

    // The pick from the new stream will be included in the report
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder()
            .setNumCallsStarted(1)
            .build());

    verify(args, atLeast(0)).getHeaders();
    verifyNoMoreInteractions(args);
  }

  @Test
  public void abundantInitialResponse() {
    Metadata headers = new Metadata();
    PickSubchannelArgs args = mock(PickSubchannelArgs.class);
    when(args.getHeaders()).thenReturn(headers);

    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);
    assertEquals(1, fakeOobChannels.size());
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();

    // Simulate LB initial response
    assertEquals(0, fakeClock.numPendingTasks());
    lbResponseObserver.onNext(buildInitialResponse(1983));

    // Load reporting task is scheduled
    assertEquals(1, fakeClock.numPendingTasks());
    FakeClock.ScheduledTask scheduledTask = fakeClock.getPendingTasks().iterator().next();
    assertEquals(1983, scheduledTask.getDelay(TimeUnit.MILLISECONDS));

    // Simulate an abundant LB initial response, with a different report interval
    lbResponseObserver.onNext(buildInitialResponse(9097));
    // It doesn't affect load-reporting at all
    assertThat(fakeClock.getPendingTasks()).containsExactly(scheduledTask);
    assertEquals(1983, scheduledTask.getDelay(TimeUnit.MILLISECONDS));
  }

  @Test
  public void raceBetweenLoadReportingAndLbStreamClosure() {
    Metadata headers = new Metadata();
    PickSubchannelArgs args = mock(PickSubchannelArgs.class);
    when(args.getHeaders()).thenReturn(headers);

    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);
    assertEquals(1, fakeOobChannels.size());
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    StreamObserver<LoadBalanceRequest> lbRequestObserver = lbRequestObservers.poll();
    InOrder inOrder = inOrder(lbRequestObserver);

    inOrder.verify(lbRequestObserver).onNext(
        eq(LoadBalanceRequest.newBuilder().setInitialRequest(
                InitialLoadBalanceRequest.newBuilder().setName(SERVICE_AUTHORITY).build())
            .build()));

    // Simulate receiving LB response
    assertEquals(0, fakeClock.numPendingTasks());
    lbResponseObserver.onNext(buildInitialResponse(1983));

    // Load reporting task is scheduled
    assertEquals(1, fakeClock.numPendingTasks());
    FakeClock.ScheduledTask scheduledTask = fakeClock.getPendingTasks().iterator().next();
    assertEquals(1983, scheduledTask.getDelay(TimeUnit.MILLISECONDS));

    // Close lbStream
    lbResponseObserver.onCompleted();

    // Reporting task cancelled
    assertEquals(0, fakeClock.numPendingTasks());

    // Simulate a race condition where the task has just started when its cancelled
    scheduledTask.command.run();

    // No report sent. No new task scheduled
    inOrder.verify(lbRequestObserver, never()).onNext(any(LoadBalanceRequest.class));
    assertEquals(0, fakeClock.numPendingTasks());
  }

  private void assertNextReport(
      InOrder inOrder, StreamObserver<LoadBalanceRequest> lbRequestObserver,
      long loadReportIntervalMillis, ClientStats expectedReport) {
    assertEquals(0, fakeClock.forwardTime(loadReportIntervalMillis - 1, TimeUnit.MILLISECONDS));
    inOrder.verifyNoMoreInteractions();
    assertEquals(1, fakeClock.forwardTime(1, TimeUnit.MILLISECONDS));
    assertEquals(1, fakeClock.numPendingTasks());
    inOrder.verify(lbRequestObserver).onNext(
        eq(LoadBalanceRequest.newBuilder()
            .setClientStats(
                ClientStats.newBuilder(expectedReport)
                .setTimestamp(Timestamps.fromMillis(fakeClock.currentTimeMillis()))
                .build())
            .build()));
  }

  @Test
  public void acquireAndReleaseScheduledExecutor() {
    verify(timerServicePool).getObject();
    verifyNoMoreInteractions(timerServicePool);

    balancer.shutdown();
    verify(timerServicePool).returnObject(same(fakeClock.getScheduledExecutorService()));
    verifyNoMoreInteractions(timerServicePool);
  }

  @Test
  public void nameResolutionFailsThenRecoverToDelegate() {
    Status error = Status.NOT_FOUND.withDescription("www.google.com not found");
    deliverNameResolutionError(error);
    verify(helper).updatePicker(pickerCaptor.capture());
    ErrorPicker errorPicker = (ErrorPicker) pickerCaptor.getValue();
    assertSame(error, errorPicker.result.getStatus());

    // Recover with a subsequent success
    List<EquivalentAddressGroup> resolvedServers = createResolvedServerAddresses(false);

    Attributes resolutionAttrs = Attributes.newBuilder().set(RESOLUTION_ATTR, "yeah").build();
    deliverResolvedAddresses(resolvedServers, resolutionAttrs);

    verify(pickFirstBalancerFactory).newLoadBalancer(helper);
    verify(pickFirstBalancer).handleResolvedAddressGroups(eq(resolvedServers), eq(resolutionAttrs));
    verifyNoMoreInteractions(roundRobinBalancerFactory);
    verifyNoMoreInteractions(roundRobinBalancer);
  }

  @Test
  public void nameResolutionFailsThenRecoverToGrpclb() {
    Status error = Status.NOT_FOUND.withDescription("www.google.com not found");
    deliverNameResolutionError(error);
    verify(helper).updatePicker(pickerCaptor.capture());
    ErrorPicker errorPicker = (ErrorPicker) pickerCaptor.getValue();
    assertSame(error, errorPicker.result.getStatus());

    // Recover with a subsequent success
    List<EquivalentAddressGroup> resolvedServers = createResolvedServerAddresses(true);
    EquivalentAddressGroup eag = resolvedServers.get(0);

    Attributes resolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(resolvedServers, resolutionAttrs);

    assertSame(LbPolicy.GRPCLB, balancer.getLbPolicy());
    assertNull(balancer.getDelegate());
    verify(helper).createOobChannel(addrsEq(eag), eq(lbAuthority(0)));
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());

    verifyNoMoreInteractions(pickFirstBalancerFactory);
    verifyNoMoreInteractions(pickFirstBalancer);
    verifyNoMoreInteractions(roundRobinBalancerFactory);
    verifyNoMoreInteractions(roundRobinBalancer);
  }

  @Test
  public void delegatingPickFirstThenNameResolutionFails() {
    List<EquivalentAddressGroup> resolvedServers = createResolvedServerAddresses(false);

    Attributes resolutionAttrs = Attributes.newBuilder().set(RESOLUTION_ATTR, "yeah").build();
    deliverResolvedAddresses(resolvedServers, resolutionAttrs);

    verify(pickFirstBalancerFactory).newLoadBalancer(helper);
    verify(pickFirstBalancer).handleResolvedAddressGroups(eq(resolvedServers), eq(resolutionAttrs));

    // Then let name resolution fail.  The error will be passed directly to the delegate.
    Status error = Status.NOT_FOUND.withDescription("www.google.com not found");
    deliverNameResolutionError(error);
    verify(pickFirstBalancer).handleNameResolutionError(error);
    verify(helper, never()).updatePicker(any(SubchannelPicker.class));
    verifyNoMoreInteractions(roundRobinBalancerFactory);
    verifyNoMoreInteractions(roundRobinBalancer);
  }

  @Test
  public void delegatingRoundRobinThenNameResolutionFails() {
    List<EquivalentAddressGroup> resolvedServers = createResolvedServerAddresses(false, false);

    Attributes resolutionAttrs = Attributes.newBuilder()
        .set(RESOLUTION_ATTR, "yeah")
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.ROUND_ROBIN)
        .build();
    deliverResolvedAddresses(resolvedServers, resolutionAttrs);

    verify(roundRobinBalancerFactory).newLoadBalancer(helper);
    verify(roundRobinBalancer).handleResolvedAddressGroups(resolvedServers, resolutionAttrs);

    // Then let name resolution fail.  The error will be passed directly to the delegate.
    Status error = Status.NOT_FOUND.withDescription("www.google.com not found");
    deliverNameResolutionError(error);
    verify(roundRobinBalancer).handleNameResolutionError(error);
    verify(helper, never()).updatePicker(any(SubchannelPicker.class));
    verifyNoMoreInteractions(pickFirstBalancerFactory);
    verifyNoMoreInteractions(pickFirstBalancer);
  }

  @Test
  public void grpclbThenNameResolutionFails() {
    InOrder inOrder = inOrder(helper);
    // Go to GRPCLB first
    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    assertSame(LbPolicy.GRPCLB, balancer.getLbPolicy());
    assertNull(balancer.getDelegate());
    verify(helper).createOobChannel(addrsEq(grpclbResolutionList.get(0)), eq(lbAuthority(0)));
    assertEquals(1, fakeOobChannels.size());
    ManagedChannel oobChannel = fakeOobChannels.poll();
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();

    // Let name resolution fail before round-robin list is ready
    Status error = Status.NOT_FOUND.withDescription("www.google.com not found");
    deliverNameResolutionError(error);

    inOrder.verify(helper).updatePicker(pickerCaptor.capture());
    ErrorPicker errorPicker = (ErrorPicker) pickerCaptor.getValue();
    assertSame(error, errorPicker.result.getStatus());
    assertFalse(oobChannel.isShutdown());

    // Simulate receiving LB response
    List<ServerEntry> backends = Arrays.asList(
        new ServerEntry("127.0.0.1", 2000, "TOKEN1"),
        new ServerEntry("127.0.0.1", 2010, "TOKEN2"));
    verify(helper, never()).runSerialized(any(Runnable.class));
    lbResponseObserver.onNext(buildInitialResponse());
    lbResponseObserver.onNext(buildLbResponse(backends));

    verify(helper, times(2)).runSerialized(any(Runnable.class));
    inOrder.verify(helper).createSubchannel(
        eq(new EquivalentAddressGroup(backends.get(0).addr)), any(Attributes.class));
    inOrder.verify(helper).createSubchannel(
        eq(new EquivalentAddressGroup(backends.get(1).addr)), any(Attributes.class));
  }

  @SuppressWarnings("unchecked")
  @Test
  public void switchPolicy() {
    // Go to GRPCLB first
    List<EquivalentAddressGroup> grpclbResolutionList =
        createResolvedServerAddresses(true, false);
    Attributes grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    assertSame(LbPolicy.GRPCLB, balancer.getLbPolicy());
    assertNull(balancer.getDelegate());
    verify(helper).createOobChannel(addrsEq(grpclbResolutionList.get(0)), eq(lbAuthority(0)));
    assertEquals(1, fakeOobChannels.size());
    ManagedChannel oobChannel = fakeOobChannels.poll();
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());

    // Switch to PICK_FIRST
    List<EquivalentAddressGroup> pickFirstResolutionList =
        createResolvedServerAddresses(true, false);
    Attributes pickFirstResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.PICK_FIRST).build();
    verify(pickFirstBalancerFactory, never()).newLoadBalancer(any(Helper.class));
    assertEquals(1, lbRequestObservers.size());
    StreamObserver<LoadBalanceRequest> lbRequestObserver = lbRequestObservers.poll();

    verify(lbRequestObserver, never()).onCompleted();
    assertFalse(oobChannel.isShutdown());
    deliverResolvedAddresses(pickFirstResolutionList, pickFirstResolutionAttrs);

    verify(pickFirstBalancerFactory).newLoadBalancer(same(helper));
    // Only non-LB addresses are passed to the delegate
    verify(pickFirstBalancer).handleResolvedAddressGroups(
        eq(Arrays.asList(pickFirstResolutionList.get(1))), same(pickFirstResolutionAttrs));
    assertSame(LbPolicy.PICK_FIRST, balancer.getLbPolicy());
    assertSame(pickFirstBalancer, balancer.getDelegate());
    // GRPCLB connection is closed
    verify(lbRequestObserver).onCompleted();
    assertTrue(oobChannel.isShutdown());

    // Switch to ROUND_ROBIN
    List<EquivalentAddressGroup> roundRobinResolutionList =
        createResolvedServerAddresses(true, false, false);
    Attributes roundRobinResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.ROUND_ROBIN).build();
    verify(roundRobinBalancerFactory, never()).newLoadBalancer(any(Helper.class));
    deliverResolvedAddresses(roundRobinResolutionList, roundRobinResolutionAttrs);

    verify(roundRobinBalancerFactory).newLoadBalancer(same(helper));
    // Only non-LB addresses are passed to the delegate
    verify(roundRobinBalancer).handleResolvedAddressGroups(
        eq(roundRobinResolutionList.subList(1, 3)), same(roundRobinResolutionAttrs));
    assertSame(LbPolicy.ROUND_ROBIN, balancer.getLbPolicy());
    assertSame(roundRobinBalancer, balancer.getDelegate());

    // Special case: if all addresses are loadbalancers, use GRPCLB no matter what the NameResolver
    // says.
    grpclbResolutionList = createResolvedServerAddresses(true, true, true);
    grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.PICK_FIRST).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    assertSame(LbPolicy.GRPCLB, balancer.getLbPolicy());
    assertNull(balancer.getDelegate());
    EquivalentAddressGroup combinedEag = new EquivalentAddressGroup(Arrays.asList(
        grpclbResolutionList.get(0).getAddresses().get(0),
        grpclbResolutionList.get(1).getAddresses().get(0),
        grpclbResolutionList.get(2).getAddresses().get(0)));
    verify(helper).createOobChannel(eq(combinedEag), eq(lbAuthority(0)));
    assertEquals(1, fakeOobChannels.size());
    oobChannel = fakeOobChannels.poll();
    verify(mockLbService, times(2)).balanceLoad(lbResponseObserverCaptor.capture());

    // Special case: PICK_FIRST is the default
    pickFirstResolutionList = createResolvedServerAddresses(true, false, false);
    pickFirstResolutionAttrs = Attributes.EMPTY;
    verify(pickFirstBalancerFactory).newLoadBalancer(any(Helper.class));
    assertFalse(oobChannel.isShutdown());
    deliverResolvedAddresses(pickFirstResolutionList, pickFirstResolutionAttrs);

    verify(pickFirstBalancerFactory, times(2)).newLoadBalancer(same(helper));
    // Only non-LB addresses are passed to the delegate
    verify(pickFirstBalancer).handleResolvedAddressGroups(
        eq(pickFirstResolutionList.subList(1, 3)), same(pickFirstResolutionAttrs));
    assertSame(LbPolicy.PICK_FIRST, balancer.getLbPolicy());
    assertSame(pickFirstBalancer, balancer.getDelegate());
    // GRPCLB connection is closed
    assertTrue(oobChannel.isShutdown());
  }

  @Test
  public void grpclbUpdatedAddresses_avoidsReconnect() {
    List<EquivalentAddressGroup> grpclbResolutionList =
        createResolvedServerAddresses(true, false);
    Attributes grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    assertSame(LbPolicy.GRPCLB, balancer.getLbPolicy());
    verify(helper).createOobChannel(addrsEq(grpclbResolutionList.get(0)), eq(lbAuthority(0)));
    ManagedChannel oobChannel = fakeOobChannels.poll();
    assertEquals(1, lbRequestObservers.size());

    List<EquivalentAddressGroup> grpclbResolutionList2 =
        createResolvedServerAddresses(true, false, true);
    EquivalentAddressGroup combinedEag = new EquivalentAddressGroup(Arrays.asList(
        grpclbResolutionList2.get(0).getAddresses().get(0),
        grpclbResolutionList2.get(2).getAddresses().get(0)));
    deliverResolvedAddresses(grpclbResolutionList2, grpclbResolutionAttrs);
    verify(helper).updateOobChannelAddresses(eq(oobChannel), addrsEq(combinedEag));
    assertEquals(1, lbRequestObservers.size()); // No additional RPC
  }

  @Test
  public void grpclbUpdatedAddresses_reconnectOnAuthorityChange() {
    List<EquivalentAddressGroup> grpclbResolutionList =
        createResolvedServerAddresses(true, false);
    Attributes grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    assertSame(LbPolicy.GRPCLB, balancer.getLbPolicy());
    verify(helper).createOobChannel(addrsEq(grpclbResolutionList.get(0)), eq(lbAuthority(0)));
    ManagedChannel oobChannel = fakeOobChannels.poll();
    assertEquals(1, lbRequestObservers.size());

    final String newAuthority = "some-new-authority";
    List<EquivalentAddressGroup> grpclbResolutionList2 =
        createResolvedServerAddresses(false);
    grpclbResolutionList2.add(new EquivalentAddressGroup(
        new FakeSocketAddress("somethingNew"), lbAttributes(newAuthority)));
    deliverResolvedAddresses(grpclbResolutionList2, grpclbResolutionAttrs);
    assertTrue(oobChannel.isTerminated());
    verify(helper).createOobChannel(addrsEq(grpclbResolutionList2.get(1)), eq(newAuthority));
    assertEquals(2, lbRequestObservers.size()); // An additional RPC
  }

  @Test
  public void grpclbWorking() {
    InOrder inOrder = inOrder(helper);
    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    assertSame(LbPolicy.GRPCLB, balancer.getLbPolicy());
    assertNull(balancer.getDelegate());
    verify(helper).createOobChannel(addrsEq(grpclbResolutionList.get(0)), eq(lbAuthority(0)));
    assertEquals(1, fakeOobChannels.size());
    ManagedChannel oobChannel = fakeOobChannels.poll();
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    StreamObserver<LoadBalanceRequest> lbRequestObserver = lbRequestObservers.poll();
    verify(lbRequestObserver).onNext(
        eq(LoadBalanceRequest.newBuilder().setInitialRequest(
                InitialLoadBalanceRequest.newBuilder().setName(SERVICE_AUTHORITY).build())
            .build()));

    // Simulate receiving LB response
    List<ServerEntry> backends1 = Arrays.asList(
        new ServerEntry("127.0.0.1", 2000, "token0001"),
        new ServerEntry("127.0.0.1", 2010, "token0002"));
    inOrder.verify(helper, never()).updatePicker(any(SubchannelPicker.class));
    lbResponseObserver.onNext(buildInitialResponse());
    lbResponseObserver.onNext(buildLbResponse(backends1));

    inOrder.verify(helper).createSubchannel(
        eq(new EquivalentAddressGroup(backends1.get(0).addr)), any(Attributes.class));
    inOrder.verify(helper).createSubchannel(
        eq(new EquivalentAddressGroup(backends1.get(1).addr)), any(Attributes.class));
    assertEquals(2, mockSubchannels.size());
    Subchannel subchannel1 = mockSubchannels.poll();
    Subchannel subchannel2 = mockSubchannels.poll();
    verify(subchannel1).requestConnection();
    verify(subchannel2).requestConnection();
    assertEquals(new EquivalentAddressGroup(backends1.get(0).addr), subchannel1.getAddresses());
    assertEquals(new EquivalentAddressGroup(backends1.get(1).addr), subchannel2.getAddresses());

    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(CONNECTING));
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(CONNECTING));
    inOrder.verifyNoMoreInteractions();

    // Let subchannels be connected
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(helper).updatePicker(pickerCaptor.capture());
    RoundRobinPicker picker1 = (RoundRobinPicker) pickerCaptor.getValue();

    assertThat(picker1.list).containsExactly(
        new RoundRobinEntry(subchannel2, balancer.getLoadRecorder(), "token0002"));

    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(helper).updatePicker(pickerCaptor.capture());
    RoundRobinPicker picker2 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker2.list).containsExactly(
        new RoundRobinEntry(subchannel1, balancer.getLoadRecorder(), "token0001"),
        new RoundRobinEntry(subchannel2, balancer.getLoadRecorder(), "token0002"))
        .inOrder();

    // Disconnected subchannels
    verify(subchannel1).requestConnection();
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(IDLE));
    verify(subchannel1, times(2)).requestConnection();
    inOrder.verify(helper).updatePicker(pickerCaptor.capture());
    RoundRobinPicker picker3 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker3.list).containsExactly(
        new RoundRobinEntry(subchannel2, balancer.getLoadRecorder(), "token0002"));

    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(CONNECTING));
    inOrder.verifyNoMoreInteractions();

    // As long as there is at least one READY subchannel, round robin will work.
    Status error1 = Status.UNAVAILABLE.withDescription("error1");
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forTransientFailure(error1));
    inOrder.verifyNoMoreInteractions();

    // If no subchannel is READY, will propagate an error from an arbitrary subchannel (but here
    // only subchannel1 has error).
    verify(subchannel2).requestConnection();
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(IDLE));
    verify(subchannel2, times(2)).requestConnection();
    inOrder.verify(helper).updatePicker(pickerCaptor.capture());
    ErrorPicker picker6 = (ErrorPicker) pickerCaptor.getValue();
    assertNull(picker6.result.getSubchannel());
    assertSame(error1, picker6.result.getStatus());

    // Update backends, with a drop entry
    List<ServerEntry> backends2 =
        Arrays.asList(
            new ServerEntry("127.0.0.1", 2030, "token0003"),  // New address
            new ServerEntry(DropType.RATE_LIMITING),
            new ServerEntry("127.0.0.1", 2010, "token0004"),  // Existing address with token changed
            new ServerEntry("127.0.0.1", 2030, "token0005"),  // New address appearing second time
            new ServerEntry(DropType.LOAD_BALANCING));
    verify(subchannel1, never()).shutdown();

    lbResponseObserver.onNext(buildLbResponse(backends2));
    verify(subchannel1).shutdown();  // not in backends2, closed
    verify(subchannel2, never()).shutdown();  // backends2[2], will be kept

    inOrder.verify(helper, never()).createSubchannel(
        eq(new EquivalentAddressGroup(backends2.get(2).addr)), any(Attributes.class));
    inOrder.verify(helper).createSubchannel(
        eq(new EquivalentAddressGroup(backends2.get(0).addr)), any(Attributes.class));
    assertEquals(1, mockSubchannels.size());
    Subchannel subchannel3 = mockSubchannels.poll();
    verify(subchannel3).requestConnection();
    assertEquals(new EquivalentAddressGroup(backends2.get(0).addr), subchannel3.getAddresses());
    inOrder.verify(helper).updatePicker(pickerCaptor.capture());
    RoundRobinPicker picker7 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker7.list).containsExactly(
        new RoundRobinEntry(DropType.RATE_LIMITING, balancer.getLoadRecorder()),
        new RoundRobinEntry(DropType.LOAD_BALANCING, balancer.getLoadRecorder())).inOrder();

    // State updates on obsolete subchannel1 will have no effect
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(READY));
    deliverSubchannelState(
        subchannel1, ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE));
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(SHUTDOWN));
    inOrder.verifyNoMoreInteractions();

    deliverSubchannelState(subchannel3, ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(helper).updatePicker(pickerCaptor.capture());
    RoundRobinPicker picker8 = (RoundRobinPicker) pickerCaptor.getValue();
    // subchannel2 is still IDLE, thus not in the active list
    assertThat(picker8.list).containsExactly(
        new RoundRobinEntry(subchannel3, balancer.getLoadRecorder(), "token0003"),
        new RoundRobinEntry(DropType.RATE_LIMITING, balancer.getLoadRecorder()),
        new RoundRobinEntry(subchannel3, balancer.getLoadRecorder(), "token0005"),
        new RoundRobinEntry(DropType.LOAD_BALANCING, balancer.getLoadRecorder())).inOrder();
    // subchannel2 becomes READY and makes it into the list
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(helper).updatePicker(pickerCaptor.capture());
    RoundRobinPicker picker9 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker9.list).containsExactly(
        new RoundRobinEntry(subchannel3, balancer.getLoadRecorder(), "token0003"),
        new RoundRobinEntry(DropType.RATE_LIMITING, balancer.getLoadRecorder()),
        new RoundRobinEntry(subchannel2, balancer.getLoadRecorder(), "token0004"),
        new RoundRobinEntry(subchannel3, balancer.getLoadRecorder(), "token0005"),
        new RoundRobinEntry(DropType.LOAD_BALANCING, balancer.getLoadRecorder())).inOrder();
    verify(subchannel3, never()).shutdown();

    // Update backends, with no entry
    lbResponseObserver.onNext(buildLbResponse(Collections.<ServerEntry>emptyList()));
    verify(subchannel2).shutdown();
    verify(subchannel3).shutdown();
    inOrder.verify(helper).updatePicker((GrpclbLoadBalancer.BUFFER_PICKER));

    assertFalse(oobChannel.isShutdown());
    assertEquals(0, lbRequestObservers.size());
    verify(lbRequestObserver, never()).onCompleted();
    verify(lbRequestObserver, never()).onError(any(Throwable.class));

    // Load reporting was not requested, thus never scheduled
    assertEquals(0, fakeClock.numPendingTasks());
  }

  @Test
  public void grpclbMultipleAuthorities() throws Exception {
    List<EquivalentAddressGroup> grpclbResolutionList = Arrays.asList(
        new EquivalentAddressGroup(
            new FakeSocketAddress("fake-address-1"),
            lbAttributes("fake-authority-1")),
        new EquivalentAddressGroup(
            new FakeSocketAddress("fake-address-2"),
            lbAttributes("fake-authority-2")),
        new EquivalentAddressGroup(
            new FakeSocketAddress("not-a-lb-address")),
        new EquivalentAddressGroup(
            new FakeSocketAddress("fake-address-3"),
            lbAttributes("fake-authority-1")));
    final EquivalentAddressGroup goldenOobChannelEag = new EquivalentAddressGroup(
        Arrays.<SocketAddress>asList(
            new FakeSocketAddress("fake-address-1"),
            new FakeSocketAddress("fake-address-3")));

    Attributes grpclbResolutionAttrs = Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_POLICY, LbPolicy.GRPCLB).build();
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    assertSame(LbPolicy.GRPCLB, balancer.getLbPolicy());
    assertNull(balancer.getDelegate());
    verify(helper).createOobChannel(goldenOobChannelEag, "fake-authority-1");
  }

  private void deliverSubchannelState(
      final Subchannel subchannel, final ConnectivityStateInfo newState) {
    channelExecutor.execute(new Runnable() {
        @Override
        public void run() {
          balancer.handleSubchannelState(subchannel, newState);
        }
      });
  }

  private void deliverNameResolutionError(final Status error) {
    channelExecutor.execute(new Runnable() {
        @Override
        public void run() {
          balancer.handleNameResolutionError(error);
        }
      });
  }

  private void deliverResolvedAddresses(
      final List<EquivalentAddressGroup> addrs, final Attributes attrs) {
    channelExecutor.execute(new Runnable() {
        @Override
        public void run() {
          balancer.handleResolvedAddressGroups(addrs, attrs);
        }
      });
  }

  private static List<EquivalentAddressGroup> createResolvedServerAddresses(boolean ... isLb) {
    ArrayList<EquivalentAddressGroup> list = new ArrayList<EquivalentAddressGroup>();
    for (int i = 0; i < isLb.length; i++) {
      SocketAddress addr = new FakeSocketAddress("fake-address-" + i);
      EquivalentAddressGroup eag =
          new EquivalentAddressGroup(
              addr,
              isLb[i] ? lbAttributes(lbAuthority(i)) : Attributes.EMPTY);
      list.add(eag);
    }
    return list;
  }

  private static String lbAuthority(int unused) {
    // TODO(ejona): Support varying authorities
    return "lb.google.com";
  }

  private static Attributes lbAttributes(String authority) {
    return Attributes.newBuilder()
        .set(GrpclbConstants.ATTR_LB_ADDR_AUTHORITY, authority)
        .build();
  }

  private static LoadBalanceResponse buildInitialResponse() {
    return buildInitialResponse(0);
  }

  private static LoadBalanceResponse buildInitialResponse(long loadReportIntervalMillis) {
    return LoadBalanceResponse.newBuilder()
        .setInitialResponse(
            InitialLoadBalanceResponse.newBuilder()
            .setClientStatsReportInterval(Durations.fromMillis(loadReportIntervalMillis)))
        .build();
  }

  private static EquivalentAddressGroup addrsEq(EquivalentAddressGroup eag) {
    return eq(new EquivalentAddressGroup(eag.getAddresses()));
  }

  private static LoadBalanceResponse buildLbResponse(List<ServerEntry> servers) {
    ServerList.Builder serverListBuilder = ServerList.newBuilder();
    for (ServerEntry server : servers) {
      if (server.dropType == null) {
        serverListBuilder.addServers(Server.newBuilder()
            .setIpAddress(ByteString.copyFrom(server.addr.getAddress().getAddress()))
            .setPort(server.addr.getPort())
            .setLoadBalanceToken(server.token)
            .build());
      } else {
        switch (server.dropType) {
          case RATE_LIMITING:
            serverListBuilder.addServers(Server.newBuilder().setDropForRateLimiting(true).build());
            break;
          case LOAD_BALANCING:
            serverListBuilder.addServers(Server.newBuilder().setDropForLoadBalancing(true).build());
            break;
          default:
            fail("Unhandled " + server.dropType);
        }
      }
    }
    return LoadBalanceResponse.newBuilder()
        .setServerList(serverListBuilder.build())
        .build();
  }

  private static class ServerEntry {
    final InetSocketAddress addr;
    final String token;
    final DropType dropType;

    ServerEntry(String host, int port, String token) {
      this.addr = new InetSocketAddress(host, port);
      this.token = token;
      this.dropType = null;
    }

    ServerEntry(DropType dropType) {
      this.dropType = dropType;
      this.addr = null;
      this.token = null;
    }
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

    @Override
    public boolean equals(Object other) {
      if (other instanceof FakeSocketAddress) {
        FakeSocketAddress otherAddr = (FakeSocketAddress) other;
        return name.equals(otherAddr.name);
      }
      return false;
    }

    @Override
    public int hashCode() {
      return name.hashCode();
    }
  }
}
