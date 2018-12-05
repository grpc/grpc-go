/*
 * Copyright 2018 The gRPC Authors
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
import static io.grpc.LoadBalancer.ATTR_LOAD_BALANCING_CONFIG;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.RETURNS_DEEP_STUBS;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import io.grpc.Attributes;
import io.grpc.ChannelLogger;
import io.grpc.ChannelLogger.ChannelLogLevel;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;
import io.grpc.ManagedChannel;
import io.grpc.Status;
import io.grpc.SynchronizationContext;
import io.grpc.grpclb.GrpclbLoadBalancerProvider;
import io.grpc.internal.AutoConfiguredLoadBalancerFactory.AutoConfiguredLoadBalancer;
import io.grpc.internal.AutoConfiguredLoadBalancerFactory.PolicyException;
import io.grpc.internal.AutoConfiguredLoadBalancerFactory.PolicySelection;
import io.grpc.util.ForwardingLoadBalancerHelper;
import java.net.SocketAddress;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

/**
 * Unit tests for {@link AutoConfiguredLoadBalancerFactory}.
 */
@RunWith(JUnit4.class)
public class AutoConfiguredLoadBalancerFactoryTest {
  private final AutoConfiguredLoadBalancerFactory lbf = new AutoConfiguredLoadBalancerFactory();

  private final ChannelLogger channelLogger = mock(ChannelLogger.class);
  private final LoadBalancer testLbBalancer = mock(LoadBalancer.class);
  private final LoadBalancerProvider testLbBalancerProvider =
      mock(LoadBalancerProvider.class, delegatesTo(new TestLbLoadBalancerProvider()));

  @Before
  public void setUp() {
    LoadBalancerRegistry.getDefaultRegistry().register(testLbBalancerProvider);
  }

  @After
  public void tearDown() {
    LoadBalancerRegistry.getDefaultRegistry().deregister(testLbBalancerProvider);
  }

  @Test
  public void newLoadBalancer_isAuto() {
    LoadBalancer lb = lbf.newLoadBalancer(new TestHelper());

    assertThat(lb).isInstanceOf(AutoConfiguredLoadBalancer.class);
  }

  @Test
  public void defaultIsPickFirst() {
    AutoConfiguredLoadBalancer lb =
        (AutoConfiguredLoadBalancer) lbf.newLoadBalancer(new TestHelper());

    assertThat(lb.getDelegateProvider()).isInstanceOf(PickFirstLoadBalancerProvider.class);
    assertThat(lb.getDelegate().getClass().getName()).contains("PickFirst");
  }

  @Test
  public void forwardsCalls() {
    AutoConfiguredLoadBalancer lb =
        (AutoConfiguredLoadBalancer) lbf.newLoadBalancer(new TestHelper());

    final AtomicInteger calls = new AtomicInteger();
    TestLoadBalancer testlb = new TestLoadBalancer() {

      @Override
      public void handleNameResolutionError(Status error) {
        calls.getAndSet(1);
      }

      @Override
      public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {
        calls.getAndSet(2);
      }

      @Override
      public void shutdown() {
        calls.getAndSet(3);
      }
    };

    lb.setDelegate(testlb);

    lb.handleNameResolutionError(Status.RESOURCE_EXHAUSTED);
    assertThat(calls.getAndSet(0)).isEqualTo(1);

    lb.handleSubchannelState(null, null);
    assertThat(calls.getAndSet(0)).isEqualTo(2);

    lb.shutdown();
    assertThat(calls.getAndSet(0)).isEqualTo(3);
  }

  @Test
  public void handleResolvedAddressGroups_keepOldBalancer() {
    final List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(new SocketAddress(){}, Attributes.EMPTY));
    Helper helper = new TestHelper() {
      @Override
      public Subchannel createSubchannel(List<EquivalentAddressGroup> addrs, Attributes attrs) {
        assertThat(addrs).isEqualTo(servers);
        return new TestSubchannel(addrs, attrs);
      }

      @Override
      public void updateBalancingState(ConnectivityState newState, SubchannelPicker newPicker) {
        // noop
      }
    };
    AutoConfiguredLoadBalancer lb =
        (AutoConfiguredLoadBalancer) lbf.newLoadBalancer(helper);
    LoadBalancer oldDelegate = lb.getDelegate();

    lb.handleResolvedAddressGroups(servers, Attributes.EMPTY);

    assertThat(lb.getDelegate()).isSameAs(oldDelegate);
  }

  @Test
  public void handleResolvedAddressGroups_shutsDownOldBalancer() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("loadBalancingPolicy", "round_robin");
    Attributes serviceConfigAttrs =
        Attributes.newBuilder()
            .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig)
            .build();
    final List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.EMPTY));
    Helper helper = new TestHelper() {
      @Override
      public Subchannel createSubchannel(List<EquivalentAddressGroup> addrs, Attributes attrs) {
        assertThat(addrs).isEqualTo(servers);
        return new TestSubchannel(addrs, attrs);
      }

      @Override
      public void updateBalancingState(ConnectivityState newState, SubchannelPicker newPicker) {
        // noop
      }
    };
    AutoConfiguredLoadBalancer lb =
        (AutoConfiguredLoadBalancer) lbf.newLoadBalancer(helper);
    final AtomicBoolean shutdown = new AtomicBoolean();
    TestLoadBalancer testlb = new TestLoadBalancer() {

      @Override
      public void handleNameResolutionError(Status error) {
        // noop
      }

      @Override
      public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {
        // noop
      }

      @Override
      public void shutdown() {
        shutdown.set(true);
      }
    };
    lb.setDelegate(testlb);

    lb.handleResolvedAddressGroups(servers, serviceConfigAttrs);

    assertThat(lb.getDelegateProvider().getClass().getName()).isEqualTo(
        "io.grpc.util.SecretRoundRobinLoadBalancerProvider$Provider");
    assertTrue(shutdown.get());
  }

  @Test
  public void handleResolvedAddressGroups_propagateLbConfigToDelegate() throws Exception {
    Map<String, Object> serviceConfig =
        parseConfig("{\"loadBalancingConfig\": [ {\"test_lb\": { \"setting1\": \"high\" } } ] }");
    Attributes serviceConfigAttrs =
        Attributes.newBuilder()
            .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig)
            .build();
    final List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.EMPTY));
    Helper helper = new TestHelper() {
        @Override
        public void updateBalancingState(ConnectivityState newState, SubchannelPicker newPicker) {
          // noop
        }
      };
    AutoConfiguredLoadBalancer lb =
        (AutoConfiguredLoadBalancer) lbf.newLoadBalancer(helper);

    lb.handleResolvedAddressGroups(servers, serviceConfigAttrs);

    verify(testLbBalancerProvider).newLoadBalancer(same(helper));
    assertThat(lb.getDelegate()).isSameAs(testLbBalancer);
    ArgumentCaptor<Attributes> attrsCaptor = ArgumentCaptor.forClass(null);
    verify(testLbBalancer).handleResolvedAddressGroups(same(servers), attrsCaptor.capture());
    assertThat(attrsCaptor.getValue().get(ATTR_LOAD_BALANCING_CONFIG))
        .isEqualTo(Collections.singletonMap("setting1", "high"));
    verifyNoMoreInteractions(testLbBalancer);

    serviceConfig =
        parseConfig("{\"loadBalancingConfig\": [ {\"test_lb\": { \"setting1\": \"low\" } } ] }");
    serviceConfigAttrs =
        Attributes.newBuilder()
            .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig)
            .build();
    lb.handleResolvedAddressGroups(servers, serviceConfigAttrs);

    verify(testLbBalancer, times(2))
        .handleResolvedAddressGroups(same(servers), attrsCaptor.capture());
    // But the balancer config is changed.
    assertThat(attrsCaptor.getValue().get(ATTR_LOAD_BALANCING_CONFIG))
        .isEqualTo(Collections.singletonMap("setting1", "low"));
    // Service config didn't change policy, thus the delegateLb is not swapped
    verifyNoMoreInteractions(testLbBalancer);
    verify(testLbBalancerProvider).newLoadBalancer(any(Helper.class));
  }

  @Test
  public void decideLoadBalancerProvider_noBalancerAddresses_noServiceConfig_pickFirst()
      throws Exception {
    Map<String, Object> serviceConfig = null;
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(new SocketAddress(){}, Attributes.EMPTY));
    PolicySelection selection =
        AutoConfiguredLoadBalancerFactory.decideLoadBalancerProvider(
            servers, serviceConfig, channelLogger);

    assertThat(selection.provider).isInstanceOf(PickFirstLoadBalancerProvider.class);
    assertThat(selection.config).isNull();
    verifyZeroInteractions(channelLogger);
  }

  @Test
  public void decideLoadBalancerProvider_oneBalancer_noServiceConfig_grpclb() throws Exception {
    Map<String, Object> serviceConfig = null;
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.newBuilder().set(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY, "ok").build()));
    PolicySelection selection = AutoConfiguredLoadBalancerFactory.decideLoadBalancerProvider(
        servers, serviceConfig, channelLogger);

    assertThat(selection.provider).isInstanceOf(GrpclbLoadBalancerProvider.class);
    assertThat(selection.config).isNull();
    verifyZeroInteractions(channelLogger);
  }

  @Test
  public void decideLoadBalancerProvider_grpclbOverridesServiceConfigLbPolicy() throws Exception {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("loadBalancingPolicy", "round_robin");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.newBuilder().set(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY, "ok").build()));
    PolicySelection selection = AutoConfiguredLoadBalancerFactory.decideLoadBalancerProvider(
        servers, serviceConfig, channelLogger);

    assertThat(selection.provider).isInstanceOf(GrpclbLoadBalancerProvider.class);
    assertThat(selection.config).isNull();
    verifyZeroInteractions(channelLogger);
  }

  @SuppressWarnings("unchecked")
  @Test
  public void decideLoadBalancerProvider_grpclbOverridesServiceConfigLbConfig() throws Exception {
    Map<String, Object> serviceConfig =
        parseConfig("{\"loadBalancingConfig\": [ {\"round_robin\": {} } ] }");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.newBuilder().set(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY, "ok").build()));
    PolicySelection selection = AutoConfiguredLoadBalancerFactory.decideLoadBalancerProvider(
        servers, serviceConfig, channelLogger);

    assertThat(selection.provider).isInstanceOf(GrpclbLoadBalancerProvider.class);
    assertThat(selection.config).isNull();
    verifyZeroInteractions(channelLogger);
  }

  @Test
  public void decideLoadBalancerProvider_serviceConfigLbPolicyOverridesDefault() throws Exception {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("loadBalancingPolicy", "round_robin");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.EMPTY));
    PolicySelection selection = AutoConfiguredLoadBalancerFactory.decideLoadBalancerProvider(
        servers, serviceConfig, channelLogger);

    assertThat(selection.provider.getClass().getName()).isEqualTo(
        "io.grpc.util.SecretRoundRobinLoadBalancerProvider$Provider");
    assertThat(selection.config).isEqualTo(Collections.emptyMap());
    verifyZeroInteractions(channelLogger);
  }

  @Test
  public void decideLoadBalancerProvider_serviceConfigLbConfigOverridesDefault() throws Exception {
    Map<String, Object> serviceConfig =
        parseConfig("{\"loadBalancingConfig\": [ {\"round_robin\": {\"setting1\": \"high\"} } ] }");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.EMPTY));
    PolicySelection selection = AutoConfiguredLoadBalancerFactory.decideLoadBalancerProvider(
        servers, serviceConfig, channelLogger);

    assertThat(selection.provider.getClass().getName()).isEqualTo(
        "io.grpc.util.SecretRoundRobinLoadBalancerProvider$Provider");
    assertThat(selection.config).isEqualTo(Collections.singletonMap("setting1", "high"));
    verifyZeroInteractions(channelLogger);
  }

  @Test
  public void decideLoadBalancerProvider_serviceConfigLbPolicyFailsOnUnknown() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("loadBalancingPolicy", "MAGIC_BALANCER");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.EMPTY));
    try {
      AutoConfiguredLoadBalancerFactory.decideLoadBalancerProvider(
          servers, serviceConfig, channelLogger);
      fail();
    } catch (PolicyException e) {
      assertThat(e.getMessage()).isEqualTo(
          "None of [magic_balancer] specified by Service Config are available.");
    }
  }

  @Test
  public void decideLoadBalancerProvider_serviceConfigLbConfigFailsOnUnknown() throws Exception {
    Map<String, Object> serviceConfig =
        parseConfig("{\"loadBalancingConfig\": [ {\"magic_balancer\": {} } ] }");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.EMPTY));
    try {
      AutoConfiguredLoadBalancerFactory.decideLoadBalancerProvider(
          servers, serviceConfig, channelLogger);
      fail();
    } catch (PolicyException e) {
      assertThat(e.getMessage()).isEqualTo(
          "None of [magic_balancer] specified by Service Config are available.");
    }
  }

  @Test
  public void decideLoadBalancerProvider_serviceConfigLbConfigSkipUnknown() throws Exception {
    Map<String, Object> serviceConfig =
        parseConfig(
            "{\"loadBalancingConfig\": [ {\"magic_balancer\": {} }, {\"round_robin\": {} } ] }");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.EMPTY));
    PolicySelection selection = AutoConfiguredLoadBalancerFactory.decideLoadBalancerProvider(
        servers, serviceConfig, channelLogger);

    assertThat(selection.provider.getClass().getName()).isEqualTo(
        "io.grpc.util.SecretRoundRobinLoadBalancerProvider$Provider");
    assertThat(selection.config).isEqualTo(Collections.emptyMap());
    verify(channelLogger).log(
        eq(ChannelLogLevel.DEBUG),
        eq("{0} specified by Service Config are not available"),
        eq(new LinkedHashSet<String>(Arrays.asList("magic_balancer"))));
  }

  @Test
  public void channelTracing_lbPolicyChanged() {
    final FakeClock clock = new FakeClock();
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(new SocketAddress(){}, Attributes.EMPTY));
    Helper helper = new TestHelper() {
      @Override
      public Subchannel createSubchannel(List<EquivalentAddressGroup> addrs, Attributes attrs) {
        return new TestSubchannel(addrs, attrs);
      }

      @Override
      public ManagedChannel createOobChannel(EquivalentAddressGroup eag, String authority) {
        return mock(ManagedChannel.class, RETURNS_DEEP_STUBS);
      }

      @Override
      public String getAuthority() {
        return "fake_authority";
      }

      @Override
      public void updateBalancingState(ConnectivityState newState, SubchannelPicker newPicker) {
        // noop
      }

      @Override
      public SynchronizationContext getSynchronizationContext() {
        return new SynchronizationContext(
            new Thread.UncaughtExceptionHandler() {
              @Override
              public void uncaughtException(Thread t, Throwable e) {
                throw new AssertionError(e);
              }
            });
      }

      @Override
      public ScheduledExecutorService getScheduledExecutorService() {
        return clock.getScheduledExecutorService();
      }
    };

    LoadBalancer lb = new AutoConfiguredLoadBalancerFactory().newLoadBalancer(helper);
    lb.handleResolvedAddressGroups(servers, Attributes.EMPTY);

    verifyNoMoreInteractions(channelLogger);

    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("loadBalancingPolicy", "round_robin");
    lb.handleResolvedAddressGroups(servers,
        Attributes.newBuilder()
            .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig).build());

    verify(channelLogger).log(
        eq(ChannelLogLevel.INFO),
        eq("Load balancer changed from {0} to {1}"),
        eq("PickFirstLoadBalancer"), eq("RoundRobinLoadBalancer"));
    verify(channelLogger).log(
        eq(ChannelLogLevel.DEBUG),
        eq("Load-balancing config: {0}"),
        eq(Collections.emptyMap()));
    verifyNoMoreInteractions(channelLogger);

    serviceConfig.put("loadBalancingPolicy", "round_robin");
    lb.handleResolvedAddressGroups(servers,
        Attributes.newBuilder()
            .set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig).build());
    verify(channelLogger, times(2)).log(
        eq(ChannelLogLevel.DEBUG),
        eq("Load-balancing config: {0}"),
        eq(Collections.emptyMap()));
    verifyNoMoreInteractions(channelLogger);

    servers = Collections.singletonList(new EquivalentAddressGroup(
        new SocketAddress(){},
        Attributes.newBuilder().set(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY, "ok").build()));
    lb.handleResolvedAddressGroups(servers, Attributes.EMPTY);

    verify(channelLogger).log(
        eq(ChannelLogLevel.INFO),
        eq("Load balancer changed from {0} to {1}"),
        eq("RoundRobinLoadBalancer"), eq("GrpclbLoadBalancer"));

    verifyNoMoreInteractions(channelLogger);
  }

  public static class ForwardingLoadBalancer extends LoadBalancer {
    private final LoadBalancer delegate;

    public ForwardingLoadBalancer(LoadBalancer delegate) {
      this.delegate = delegate;
    }

    protected LoadBalancer delegate() {
      return delegate;
    }

    @Override
    public void handleResolvedAddressGroups(
        List<EquivalentAddressGroup> servers, Attributes attributes) {
      delegate().handleResolvedAddressGroups(servers, attributes);
    }

    @Override
    public void handleNameResolutionError(Status error) {
      delegate().handleNameResolutionError(error);
    }

    @Override
    public void handleSubchannelState(Subchannel subchannel, ConnectivityStateInfo stateInfo) {
      delegate().handleSubchannelState(subchannel, stateInfo);
    }

    @Override
    public void shutdown() {
      delegate().shutdown();
    }
  }

  @SuppressWarnings("unchecked")
  private static Map<String, Object> parseConfig(String json) throws Exception {
    return (Map<String, Object>) JsonParser.parse(json);
  }

  private static class TestLoadBalancer extends ForwardingLoadBalancer {
    TestLoadBalancer() {
      super(null);
    }
  }

  private class TestHelper extends ForwardingLoadBalancerHelper {
    @Override
    protected Helper delegate() {
      return null;
    }

    @Override
    public ChannelLogger getChannelLogger() {
      return channelLogger;
    }
  }

  private static class TestSubchannel extends Subchannel {
    TestSubchannel(List<EquivalentAddressGroup> addrs, Attributes attrs) {
      this.addrs = addrs;
      this.attrs = attrs;
    }

    final List<EquivalentAddressGroup> addrs;
    final Attributes attrs;

    @Override
    public void shutdown() {
    }

    @Override
    public void requestConnection() {
    }

    @Override
    public List<EquivalentAddressGroup> getAllAddresses() {
      return addrs;
    }

    @Override
    public Attributes getAttributes() {
      return attrs;
    }
  }

  private final class TestLbLoadBalancerProvider extends LoadBalancerProvider {
    static final String POLICY_NAME = "test_lb";

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
      return POLICY_NAME;
    }

    @Override
    public LoadBalancer newLoadBalancer(Helper helper) {
      return testLbBalancer;
    }
  }
}
