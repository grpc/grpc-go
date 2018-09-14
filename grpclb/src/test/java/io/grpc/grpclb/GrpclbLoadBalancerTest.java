/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.grpclb;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.READY;
import static io.grpc.ConnectivityState.SHUTDOWN;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;
import static io.grpc.grpclb.GrpclbState.BUFFER_ENTRY;
import static io.grpc.grpclb.GrpclbState.DROP_PICK_RESULT;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.atLeast;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.Iterables;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.protobuf.ByteString;
import com.google.protobuf.util.Durations;
import com.google.protobuf.util.Timestamps;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.ClientStreamTracer;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.grpclb.GrpclbState.BackendEntry;
import io.grpc.grpclb.GrpclbState.DropEntry;
import io.grpc.grpclb.GrpclbState.ErrorEntry;
import io.grpc.grpclb.GrpclbState.RoundRobinPicker;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.BackoffPolicy;
import io.grpc.internal.FakeClock;
import io.grpc.internal.GrpcAttributes;
import io.grpc.internal.ObjectPool;
import io.grpc.internal.SerializingExecutor;
import io.grpc.internal.TimeProvider;
import io.grpc.lb.v1.ClientStats;
import io.grpc.lb.v1.ClientStatsPerToken;
import io.grpc.lb.v1.InitialLoadBalanceRequest;
import io.grpc.lb.v1.InitialLoadBalanceResponse;
import io.grpc.lb.v1.LoadBalanceRequest;
import io.grpc.lb.v1.LoadBalanceResponse;
import io.grpc.lb.v1.LoadBalancerGrpc;
import io.grpc.lb.v1.Server;
import io.grpc.lb.v1.ServerList;
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
import javax.annotation.Nullable;
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
      Attributes.Key.create("resolution-attr");
  private static final String SERVICE_AUTHORITY = "api.google.com";
  private static final FakeClock.TaskFilter LOAD_REPORTING_TASK_FILTER =
      new FakeClock.TaskFilter() {
        @Override
        public boolean shouldAccept(Runnable command) {
          return command instanceof GrpclbState.LoadReportingTask;
        }
      };
  private static final FakeClock.TaskFilter FALLBACK_MODE_TASK_FILTER =
      new FakeClock.TaskFilter() {
        @Override
        public boolean shouldAccept(Runnable command) {
          return command instanceof GrpclbState.FallbackModeTask;
        }
      };
  private static final FakeClock.TaskFilter LB_RPC_RETRY_TASK_FILTER =
      new FakeClock.TaskFilter() {
        @Override
        public boolean shouldAccept(Runnable command) {
          return command instanceof GrpclbState.LbRpcRetryTask;
        }
      };
  private static final Attributes LB_BACKEND_ATTRS =
      Attributes.newBuilder().set(GrpcAttributes.ATTR_LB_PROVIDED_BACKEND, true).build();

  @Mock
  private Helper helper;
  @Mock
  private SubchannelPool subchannelPool;
  private SubchannelPicker currentPicker;
  private LoadBalancerGrpc.LoadBalancerImplBase mockLbService;
  @Captor
  private ArgumentCaptor<StreamObserver<LoadBalanceResponse>> lbResponseObserverCaptor;
  private final FakeClock fakeClock = new FakeClock();
  private final LinkedList<StreamObserver<LoadBalanceRequest>> lbRequestObservers =
      new LinkedList<StreamObserver<LoadBalanceRequest>>();
  private final LinkedList<Subchannel> mockSubchannels = new LinkedList<Subchannel>();
  private final LinkedList<ManagedChannel> fakeOobChannels = new LinkedList<ManagedChannel>();
  private final ArrayList<Subchannel> subchannelTracker = new ArrayList<>();
  private final ArrayList<ManagedChannel> oobChannelTracker = new ArrayList<>();
  private final ArrayList<String> failingLbAuthorities = new ArrayList<>();
  private final TimeProvider timeProvider = new TimeProvider() {
      @Override
      public long currentTimeNanos() {
        return fakeClock.getTicker().read();
      }
    };
  private io.grpc.Server fakeLbServer;
  @Captor
  private ArgumentCaptor<SubchannelPicker> pickerCaptor;
  private final SerializingExecutor channelExecutor =
      new SerializingExecutor(MoreExecutors.directExecutor());
  @Mock
  private ObjectPool<ScheduledExecutorService> timerServicePool;
  @Mock
  private BackoffPolicy.Provider backoffPolicyProvider;
  @Mock
  private BackoffPolicy backoffPolicy1;
  @Mock
  private BackoffPolicy backoffPolicy2;
  private GrpclbLoadBalancer balancer;

  @SuppressWarnings("unchecked")
  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    mockLbService = mock(LoadBalancerGrpc.LoadBalancerImplBase.class, delegatesTo(
        new LoadBalancerGrpc.LoadBalancerImplBase() {
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
        }));
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
      }).when(subchannelPool).takeOrCreateSubchannel(
          any(EquivalentAddressGroup.class), any(Attributes.class));
    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) throws Throwable {
          Runnable task = (Runnable) invocation.getArguments()[0];
          channelExecutor.execute(task);
          return null;
        }
      }).when(helper).runSerialized(any(Runnable.class));
    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) throws Throwable {
          currentPicker = (SubchannelPicker) invocation.getArguments()[1];
          return null;
        }
      }).when(helper).updateBalancingState(
          any(ConnectivityState.class), any(SubchannelPicker.class));
    when(helper.getAuthority()).thenReturn(SERVICE_AUTHORITY);
    ScheduledExecutorService timerService = fakeClock.getScheduledExecutorService();
    when(timerServicePool.getObject()).thenReturn(timerService);
    when(backoffPolicy1.nextBackoffNanos()).thenReturn(10L, 100L);
    when(backoffPolicy2.nextBackoffNanos()).thenReturn(10L, 100L);
    when(backoffPolicyProvider.get()).thenReturn(backoffPolicy1, backoffPolicy2);
    balancer = new GrpclbLoadBalancer(
        helper,
        subchannelPool,
        timerServicePool,
        timeProvider,
        backoffPolicyProvider);
    verify(subchannelPool).init(same(helper), same(timerService));
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
      // GRPCLB manages subchannels only through subchannelPool
      for (Subchannel subchannel: subchannelTracker) {
        verify(subchannelPool).returnSubchannel(same(subchannel));
        // Our mock subchannelPool never calls Subchannel.shutdown(), thus we can tell if
        // LoadBalancer has called it expectedly.
        verify(subchannel, never()).shutdown();
      }
      verify(helper, never())
          .createSubchannel(any(EquivalentAddressGroup.class), any(Attributes.class));
      // No timer should linger after shutdown
      assertThat(fakeClock.getPendingTasks()).isEmpty();
    } finally {
      if (fakeLbServer != null) {
        fakeLbServer.shutdownNow();
      }
    }
  }

  @Test
  public void roundRobinPickerNoDrop() {
    GrpclbClientLoadRecorder loadRecorder = new GrpclbClientLoadRecorder(timeProvider);
    Subchannel subchannel = mock(Subchannel.class);
    BackendEntry b1 = new BackendEntry(subchannel, loadRecorder, "LBTOKEN0001");
    BackendEntry b2 = new BackendEntry(subchannel, loadRecorder, "LBTOKEN0002");

    List<BackendEntry> pickList = Arrays.asList(b1, b2);
    RoundRobinPicker picker = new RoundRobinPicker(Collections.<DropEntry>emptyList(), pickList);

    PickSubchannelArgs args1 = mock(PickSubchannelArgs.class);
    Metadata headers1 = new Metadata();
    // The existing token on the headers will be replaced
    headers1.put(GrpclbConstants.TOKEN_METADATA_KEY, "LBTOKEN__OLD");
    when(args1.getHeaders()).thenReturn(headers1);
    assertSame(b1.result, picker.pickSubchannel(args1));
    verify(args1).getHeaders();
    assertThat(headers1.getAll(GrpclbConstants.TOKEN_METADATA_KEY)).containsExactly("LBTOKEN0001");

    PickSubchannelArgs args2 = mock(PickSubchannelArgs.class);
    Metadata headers2 = new Metadata();
    when(args2.getHeaders()).thenReturn(headers2);
    assertSame(b2.result, picker.pickSubchannel(args2));
    verify(args2).getHeaders();
    assertThat(headers2.getAll(GrpclbConstants.TOKEN_METADATA_KEY)).containsExactly("LBTOKEN0002");

    PickSubchannelArgs args3 = mock(PickSubchannelArgs.class);
    Metadata headers3 = new Metadata();
    when(args3.getHeaders()).thenReturn(headers3);
    assertSame(b1.result, picker.pickSubchannel(args3));
    verify(args3).getHeaders();
    assertThat(headers3.getAll(GrpclbConstants.TOKEN_METADATA_KEY)).containsExactly("LBTOKEN0001");

    verify(subchannel, never()).getAttributes();
  }


  @Test
  public void roundRobinPickerWithDrop() {
    assertTrue(DROP_PICK_RESULT.isDrop());
    GrpclbClientLoadRecorder loadRecorder = new GrpclbClientLoadRecorder(timeProvider);
    Subchannel subchannel = mock(Subchannel.class);
    // 1 out of 2 requests are to be dropped
    DropEntry d = new DropEntry(loadRecorder, "LBTOKEN0003");
    List<DropEntry> dropList = Arrays.asList(null, d);

    BackendEntry b1 = new BackendEntry(subchannel, loadRecorder, "LBTOKEN0001");
    BackendEntry b2 = new BackendEntry(subchannel, loadRecorder, "LBTOKEN0002");
    List<BackendEntry> pickList = Arrays.asList(b1, b2);
    RoundRobinPicker picker = new RoundRobinPicker(dropList, pickList);

    // dropList[0], pickList[0]
    PickSubchannelArgs args1 = mock(PickSubchannelArgs.class);
    Metadata headers1 = new Metadata();
    headers1.put(GrpclbConstants.TOKEN_METADATA_KEY, "LBTOKEN__OLD");
    when(args1.getHeaders()).thenReturn(headers1);
    assertSame(b1.result, picker.pickSubchannel(args1));
    verify(args1).getHeaders();
    assertThat(headers1.getAll(GrpclbConstants.TOKEN_METADATA_KEY)).containsExactly("LBTOKEN0001");

    // dropList[1]: drop
    PickSubchannelArgs args2 = mock(PickSubchannelArgs.class);
    Metadata headers2 = new Metadata();
    when(args2.getHeaders()).thenReturn(headers2);
    assertSame(DROP_PICK_RESULT, picker.pickSubchannel(args2));
    verify(args2, never()).getHeaders();

    // dropList[0], pickList[1]
    PickSubchannelArgs args3 = mock(PickSubchannelArgs.class);
    Metadata headers3 = new Metadata();
    when(args3.getHeaders()).thenReturn(headers3);
    assertSame(b2.result, picker.pickSubchannel(args3));
    verify(args3).getHeaders();
    assertThat(headers3.getAll(GrpclbConstants.TOKEN_METADATA_KEY)).containsExactly("LBTOKEN0002");

    // dropList[1]: drop
    PickSubchannelArgs args4 = mock(PickSubchannelArgs.class);
    Metadata headers4 = new Metadata();
    when(args4.getHeaders()).thenReturn(headers4);
    assertSame(DROP_PICK_RESULT, picker.pickSubchannel(args4));
    verify(args4, never()).getHeaders();

    // dropList[0], pickList[0]
    PickSubchannelArgs args5 = mock(PickSubchannelArgs.class);
    Metadata headers5 = new Metadata();
    when(args5.getHeaders()).thenReturn(headers5);
    assertSame(b1.result, picker.pickSubchannel(args5));
    verify(args5).getHeaders();
    assertThat(headers5.getAll(GrpclbConstants.TOKEN_METADATA_KEY)).containsExactly("LBTOKEN0001");

    verify(subchannel, never()).getAttributes();
  }

  @Test
  public void loadReporting() {
    Metadata headers = new Metadata();
    PickSubchannelArgs args = mock(PickSubchannelArgs.class);
    when(args.getHeaders()).thenReturn(headers);

    long loadReportIntervalMillis = 1983;
    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    // Fallback timer is started as soon as address is resolved.
    assertEquals(1, fakeClock.numPendingTasks(FALLBACK_MODE_TASK_FILTER));

    assertEquals(1, fakeOobChannels.size());
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    StreamObserver<LoadBalanceRequest> lbRequestObserver = lbRequestObservers.poll();
    InOrder inOrder = inOrder(lbRequestObserver);
    InOrder helperInOrder = inOrder(helper, subchannelPool);

    inOrder.verify(lbRequestObserver).onNext(
        eq(LoadBalanceRequest.newBuilder().setInitialRequest(
                InitialLoadBalanceRequest.newBuilder().setName(SERVICE_AUTHORITY).build())
            .build()));

    // Simulate receiving LB response
    assertEquals(0, fakeClock.numPendingTasks(LOAD_REPORTING_TASK_FILTER));
    lbResponseObserver.onNext(buildInitialResponse(loadReportIntervalMillis));

    // Load reporting task is scheduled
    assertEquals(1, fakeClock.numPendingTasks(LOAD_REPORTING_TASK_FILTER));
    assertEquals(0, fakeClock.runDueTasks());

    List<ServerEntry> backends = Arrays.asList(
        new ServerEntry("127.0.0.1", 2000, "token0001"),
        new ServerEntry("token0001"),  // drop
        new ServerEntry("127.0.0.1", 2010, "token0002"),
        new ServerEntry("token0003"));  // drop

    lbResponseObserver.onNext(buildLbResponse(backends));

    assertEquals(2, mockSubchannels.size());
    Subchannel subchannel1 = mockSubchannels.poll();
    Subchannel subchannel2 = mockSubchannels.poll();
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(CONNECTING));
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(CONNECTING));
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(READY));
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(READY));

    helperInOrder.verify(helper, atLeast(1))
        .updateBalancingState(eq(READY), pickerCaptor.capture());
    RoundRobinPicker picker = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker.dropList).containsExactly(
        null,
        new DropEntry(getLoadRecorder(), "token0001"),
        null,
        new DropEntry(getLoadRecorder(), "token0003")).inOrder();
    assertThat(picker.pickList).containsExactly(
        new BackendEntry(subchannel1, getLoadRecorder(), "token0001"),
        new BackendEntry(subchannel2, getLoadRecorder(), "token0002")).inOrder();

    // Report, no data
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder().build());

    PickResult pick1 = picker.pickSubchannel(args);
    assertSame(subchannel1, pick1.getSubchannel());
    assertSame(getLoadRecorder(), pick1.getStreamTracerFactory());

    // Merely the pick will not be recorded as upstart.
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder().build());

    ClientStreamTracer tracer1 =
        pick1.getStreamTracerFactory().newClientStreamTracer(CallOptions.DEFAULT, new Metadata());

    PickResult pick2 = picker.pickSubchannel(args);
    assertNull(pick2.getSubchannel());
    assertSame(DROP_PICK_RESULT, pick2);

    // Report includes upstart of pick1 and the drop of pick2
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder()
            .setNumCallsStarted(2)
            .setNumCallsFinished(1)  // pick2
            .addCallsFinishedWithDrop(
                ClientStatsPerToken.newBuilder()
                    .setLoadBalanceToken("token0001")
                    .setNumCalls(1)          // pick2
                    .build())
            .build());

    PickResult pick3 = picker.pickSubchannel(args);
    assertSame(subchannel2, pick3.getSubchannel());
    assertSame(getLoadRecorder(), pick3.getStreamTracerFactory());
    ClientStreamTracer tracer3 =
        pick3.getStreamTracerFactory().newClientStreamTracer(CallOptions.DEFAULT, new Metadata());

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
    assertSame(DROP_PICK_RESULT, pick4);

    // pick1 ended without sending anything
    tracer1.streamClosed(Status.CANCELLED);

    // 4th report includes end of pick1 and drop of pick4
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder()
            .setNumCallsStarted(1)  // pick4
            .setNumCallsFinished(2)
            .setNumCallsFinishedWithClientFailedToSend(1)   // pick1
            .addCallsFinishedWithDrop(
                ClientStatsPerToken.newBuilder()
                    .setLoadBalanceToken("token0003")
                    .setNumCalls(1)   // pick4
                    .build())
        .build());

    PickResult pick5 = picker.pickSubchannel(args);
    assertSame(subchannel1, pick1.getSubchannel());
    assertSame(getLoadRecorder(), pick5.getStreamTracerFactory());
    ClientStreamTracer tracer5 =
        pick5.getStreamTracerFactory().newClientStreamTracer(CallOptions.DEFAULT, new Metadata());

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
    helperInOrder.verify(helper, never())
        .updateBalancingState(any(ConnectivityState.class), any(SubchannelPicker.class));

    // Make a new pick on that picker.  It will not show up on the report of the new stream, because
    // that picker is associated with the previous stream.
    PickResult pick6 = picker.pickSubchannel(args);
    assertNull(pick6.getSubchannel());
    assertSame(DROP_PICK_RESULT, pick6);
    assertNextReport(
        inOrder, lbRequestObserver, loadReportIntervalMillis,
        ClientStats.newBuilder().build());

    // New stream got the list update
    lbResponseObserver.onNext(buildLbResponse(backends));

    // Same backends, thus no new subchannels
    helperInOrder.verify(subchannelPool, never()).takeOrCreateSubchannel(
        any(EquivalentAddressGroup.class), any(Attributes.class));
    // But the new RoundRobinEntries have a new loadRecorder, thus considered different from
    // the previous list, thus a new picker is created
    helperInOrder.verify(helper).updateBalancingState(eq(READY), pickerCaptor.capture());
    picker = (RoundRobinPicker) pickerCaptor.getValue();

    PickResult pick1p = picker.pickSubchannel(args);
    assertSame(subchannel1, pick1p.getSubchannel());
    assertSame(getLoadRecorder(), pick1p.getStreamTracerFactory());
    pick1p.getStreamTracerFactory().newClientStreamTracer(CallOptions.DEFAULT, new Metadata());

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
    Attributes grpclbResolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);
    assertEquals(1, fakeOobChannels.size());
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();

    // Simulate LB initial response
    assertEquals(0, fakeClock.numPendingTasks(LOAD_REPORTING_TASK_FILTER));
    lbResponseObserver.onNext(buildInitialResponse(1983));

    // Load reporting task is scheduled
    assertEquals(1, fakeClock.numPendingTasks(LOAD_REPORTING_TASK_FILTER));
    FakeClock.ScheduledTask scheduledTask = fakeClock.getPendingTasks().iterator().next();
    assertEquals(1983, scheduledTask.getDelay(TimeUnit.MILLISECONDS));

    // Simulate an abundant LB initial response, with a different report interval
    lbResponseObserver.onNext(buildInitialResponse(9097));
    // It doesn't affect load-reporting at all
    assertThat(fakeClock.getPendingTasks(LOAD_REPORTING_TASK_FILTER))
        .containsExactly(scheduledTask);
    assertEquals(1983, scheduledTask.getDelay(TimeUnit.MILLISECONDS));
  }

  @Test
  public void raceBetweenLoadReportingAndLbStreamClosure() {
    Metadata headers = new Metadata();
    PickSubchannelArgs args = mock(PickSubchannelArgs.class);
    when(args.getHeaders()).thenReturn(headers);

    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.EMPTY;
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
    assertEquals(0, fakeClock.numPendingTasks(LOAD_REPORTING_TASK_FILTER));
    lbResponseObserver.onNext(buildInitialResponse(1983));

    // Load reporting task is scheduled
    assertEquals(1, fakeClock.numPendingTasks(LOAD_REPORTING_TASK_FILTER));
    FakeClock.ScheduledTask scheduledTask = fakeClock.getPendingTasks().iterator().next();
    assertEquals(1983, scheduledTask.getDelay(TimeUnit.MILLISECONDS));

    // Close lbStream
    lbResponseObserver.onCompleted();

    // Reporting task cancelled
    assertEquals(0, fakeClock.numPendingTasks(LOAD_REPORTING_TASK_FILTER));

    // Simulate a race condition where the task has just started when its cancelled
    scheduledTask.command.run();

    // No report sent. No new task scheduled
    inOrder.verify(lbRequestObserver, never()).onNext(any(LoadBalanceRequest.class));
    assertEquals(0, fakeClock.numPendingTasks(LOAD_REPORTING_TASK_FILTER));
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
                .setTimestamp(Timestamps.fromNanos(fakeClock.getTicker().read()))
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
    verify(helper).updateBalancingState(eq(TRANSIENT_FAILURE), pickerCaptor.capture());
    RoundRobinPicker picker = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker.dropList).isEmpty();
    assertThat(picker.pickList).containsExactly(new ErrorEntry(error));

    // Recover with a subsequent success
    List<EquivalentAddressGroup> resolvedServers = createResolvedServerAddresses(false);

    Attributes resolutionAttrs = Attributes.newBuilder().set(RESOLUTION_ATTR, "yeah").build();
    deliverResolvedAddresses(resolvedServers, resolutionAttrs);
  }

  @Test
  public void nameResolutionFailsThenRecoverToGrpclb() {
    Status error = Status.NOT_FOUND.withDescription("www.google.com not found");
    deliverNameResolutionError(error);
    verify(helper).updateBalancingState(eq(TRANSIENT_FAILURE), pickerCaptor.capture());
    RoundRobinPicker picker = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker.dropList).isEmpty();
    assertThat(picker.pickList).containsExactly(new ErrorEntry(error));

    // Recover with a subsequent success
    List<EquivalentAddressGroup> resolvedServers = createResolvedServerAddresses(true);
    EquivalentAddressGroup eag = resolvedServers.get(0);

    Attributes resolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(resolvedServers, resolutionAttrs);

    verify(helper).createOobChannel(eq(eag), eq(lbAuthority(0)));
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
  }

  @Test
  public void grpclbThenNameResolutionFails() {
    InOrder inOrder = inOrder(helper, subchannelPool);
    // Go to GRPCLB first
    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    verify(helper).createOobChannel(eq(grpclbResolutionList.get(0)), eq(lbAuthority(0)));
    assertEquals(1, fakeOobChannels.size());
    ManagedChannel oobChannel = fakeOobChannels.poll();
    verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();

    // Let name resolution fail before round-robin list is ready
    Status error = Status.NOT_FOUND.withDescription("www.google.com not found");
    deliverNameResolutionError(error);

    inOrder.verify(helper).updateBalancingState(eq(TRANSIENT_FAILURE), pickerCaptor.capture());
    RoundRobinPicker picker = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker.dropList).isEmpty();
    assertThat(picker.pickList).containsExactly(new ErrorEntry(error));
    assertFalse(oobChannel.isShutdown());

    // Simulate receiving LB response
    List<ServerEntry> backends = Arrays.asList(
        new ServerEntry("127.0.0.1", 2000, "TOKEN1"),
        new ServerEntry("127.0.0.1", 2010, "TOKEN2"));
    verify(helper, never()).runSerialized(any(Runnable.class));
    lbResponseObserver.onNext(buildInitialResponse());
    lbResponseObserver.onNext(buildLbResponse(backends));

    verify(helper, times(2)).runSerialized(any(Runnable.class));
    inOrder.verify(subchannelPool).takeOrCreateSubchannel(
        eq(new EquivalentAddressGroup(backends.get(0).addr, LB_BACKEND_ATTRS)),
        any(Attributes.class));
    inOrder.verify(subchannelPool).takeOrCreateSubchannel(
        eq(new EquivalentAddressGroup(backends.get(1).addr, LB_BACKEND_ATTRS)),
        any(Attributes.class));
  }

  @Test
  public void grpclbUpdatedAddresses_avoidsReconnect() {
    List<EquivalentAddressGroup> grpclbResolutionList =
        createResolvedServerAddresses(true, false);
    Attributes grpclbResolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    verify(helper).createOobChannel(eq(grpclbResolutionList.get(0)), eq(lbAuthority(0)));
    ManagedChannel oobChannel = fakeOobChannels.poll();
    assertEquals(1, lbRequestObservers.size());

    List<EquivalentAddressGroup> grpclbResolutionList2 =
        createResolvedServerAddresses(true, false, true);
    EquivalentAddressGroup combinedEag = new EquivalentAddressGroup(Arrays.asList(
        grpclbResolutionList2.get(0).getAddresses().get(0),
        grpclbResolutionList2.get(2).getAddresses().get(0)),
        lbAttributes(lbAuthority(0)));
    deliverResolvedAddresses(grpclbResolutionList2, grpclbResolutionAttrs);
    verify(helper).updateOobChannelAddresses(eq(oobChannel), eq(combinedEag));
    assertEquals(1, lbRequestObservers.size()); // No additional RPC
  }

  @Test
  public void grpclbUpdatedAddresses_reconnectOnAuthorityChange() {
    List<EquivalentAddressGroup> grpclbResolutionList =
        createResolvedServerAddresses(true, false);
    Attributes grpclbResolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    verify(helper).createOobChannel(eq(grpclbResolutionList.get(0)), eq(lbAuthority(0)));
    ManagedChannel oobChannel = fakeOobChannels.poll();
    assertEquals(1, lbRequestObservers.size());

    final String newAuthority = "some-new-authority";
    List<EquivalentAddressGroup> grpclbResolutionList2 =
        createResolvedServerAddresses(false);
    grpclbResolutionList2.add(new EquivalentAddressGroup(
        new FakeSocketAddress("somethingNew"), lbAttributes(newAuthority)));
    deliverResolvedAddresses(grpclbResolutionList2, grpclbResolutionAttrs);
    assertTrue(oobChannel.isTerminated());
    verify(helper).createOobChannel(eq(grpclbResolutionList2.get(1)), eq(newAuthority));
    assertEquals(2, lbRequestObservers.size()); // An additional RPC
  }

  @Test
  public void grpclbWorking() {
    InOrder inOrder = inOrder(helper, subchannelPool);
    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    // Fallback timer is started as soon as the addresses are resolved.
    assertEquals(1, fakeClock.numPendingTasks(FALLBACK_MODE_TASK_FILTER));

    verify(helper).createOobChannel(eq(grpclbResolutionList.get(0)), eq(lbAuthority(0)));
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
    inOrder.verify(helper, never())
        .updateBalancingState(any(ConnectivityState.class), any(SubchannelPicker.class));
    lbResponseObserver.onNext(buildInitialResponse());
    lbResponseObserver.onNext(buildLbResponse(backends1));

    inOrder.verify(subchannelPool).takeOrCreateSubchannel(
        eq(new EquivalentAddressGroup(backends1.get(0).addr, LB_BACKEND_ATTRS)),
        any(Attributes.class));
    inOrder.verify(subchannelPool).takeOrCreateSubchannel(
        eq(new EquivalentAddressGroup(backends1.get(1).addr, LB_BACKEND_ATTRS)),
        any(Attributes.class));
    assertEquals(2, mockSubchannels.size());
    Subchannel subchannel1 = mockSubchannels.poll();
    Subchannel subchannel2 = mockSubchannels.poll();
    verify(subchannel1).requestConnection();
    verify(subchannel2).requestConnection();
    assertEquals(
        new EquivalentAddressGroup(backends1.get(0).addr, LB_BACKEND_ATTRS),
        subchannel1.getAddresses());
    assertEquals(
        new EquivalentAddressGroup(backends1.get(1).addr, LB_BACKEND_ATTRS),
        subchannel2.getAddresses());

    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(CONNECTING));
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(CONNECTING));
    inOrder.verify(helper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    RoundRobinPicker picker0 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker0.dropList).containsExactly(null, null);
    assertThat(picker0.pickList).containsExactly(BUFFER_ENTRY);
    inOrder.verifyNoMoreInteractions();

    // Let subchannels be connected
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(helper).updateBalancingState(eq(READY), pickerCaptor.capture());
    RoundRobinPicker picker1 = (RoundRobinPicker) pickerCaptor.getValue();

    assertThat(picker1.dropList).containsExactly(null, null);
    assertThat(picker1.pickList).containsExactly(
        new BackendEntry(subchannel2, getLoadRecorder(), "token0002"));

    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(helper).updateBalancingState(eq(READY), pickerCaptor.capture());
    RoundRobinPicker picker2 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker2.dropList).containsExactly(null, null);
    assertThat(picker2.pickList).containsExactly(
        new BackendEntry(subchannel1, getLoadRecorder(), "token0001"),
        new BackendEntry(subchannel2, getLoadRecorder(), "token0002"))
        .inOrder();

    // Disconnected subchannels
    verify(subchannel1).requestConnection();
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(IDLE));
    verify(subchannel1, times(2)).requestConnection();
    inOrder.verify(helper).updateBalancingState(eq(READY), pickerCaptor.capture());
    RoundRobinPicker picker3 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker3.dropList).containsExactly(null, null);
    assertThat(picker3.pickList).containsExactly(
        new BackendEntry(subchannel2, getLoadRecorder(), "token0002"));

    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(CONNECTING));
    inOrder.verifyNoMoreInteractions();

    // As long as there is at least one READY subchannel, round robin will work.
    Status error1 = Status.UNAVAILABLE.withDescription("error1");
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forTransientFailure(error1));
    inOrder.verifyNoMoreInteractions();

    // If no subchannel is READY, some with error and the others are IDLE, will report CONNECTING
    verify(subchannel2).requestConnection();
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(IDLE));
    verify(subchannel2, times(2)).requestConnection();
    inOrder.verify(helper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    RoundRobinPicker picker4 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker4.dropList).containsExactly(null, null);
    assertThat(picker4.pickList).containsExactly(BUFFER_ENTRY);

    // Update backends, with a drop entry
    List<ServerEntry> backends2 =
        Arrays.asList(
            new ServerEntry("127.0.0.1", 2030, "token0003"),  // New address
            new ServerEntry("token0003"),  // drop
            new ServerEntry("127.0.0.1", 2010, "token0004"),  // Existing address with token changed
            new ServerEntry("127.0.0.1", 2030, "token0005"),  // New address appearing second time
            new ServerEntry("token0006"));  // drop
    verify(subchannelPool, never()).returnSubchannel(same(subchannel1));

    lbResponseObserver.onNext(buildLbResponse(backends2));
    // not in backends2, closed
    verify(subchannelPool).returnSubchannel(same(subchannel1));
    // backends2[2], will be kept
    verify(subchannelPool, never()).returnSubchannel(same(subchannel2));

    inOrder.verify(subchannelPool, never()).takeOrCreateSubchannel(
        eq(new EquivalentAddressGroup(backends2.get(2).addr, LB_BACKEND_ATTRS)),
        any(Attributes.class));
    inOrder.verify(subchannelPool).takeOrCreateSubchannel(
        eq(new EquivalentAddressGroup(backends2.get(0).addr, LB_BACKEND_ATTRS)),
        any(Attributes.class));
    assertEquals(1, mockSubchannels.size());
    Subchannel subchannel3 = mockSubchannels.poll();
    verify(subchannel3).requestConnection();
    assertEquals(
        new EquivalentAddressGroup(backends2.get(0).addr, LB_BACKEND_ATTRS),
        subchannel3.getAddresses());
    inOrder.verify(helper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    RoundRobinPicker picker7 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker7.dropList).containsExactly(
        null,
        new DropEntry(getLoadRecorder(), "token0003"),
        null,
        null,
        new DropEntry(getLoadRecorder(), "token0006")).inOrder();
    assertThat(picker7.pickList).containsExactly(BUFFER_ENTRY);

    // State updates on obsolete subchannel1 will have no effect
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(READY));
    deliverSubchannelState(
        subchannel1, ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE));
    deliverSubchannelState(subchannel1, ConnectivityStateInfo.forNonError(SHUTDOWN));
    inOrder.verifyNoMoreInteractions();

    deliverSubchannelState(subchannel3, ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(helper).updateBalancingState(eq(READY), pickerCaptor.capture());
    RoundRobinPicker picker8 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker8.dropList).containsExactly(
        null,
        new DropEntry(getLoadRecorder(), "token0003"),
        null,
        null,
        new DropEntry(getLoadRecorder(), "token0006")).inOrder();
    // subchannel2 is still IDLE, thus not in the active list
    assertThat(picker8.pickList).containsExactly(
        new BackendEntry(subchannel3, getLoadRecorder(), "token0003"),
        new BackendEntry(subchannel3, getLoadRecorder(), "token0005")).inOrder();
    // subchannel2 becomes READY and makes it into the list
    deliverSubchannelState(subchannel2, ConnectivityStateInfo.forNonError(READY));
    inOrder.verify(helper).updateBalancingState(eq(READY), pickerCaptor.capture());
    RoundRobinPicker picker9 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker9.dropList).containsExactly(
        null,
        new DropEntry(getLoadRecorder(), "token0003"),
        null,
        null,
        new DropEntry(getLoadRecorder(), "token0006")).inOrder();
    assertThat(picker9.pickList).containsExactly(
        new BackendEntry(subchannel3, getLoadRecorder(), "token0003"),
        new BackendEntry(subchannel2, getLoadRecorder(), "token0004"),
        new BackendEntry(subchannel3, getLoadRecorder(), "token0005")).inOrder();
    verify(subchannelPool, never()).returnSubchannel(same(subchannel3));

    // Update backends, with no entry
    lbResponseObserver.onNext(buildLbResponse(Collections.<ServerEntry>emptyList()));
    verify(subchannelPool).returnSubchannel(same(subchannel2));
    verify(subchannelPool).returnSubchannel(same(subchannel3));
    inOrder.verify(helper).updateBalancingState(eq(CONNECTING), pickerCaptor.capture());
    RoundRobinPicker picker10 = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker10.dropList).isEmpty();
    assertThat(picker10.pickList).containsExactly(BUFFER_ENTRY);

    assertFalse(oobChannel.isShutdown());
    assertEquals(0, lbRequestObservers.size());
    verify(lbRequestObserver, never()).onCompleted();
    verify(lbRequestObserver, never()).onError(any(Throwable.class));

    // Load reporting was not requested, thus never scheduled
    assertEquals(0, fakeClock.numPendingTasks(LOAD_REPORTING_TASK_FILTER));

    verify(subchannelPool, never()).clear();
    balancer.shutdown();
    verify(subchannelPool).clear();
  }

  @Test
  public void grpclbFallback_initialTimeout_serverListReceivedBeforeTimerExpires() {
    subtestGrpclbFallbackInitialTimeout(false);
  }

  @Test
  public void grpclbFallback_initialTimeout_timerExpires() {
    subtestGrpclbFallbackInitialTimeout(true);
  }

  // Fallback or not within the period of the initial timeout.
  private void subtestGrpclbFallbackInitialTimeout(boolean timerExpires) {
    long loadReportIntervalMillis = 1983;
    InOrder inOrder = inOrder(helper, subchannelPool);

    // Create a resolution list with a mixture of balancer and backend addresses
    List<EquivalentAddressGroup> resolutionList =
        createResolvedServerAddresses(false, true, false);
    Attributes resolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(resolutionList, resolutionAttrs);

    inOrder.verify(helper).createOobChannel(eq(resolutionList.get(1)), eq(lbAuthority(0)));

    // Attempted to connect to balancer
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
    lbResponseObserver.onNext(buildInitialResponse(loadReportIntervalMillis));
    // We don't care if runSerialized() has been run.
    inOrder.verify(helper, atLeast(0)).runSerialized(any(Runnable.class));
    inOrder.verifyNoMoreInteractions();

    assertEquals(1, fakeClock.numPendingTasks(FALLBACK_MODE_TASK_FILTER));
    fakeClock.forwardTime(GrpclbState.FALLBACK_TIMEOUT_MS - 1, TimeUnit.MILLISECONDS);
    assertEquals(1, fakeClock.numPendingTasks(FALLBACK_MODE_TASK_FILTER));

    /////////////////////////////////////////////
    // Break the LB stream before timer expires
    /////////////////////////////////////////////
    Status streamError = Status.UNAVAILABLE.withDescription("OOB stream broken");
    lbResponseObserver.onError(streamError.asException());
    // Not in fallback mode. The error will be propagated.
    verify(helper).updateBalancingState(eq(TRANSIENT_FAILURE), pickerCaptor.capture());
    RoundRobinPicker picker = (RoundRobinPicker) pickerCaptor.getValue();
    assertThat(picker.dropList).isEmpty();
    ErrorEntry errorEntry = (ErrorEntry) Iterables.getOnlyElement(picker.pickList);
    Status status = errorEntry.result.getStatus();
    assertThat(status.getCode()).isEqualTo(streamError.getCode());
    assertThat(status.getDescription()).contains(streamError.getDescription());
    // A new stream is created
    verify(mockLbService, times(2)).balanceLoad(lbResponseObserverCaptor.capture());
    lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    lbRequestObserver = lbRequestObservers.poll();
    verify(lbRequestObserver).onNext(
        eq(LoadBalanceRequest.newBuilder().setInitialRequest(
                InitialLoadBalanceRequest.newBuilder().setName(SERVICE_AUTHORITY).build())
            .build()));

    //////////////////////////////////
    // Fallback timer expires (or not)
    //////////////////////////////////
    if (timerExpires) {
      fakeClock.forwardTime(1, TimeUnit.MILLISECONDS);
      assertEquals(0, fakeClock.numPendingTasks(FALLBACK_MODE_TASK_FILTER));
      // Fall back to the backends from resolver
      fallbackTestVerifyUseOfFallbackBackendLists(
          inOrder, Arrays.asList(resolutionList.get(0), resolutionList.get(2)));

      assertFalse(oobChannel.isShutdown());
      verify(lbRequestObserver, never()).onCompleted();
    }

    ////////////////////////////////////////////////////////
    // Name resolver sends new list without any backend addr
    ////////////////////////////////////////////////////////
    resolutionList = createResolvedServerAddresses(true, true);
    deliverResolvedAddresses(resolutionList, resolutionAttrs);

    // New addresses are updated to the OobChannel
    inOrder.verify(helper).updateOobChannelAddresses(
        same(oobChannel),
        eq(new EquivalentAddressGroup(
                Arrays.asList(
                    resolutionList.get(0).getAddresses().get(0),
                    resolutionList.get(1).getAddresses().get(0)),
                lbAttributes(lbAuthority(0)))));

    if (timerExpires) {
      // Still in fallback logic, except that the backend list is empty
      fallbackTestVerifyUseOfFallbackBackendLists(
          inOrder, Collections.<EquivalentAddressGroup>emptyList());
    }

    //////////////////////////////////////////////////
    // Name resolver sends new list with backend addrs
    //////////////////////////////////////////////////
    resolutionList = createResolvedServerAddresses(true, false, false);
    deliverResolvedAddresses(resolutionList, resolutionAttrs);

    // New LB address is updated to the OobChannel
    inOrder.verify(helper).updateOobChannelAddresses(
        same(oobChannel),
        eq(resolutionList.get(0)));

    if (timerExpires) {
      // New backend addresses are used for fallback
      fallbackTestVerifyUseOfFallbackBackendLists(
          inOrder, Arrays.asList(resolutionList.get(1), resolutionList.get(2)));
    }

    ////////////////////////////////////////////////
    // Break the LB stream after the timer expires
    ////////////////////////////////////////////////
    if (timerExpires) {
      lbResponseObserver.onError(streamError.asException());

      // The error will NOT propagate to picker because fallback list is in use.
      inOrder.verify(helper, never())
          .updateBalancingState(any(ConnectivityState.class), any(SubchannelPicker.class));
      // A new stream is created
      verify(mockLbService, times(3)).balanceLoad(lbResponseObserverCaptor.capture());
      lbResponseObserver = lbResponseObserverCaptor.getValue();
      assertEquals(1, lbRequestObservers.size());
      lbRequestObserver = lbRequestObservers.poll();
      verify(lbRequestObserver).onNext(
          eq(LoadBalanceRequest.newBuilder().setInitialRequest(
                  InitialLoadBalanceRequest.newBuilder().setName(SERVICE_AUTHORITY).build())
              .build()));
    }

    /////////////////////////////////
    // Balancer returns a server list
    /////////////////////////////////
    List<ServerEntry> serverList = Arrays.asList(
        new ServerEntry("127.0.0.1", 2000, "token0001"),
        new ServerEntry("127.0.0.1", 2010, "token0002"));
    lbResponseObserver.onNext(buildInitialResponse());
    lbResponseObserver.onNext(buildLbResponse(serverList));

    // Balancer-provided server list now in effect
    fallbackTestVerifyUseOfBalancerBackendLists(inOrder, serverList);

    ///////////////////////////////////////////////////////////////
    // New backend addresses from resolver outside of fallback mode
    ///////////////////////////////////////////////////////////////
    resolutionList = createResolvedServerAddresses(true, false);
    deliverResolvedAddresses(resolutionList, resolutionAttrs);
    // Will not affect the round robin list at all
    inOrder.verify(helper, never())
        .updateBalancingState(any(ConnectivityState.class), any(SubchannelPicker.class));

    // No fallback timeout timer scheduled.
    assertEquals(0, fakeClock.numPendingTasks(FALLBACK_MODE_TASK_FILTER));
  }

  @Test
  public void grpclbFallback_balancerLost() {
    subtestGrpclbFallbackConnectionLost(true, false);
  }

  @Test
  public void grpclbFallback_subchannelsLost() {
    subtestGrpclbFallbackConnectionLost(false, true);
  }

  @Test
  public void grpclbFallback_allLost() {
    subtestGrpclbFallbackConnectionLost(true, true);
  }

  // Fallback outside of the initial timeout, where all connections are lost.
  private void subtestGrpclbFallbackConnectionLost(
      boolean balancerBroken, boolean allSubchannelsBroken) {
    long loadReportIntervalMillis = 1983;
    InOrder inOrder = inOrder(helper, mockLbService, subchannelPool);

    // Create a resolution list with a mixture of balancer and backend addresses
    List<EquivalentAddressGroup> resolutionList =
        createResolvedServerAddresses(false, true, false);
    Attributes resolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(resolutionList, resolutionAttrs);

    inOrder.verify(helper).createOobChannel(eq(resolutionList.get(1)), eq(lbAuthority(0)));

    // Attempted to connect to balancer
    assertEquals(1, fakeOobChannels.size());
    fakeOobChannels.poll();
    inOrder.verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    StreamObserver<LoadBalanceRequest> lbRequestObserver = lbRequestObservers.poll();

    verify(lbRequestObserver).onNext(
        eq(LoadBalanceRequest.newBuilder().setInitialRequest(
                InitialLoadBalanceRequest.newBuilder().setName(SERVICE_AUTHORITY).build())
            .build()));
    lbResponseObserver.onNext(buildInitialResponse(loadReportIntervalMillis));
    // We don't care if runSerialized() has been run.
    inOrder.verify(helper, atLeast(0)).runSerialized(any(Runnable.class));
    inOrder.verifyNoMoreInteractions();

    // Balancer returns a server list
    List<ServerEntry> serverList = Arrays.asList(
        new ServerEntry("127.0.0.1", 2000, "token0001"),
        new ServerEntry("127.0.0.1", 2010, "token0002"));
    lbResponseObserver.onNext(buildInitialResponse());
    lbResponseObserver.onNext(buildLbResponse(serverList));

    List<Subchannel> subchannels = fallbackTestVerifyUseOfBalancerBackendLists(inOrder, serverList);

    // Break connections
    if (balancerBroken) {
      lbResponseObserver.onError(Status.UNAVAILABLE.asException());
      // A new stream to LB is created
      inOrder.verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
      lbResponseObserver = lbResponseObserverCaptor.getValue();
      assertEquals(1, lbRequestObservers.size());
      lbRequestObserver = lbRequestObservers.poll();
    }
    if (allSubchannelsBroken) {
      for (Subchannel subchannel : subchannels) {
        // A READY subchannel transits to IDLE when receiving a go-away
        deliverSubchannelState(subchannel, ConnectivityStateInfo.forNonError(IDLE));
      }
    }

    if (balancerBroken && allSubchannelsBroken) {
      // Going into fallback
      subchannels = fallbackTestVerifyUseOfFallbackBackendLists(
          inOrder, Arrays.asList(resolutionList.get(0), resolutionList.get(2)));

      // When in fallback mode, fallback timer should not be scheduled when all backend
      // connections are lost
      for (Subchannel subchannel : subchannels) {
        deliverSubchannelState(subchannel, ConnectivityStateInfo.forNonError(IDLE));
      }

      // Exit fallback mode or cancel fallback timer when receiving a new server list from balancer
      List<ServerEntry> serverList2 = Arrays.asList(
          new ServerEntry("127.0.0.1", 2001, "token0003"),
          new ServerEntry("127.0.0.1", 2011, "token0004"));
      lbResponseObserver.onNext(buildInitialResponse());
      lbResponseObserver.onNext(buildLbResponse(serverList2));

      fallbackTestVerifyUseOfBalancerBackendLists(inOrder, serverList2);
    }
    assertEquals(0, fakeClock.numPendingTasks(FALLBACK_MODE_TASK_FILTER));

    if (!(balancerBroken && allSubchannelsBroken)) {
      verify(subchannelPool, never()).takeOrCreateSubchannel(
          eq(resolutionList.get(0)), any(Attributes.class));
      verify(subchannelPool, never()).takeOrCreateSubchannel(
          eq(resolutionList.get(2)), any(Attributes.class));
    }
  }

  private List<Subchannel> fallbackTestVerifyUseOfFallbackBackendLists(
      InOrder inOrder, List<EquivalentAddressGroup> addrs) {
    return fallbackTestVerifyUseOfBackendLists(inOrder, addrs, null);
  }

  private List<Subchannel> fallbackTestVerifyUseOfBalancerBackendLists(
      InOrder inOrder, List<ServerEntry> servers) {
    ArrayList<EquivalentAddressGroup> addrs = new ArrayList<>();
    ArrayList<String> tokens = new ArrayList<>();
    for (ServerEntry server : servers) {
      addrs.add(new EquivalentAddressGroup(server.addr, LB_BACKEND_ATTRS));
      tokens.add(server.token);
    }
    return fallbackTestVerifyUseOfBackendLists(inOrder, addrs, tokens);
  }

  private List<Subchannel> fallbackTestVerifyUseOfBackendLists(
      InOrder inOrder, List<EquivalentAddressGroup> addrs,
      @Nullable List<String> tokens) {
    if (tokens != null) {
      assertEquals(addrs.size(), tokens.size());
    }
    for (EquivalentAddressGroup addr : addrs) {
      inOrder.verify(subchannelPool).takeOrCreateSubchannel(eq(addr), any(Attributes.class));
    }
    RoundRobinPicker picker = (RoundRobinPicker) currentPicker;
    assertThat(picker.dropList).containsExactlyElementsIn(Collections.nCopies(addrs.size(), null));
    assertThat(picker.pickList).containsExactly(GrpclbState.BUFFER_ENTRY);
    assertEquals(addrs.size(), mockSubchannels.size());
    ArrayList<Subchannel> subchannels = new ArrayList<>(mockSubchannels);
    mockSubchannels.clear();
    for (Subchannel subchannel : subchannels) {
      deliverSubchannelState(subchannel, ConnectivityStateInfo.forNonError(CONNECTING));
    }
    inOrder.verify(helper, atLeast(0))
        .updateBalancingState(eq(CONNECTING), any(SubchannelPicker.class));
    inOrder.verify(helper, never())
        .updateBalancingState(any(ConnectivityState.class), any(SubchannelPicker.class));

    ArrayList<BackendEntry> pickList = new ArrayList<>();
    for (int i = 0; i < addrs.size(); i++) {
      Subchannel subchannel = subchannels.get(i);
      BackendEntry backend;
      if (tokens == null) {
        backend = new BackendEntry(subchannel);
      } else {
        backend = new BackendEntry(subchannel, getLoadRecorder(), tokens.get(i));
      }
      pickList.add(backend);
      deliverSubchannelState(subchannel, ConnectivityStateInfo.forNonError(READY));
      inOrder.verify(helper).updateBalancingState(eq(READY), pickerCaptor.capture());
      picker = (RoundRobinPicker) pickerCaptor.getValue();
      assertThat(picker.dropList)
          .containsExactlyElementsIn(Collections.nCopies(addrs.size(), null));
      assertThat(picker.pickList).containsExactlyElementsIn(pickList);
      inOrder.verify(helper, never())
          .updateBalancingState(any(ConnectivityState.class), any(SubchannelPicker.class));
    }
    return subchannels;
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
            new FakeSocketAddress("fake-address-3")),
        lbAttributes("fake-authority-1")); // Supporting multiple authorities would be good, one day

    Attributes grpclbResolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    verify(helper).createOobChannel(goldenOobChannelEag, "fake-authority-1");
  }

  @Test
  public void grpclbBalancerStreamRetry() throws Exception {
    LoadBalanceRequest expectedInitialRequest =
        LoadBalanceRequest.newBuilder()
            .setInitialRequest(
                InitialLoadBalanceRequest.newBuilder().setName(SERVICE_AUTHORITY).build())
            .build();
    InOrder inOrder =
        inOrder(mockLbService, backoffPolicyProvider, backoffPolicy1, backoffPolicy2);
    List<EquivalentAddressGroup> grpclbResolutionList = createResolvedServerAddresses(true);
    Attributes grpclbResolutionAttrs = Attributes.EMPTY;
    deliverResolvedAddresses(grpclbResolutionList, grpclbResolutionAttrs);

    assertEquals(1, fakeOobChannels.size());
    @SuppressWarnings("unused")
    ManagedChannel oobChannel = fakeOobChannels.poll();

    // First balancer RPC
    inOrder.verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    StreamObserver<LoadBalanceResponse> lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    StreamObserver<LoadBalanceRequest> lbRequestObserver = lbRequestObservers.poll();
    verify(lbRequestObserver).onNext(eq(expectedInitialRequest));
    assertEquals(0, fakeClock.numPendingTasks(LB_RPC_RETRY_TASK_FILTER));

    // Balancer closes it immediately (erroneously)
    lbResponseObserver.onCompleted();
    // Will start backoff sequence 1 (10ns)
    inOrder.verify(backoffPolicyProvider).get();
    inOrder.verify(backoffPolicy1).nextBackoffNanos();
    assertEquals(1, fakeClock.numPendingTasks(LB_RPC_RETRY_TASK_FILTER));

    // Fast-forward to a moment before the retry
    fakeClock.forwardNanos(9);
    verifyNoMoreInteractions(mockLbService);
    // Then time for retry
    fakeClock.forwardNanos(1);
    inOrder.verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    lbRequestObserver = lbRequestObservers.poll();
    verify(lbRequestObserver).onNext(eq(expectedInitialRequest));
    assertEquals(0, fakeClock.numPendingTasks(LB_RPC_RETRY_TASK_FILTER));

    // Balancer closes it with an error.
    lbResponseObserver.onError(Status.UNAVAILABLE.asException());
    // Will continue the backoff sequence 1 (100ns)
    verifyNoMoreInteractions(backoffPolicyProvider);
    inOrder.verify(backoffPolicy1).nextBackoffNanos();
    assertEquals(1, fakeClock.numPendingTasks(LB_RPC_RETRY_TASK_FILTER));

    // Fast-forward to a moment before the retry
    fakeClock.forwardNanos(100 - 1);
    verifyNoMoreInteractions(mockLbService);
    // Then time for retry
    fakeClock.forwardNanos(1);
    inOrder.verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    lbRequestObserver = lbRequestObservers.poll();
    verify(lbRequestObserver).onNext(eq(expectedInitialRequest));
    assertEquals(0, fakeClock.numPendingTasks(LB_RPC_RETRY_TASK_FILTER));

    // Balancer sends initial response.
    lbResponseObserver.onNext(buildInitialResponse());

    // Then breaks the RPC
    lbResponseObserver.onError(Status.UNAVAILABLE.asException());

    // Will reset the retry sequence and retry immediately, because balancer has responded.
    inOrder.verify(backoffPolicyProvider).get();
    inOrder.verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    lbResponseObserver = lbResponseObserverCaptor.getValue();
    assertEquals(1, lbRequestObservers.size());
    lbRequestObserver = lbRequestObservers.poll();
    verify(lbRequestObserver).onNext(eq(expectedInitialRequest));

    // Fail the retry after spending 4ns
    fakeClock.forwardNanos(4);
    lbResponseObserver.onError(Status.UNAVAILABLE.asException());
    
    // Will be on the first retry (10ns) of backoff sequence 2.
    inOrder.verify(backoffPolicy2).nextBackoffNanos();
    assertEquals(1, fakeClock.numPendingTasks(LB_RPC_RETRY_TASK_FILTER));

    // Fast-forward to a moment before the retry, the time spent in the last try is deducted.
    fakeClock.forwardNanos(10 - 4 - 1);
    verifyNoMoreInteractions(mockLbService);
    // Then time for retry
    fakeClock.forwardNanos(1);
    inOrder.verify(mockLbService).balanceLoad(lbResponseObserverCaptor.capture());
    assertEquals(1, lbRequestObservers.size());
    lbRequestObserver = lbRequestObservers.poll();
    verify(lbRequestObserver).onNext(eq(expectedInitialRequest));
    assertEquals(0, fakeClock.numPendingTasks(LB_RPC_RETRY_TASK_FILTER));

    // Wrapping up
    verify(backoffPolicyProvider, times(2)).get();
    verify(backoffPolicy1, times(2)).nextBackoffNanos();
    verify(backoffPolicy2, times(1)).nextBackoffNanos();
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

  private GrpclbClientLoadRecorder getLoadRecorder() {
    return balancer.getGrpclbState().getLoadRecorder();
  }

  private static List<EquivalentAddressGroup> createResolvedServerAddresses(boolean ... isLb) {
    ArrayList<EquivalentAddressGroup> list = new ArrayList<>();
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
        .set(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY, authority)
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

  private static LoadBalanceResponse buildLbResponse(List<ServerEntry> servers) {
    ServerList.Builder serverListBuilder = ServerList.newBuilder();
    for (ServerEntry server : servers) {
      if (server.addr != null) {
        serverListBuilder.addServers(Server.newBuilder()
            .setIpAddress(ByteString.copyFrom(server.addr.getAddress().getAddress()))
            .setPort(server.addr.getPort())
            .setLoadBalanceToken(server.token)
            .build());
      } else {
        serverListBuilder.addServers(Server.newBuilder()
            .setDrop(true)
            .setLoadBalanceToken(server.token)
            .build());
      }
    }
    return LoadBalanceResponse.newBuilder()
        .setServerList(serverListBuilder.build())
        .build();
  }

  private static class ServerEntry {
    final InetSocketAddress addr;
    final String token;

    ServerEntry(String host, int port, String token) {
      this.addr = new InetSocketAddress(host, port);
      this.token = token;
    }

    // Drop entry
    ServerEntry(String token) {
      this.addr = null;
      this.token = token;
    }
  }
}
