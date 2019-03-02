/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.xds;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.LoadBalancer.ATTR_LOAD_BALANCING_CONFIG;
import static io.grpc.xds.XdsLoadBalancer.STATE_INFO;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.anyString;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import io.envoyproxy.envoy.api.v2.DiscoveryRequest;
import io.envoyproxy.envoy.api.v2.DiscoveryResponse;
import io.envoyproxy.envoy.service.discovery.v2.AggregatedDiscoveryServiceGrpc.AggregatedDiscoveryServiceImplBase;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.ChannelLogger;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;
import io.grpc.ManagedChannel;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.SynchronizationContext;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.FakeClock;
import io.grpc.internal.JsonParser;
import io.grpc.internal.ServiceConfigUtil;
import io.grpc.internal.ServiceConfigUtil.LbConfig;
import io.grpc.internal.testing.StreamRecorder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.GrpcCleanupRule;
import io.grpc.xds.XdsLbState.SubchannelStore;
import io.grpc.xds.XdsLbState.SubchannelStoreImpl;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link XdsLoadBalancer}.
 */
@RunWith(JUnit4.class)
public class XdsLoadBalancerTest {
  @Rule
  public final GrpcCleanupRule cleanupRule = new GrpcCleanupRule();
  @Mock
  private Helper helper;
  @Mock
  private LoadBalancer fakeBalancer1;
  @Mock
  private LoadBalancer fakeBalancer2;
  private XdsLoadBalancer lb;

  private final FakeClock fakeClock = new FakeClock();
  private final StreamRecorder<DiscoveryRequest> streamRecorder = StreamRecorder.create();

  private final LoadBalancerRegistry lbRegistry = new LoadBalancerRegistry();

  private final LoadBalancerProvider lbProvider1 = new LoadBalancerProvider() {
    @Override
    public boolean isAvailable() {
      return true;
    }

    @Override
    public int getPriority() {
      return 5;
    }

    @Override
    public String getPolicyName() {
      return "supported_1";
    }

    @Override
    public LoadBalancer newLoadBalancer(Helper helper) {
      return fakeBalancer1;
    }
  };

  private final LoadBalancerProvider lbProvider2 = new LoadBalancerProvider() {
    @Override
    public boolean isAvailable() {
      return true;
    }

    @Override
    public int getPriority() {
      return 5;
    }

    @Override
    public String getPolicyName() {
      return "supported_2";
    }

    @Override
    public LoadBalancer newLoadBalancer(Helper helper) {
      return fakeBalancer2;
    }
  };

  private final LoadBalancerProvider roundRobin = new LoadBalancerProvider() {
    @Override
    public boolean isAvailable() {
      return true;
    }

    @Override
    public int getPriority() {
      return 5;
    }

    @Override
    public String getPolicyName() {
      return "round_robin";
    }

    @Override
    public LoadBalancer newLoadBalancer(Helper helper) {
      return null;
    }
  };

  private final SynchronizationContext syncContext = new SynchronizationContext(
      new Thread.UncaughtExceptionHandler() {
        @Override
        public void uncaughtException(Thread t, Throwable e) {
          throw new AssertionError(e);
        }
      });

  private final SubchannelStore fakeSubchannelStore =
      mock(SubchannelStore.class, delegatesTo(new SubchannelStoreImpl()));

  private ManagedChannel oobChannel1;
  private ManagedChannel oobChannel2;
  private ManagedChannel oobChannel3;

  private StreamObserver<DiscoveryResponse> serverResponseWriter;

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    lbRegistry.register(lbProvider1);
    lbRegistry.register(lbProvider2);
    lbRegistry.register(roundRobin);
    lb = new XdsLoadBalancer(helper, lbRegistry, fakeSubchannelStore);
    doReturn(syncContext).when(helper).getSynchronizationContext();
    doReturn(fakeClock.getScheduledExecutorService()).when(helper).getScheduledExecutorService();
    doReturn(mock(ChannelLogger.class)).when(helper).getChannelLogger();

    String serverName = InProcessServerBuilder.generateName();

    AggregatedDiscoveryServiceImplBase serviceImpl = new AggregatedDiscoveryServiceImplBase() {
      @Override
      public StreamObserver<DiscoveryRequest> streamAggregatedResources(
          final StreamObserver<DiscoveryResponse> responseObserver) {
        serverResponseWriter = responseObserver;

        return new StreamObserver<DiscoveryRequest>() {

          @Override
          public void onNext(DiscoveryRequest value) {
            streamRecorder.onNext(value);
          }

          @Override
          public void onError(Throwable t) {
            streamRecorder.onError(t);
          }

          @Override
          public void onCompleted() {
            streamRecorder.onCompleted();
            responseObserver.onCompleted();
          }
        };
      }
    };

    cleanupRule.register(
        InProcessServerBuilder
            .forName(serverName)
            .directExecutor()
            .addService(serviceImpl)
            .build()
            .start());

    InProcessChannelBuilder channelBuilder =
        InProcessChannelBuilder.forName(serverName).directExecutor();
    oobChannel1 = mock(
        ManagedChannel.class,
        delegatesTo(cleanupRule.register(channelBuilder.build())));
    oobChannel2 = mock(
        ManagedChannel.class,
        delegatesTo(cleanupRule.register(channelBuilder.build())));
    oobChannel3 = mock(
        ManagedChannel.class,
        delegatesTo(cleanupRule.register(channelBuilder.build())));

    doReturn(oobChannel1).doReturn(oobChannel2).doReturn(oobChannel3)
      .when(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
  }

  @After
  public void tearDown() {
    lb.shutdown();
  }

  @Test
  public void selectChildPolicy() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported_1\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}},"
        + "{\"supported_2\" : {\"key\" : \"val\"}}],"
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    LbConfig expectedChildPolicy =
        ServiceConfigUtil.unwrapLoadBalancingConfig(
            JsonParser.parse("{\"supported_1\" : {\"key\" : \"val\"}}"));

    LbConfig childPolicy = XdsLoadBalancer
        .selectChildPolicy(
            ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfigRaw)), lbRegistry);

    assertEquals(expectedChildPolicy, childPolicy);
  }

  @Test
  public void selectFallBackPolicy() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}},"
        + "{\"supported_2\" : {\"key\" : \"val\"}}]"
        + "}}";
    LbConfig expectedFallbackPolicy = ServiceConfigUtil.unwrapLoadBalancingConfig(
        JsonParser.parse("{\"supported_1\" : {\"key\" : \"val\"}}"));

    LbConfig fallbackPolicy = XdsLoadBalancer.selectFallbackPolicy(
        ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfigRaw)), lbRegistry);

    assertEquals(expectedFallbackPolicy, fallbackPolicy);
  }

  @Test
  public void selectFallBackPolicy_roundRobinIsDefault() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    LbConfig expectedFallbackPolicy = ServiceConfigUtil.unwrapLoadBalancingConfig(
        JsonParser.parse("{\"round_robin\" : {}}"));

    LbConfig fallbackPolicy = XdsLoadBalancer.selectFallbackPolicy(
        ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfigRaw)), lbRegistry);

    assertEquals(expectedFallbackPolicy, fallbackPolicy);
  }

  @Test
  public void canHandleEmptyAddressListFromNameResolution() {
    assertTrue(lb.canHandleEmptyAddressListFromNameResolution());
  }

  @Test
  public void resolverEvent_standardModeToStandardMode() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    XdsLbState xdsLbState1 = lb.getXdsLbStateForTest();
    assertThat(xdsLbState1.childPolicy).isNull();
    verify(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    verify(oobChannel1)
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());


    lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig2 = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig2).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    XdsLbState xdsLbState2 = lb.getXdsLbStateForTest();
    assertThat(xdsLbState2.childPolicy).isNull();
    assertThat(xdsLbState2).isSameAs(xdsLbState1);

    // verify oobChannel is unchanged
    verify(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    // verify ADS stream is unchanged
    verify(oobChannel1)
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());
  }

  @Test
  public void resolverEvent_standardModeToCustomMode() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);
    verify(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    verify(oobChannel1)
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());

    lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"supported_1\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig2 = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig2).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbStateForTest().childPolicy).isNotNull();

    // verify oobChannel is unchanged
    verify(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    // verify ADS stream is reset
    verify(oobChannel1, times(2))
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());
  }

  @Test
  public void resolverEvent_customModeToStandardMode() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"supported_1\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);
    verify(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    verify(oobChannel1)
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());

    assertThat(lb.getXdsLbStateForTest().childPolicy).isNotNull();

    lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig2 = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig2).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbStateForTest().childPolicy).isNull();

    // verify oobChannel is unchanged
    verify(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    // verify ADS stream is reset
    verify(oobChannel1, times(2))
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());
  }

  @Test
  public void resolverEvent_customModeToCustomMode() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"supported_1\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbStateForTest().childPolicy).isNotNull();
    verify(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    verify(oobChannel1)
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());

    lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"supported_2\" : {\"key\" : \"val\"}}, {\"unsupported_1\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig2 = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig2).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbStateForTest().childPolicy).isNotNull();
    // verify oobChannel is unchanged
    verify(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    // verify ADS stream is reset
    verify(oobChannel1, times(2))
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());
  }

  @Test
  public void resolverEvent_balancerNameChange() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);
    verify(helper).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    verify(oobChannel1)
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());

    lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8443\","
        + "\"childPolicy\" : [{\"supported_1\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig2 = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig2).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbStateForTest().childPolicy).isNotNull();

    // verify oobChannel is unchanged
    verify(helper, times(2)).createOobChannel(Matchers.<EquivalentAddressGroup>any(), anyString());
    verify(oobChannel1)
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());
    verify(oobChannel2)
        .newCall(Matchers.<MethodDescriptor<?, ?>>any(), Matchers.<CallOptions>any());
    verifyNoMoreInteractions(oobChannel3);
  }

  @Test
  public void fallback_AdsNotWorkingYetTimerExpired() throws Exception {
    lb.handleResolvedAddressGroups(
        Collections.<EquivalentAddressGroup>emptyList(), standardModeWithFallback1Attributes());

    assertThat(fakeClock.forwardTime(10, TimeUnit.SECONDS)).isEqualTo(1);
    assertThat(fakeClock.getPendingTasks()).isEmpty();

    ArgumentCaptor<Attributes> captor = ArgumentCaptor.forClass(Attributes.class);
    verify(fakeBalancer1).handleResolvedAddressGroups(
        Matchers.<List<EquivalentAddressGroup>>any(), captor.capture());
    assertThat(captor.getValue().get(ATTR_LOAD_BALANCING_CONFIG))
        .containsExactly("supported_1_option", "yes");
  }

  @Test
  public void fallback_AdsWorkingTimerCancelled() throws Exception {
    lb.handleResolvedAddressGroups(
        Collections.<EquivalentAddressGroup>emptyList(), standardModeWithFallback1Attributes());

    serverResponseWriter.onNext(DiscoveryResponse.getDefaultInstance());

    assertThat(fakeClock.getPendingTasks()).isEmpty();
    verify(fakeBalancer1, never()).handleResolvedAddressGroups(
        Matchers.<List<EquivalentAddressGroup>>any(), Matchers.<Attributes>any());
  }

  @Test
  public void fallback_AdsErrorAndNoActiveSubchannel() throws Exception {
    lb.handleResolvedAddressGroups(
        Collections.<EquivalentAddressGroup>emptyList(), standardModeWithFallback1Attributes());

    serverResponseWriter.onError(new Exception("fake error"));

    ArgumentCaptor<Attributes> captor = ArgumentCaptor.forClass(Attributes.class);
    verify(fakeBalancer1).handleResolvedAddressGroups(
        Matchers.<List<EquivalentAddressGroup>>any(), captor.capture());
    assertThat(captor.getValue().get(ATTR_LOAD_BALANCING_CONFIG))
        .containsExactly("supported_1_option", "yes");

    assertThat(fakeClock.forwardTime(10, TimeUnit.SECONDS)).isEqualTo(1);
    assertThat(fakeClock.getPendingTasks()).isEmpty();

    // verify handleResolvedAddressGroups() is not called again
    verify(fakeBalancer1).handleResolvedAddressGroups(
        Matchers.<List<EquivalentAddressGroup>>any(), Matchers.<Attributes>any());
  }

  @Test
  public void fallback_AdsErrorWithActiveSubchannel() throws Exception {
    lb.handleResolvedAddressGroups(
        Collections.<EquivalentAddressGroup>emptyList(), standardModeWithFallback1Attributes());

    serverResponseWriter.onNext(DiscoveryResponse.getDefaultInstance());
    doReturn(true).when(fakeSubchannelStore).hasReadyBackends();
    serverResponseWriter.onError(new Exception("fake error"));

    verify(fakeBalancer1, never()).handleResolvedAddressGroups(
        Matchers.<List<EquivalentAddressGroup>>any(), Matchers.<Attributes>any());

    Subchannel subchannel = new Subchannel() {
      @Override
      public void shutdown() {}

      @Override
      public void requestConnection() {}

      @Override
      public Attributes getAttributes() {
        return Attributes.newBuilder()
            .set(
                STATE_INFO,
                new AtomicReference<>(ConnectivityStateInfo.forNonError(ConnectivityState.READY)))
            .build();
      }
    };

    doReturn(true).when(fakeSubchannelStore).hasSubchannel(subchannel);
    doReturn(false).when(fakeSubchannelStore).hasReadyBackends();
    lb.handleSubchannelState(subchannel, ConnectivityStateInfo.forTransientFailure(
        Status.UNAVAILABLE));

    ArgumentCaptor<Attributes> captor = ArgumentCaptor.forClass(Attributes.class);
    verify(fakeBalancer1).handleResolvedAddressGroups(
        Matchers.<List<EquivalentAddressGroup>>any(), captor.capture());
    assertThat(captor.getValue().get(ATTR_LOAD_BALANCING_CONFIG))
        .containsExactly("supported_1_option", "yes");
  }

  private static Attributes standardModeWithFallback1Attributes() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"fallbackPolicy\" : [{\"supported_1\" : { \"supported_1_option\" : \"yes\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    return Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();
  }

  @Test
  public void shutdown_cleanupTimers() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    @SuppressWarnings("unchecked")
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();
    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(fakeClock.getPendingTasks()).isNotEmpty();
    lb.shutdown();
    assertThat(fakeClock.getPendingTasks()).isEmpty();
  }
}
