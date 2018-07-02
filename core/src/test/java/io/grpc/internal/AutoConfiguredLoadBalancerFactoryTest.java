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
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import io.grpc.Attributes;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.ManagedChannel;
import io.grpc.NameResolver.Factory;
import io.grpc.PickFirstBalancerFactory;
import io.grpc.Status;
import io.grpc.grpclb.GrpclbLoadBalancerFactory;
import io.grpc.internal.AutoConfiguredLoadBalancerFactory.AutoConfiguredLoadBalancer;
import io.grpc.util.RoundRobinLoadBalancerFactory;
import java.net.SocketAddress;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import javax.annotation.Nonnull;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link AutoConfiguredLoadBalancerFactory}.
 */
@RunWith(JUnit4.class)
public class AutoConfiguredLoadBalancerFactoryTest {
  private final AutoConfiguredLoadBalancerFactory lbf = new AutoConfiguredLoadBalancerFactory();

  @Test
  public void newLoadBalancer_isAuto() {
    LoadBalancer lb = lbf.newLoadBalancer(new TestHelper());

    assertThat(lb).isInstanceOf(AutoConfiguredLoadBalancer.class);
  }

  @Test
  public void defaultIsPickFirst() {
    AutoConfiguredLoadBalancer lb =
        (AutoConfiguredLoadBalancer) lbf.newLoadBalancer(new TestHelper());

    assertThat(lb.getDelegateFactory()).isInstanceOf(PickFirstBalancerFactory.class);
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

    assertThat(lb.getDelegateFactory()).isEqualTo(RoundRobinLoadBalancerFactory.getInstance());
    assertTrue(shutdown.get());
  }

  @Test
  public void decideLoadBalancerFactory_noBalancerAddresses_noServiceConfig_pickFirst() {
    Map<String, Object> serviceConfig = null;
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(new SocketAddress(){}, Attributes.EMPTY));
    LoadBalancer.Factory factory =
        AutoConfiguredLoadBalancer.decideLoadBalancerFactory(servers, serviceConfig);

    assertThat(factory).isInstanceOf(PickFirstBalancerFactory.class);
  }

  @Test
  public void decideLoadBalancerFactory_oneBalancer_noServiceConfig_grpclb() {
    Map<String, Object> serviceConfig = null;
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.newBuilder().set(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY, "ok").build()));
    LoadBalancer.Factory factory = AutoConfiguredLoadBalancer.decideLoadBalancerFactory(
        servers, serviceConfig);

    assertThat(factory).isInstanceOf(GrpclbLoadBalancerFactory.class);
  }

  @Test
  public void decideLoadBalancerFactory_grpclbOverridesServiceConfig() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("loadBalancingPolicy", "round_robin");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.newBuilder().set(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY, "ok").build()));
    LoadBalancer.Factory factory = AutoConfiguredLoadBalancer.decideLoadBalancerFactory(
        servers, serviceConfig);

    assertThat(factory).isInstanceOf(GrpclbLoadBalancerFactory.class);
  }

  @Test
  public void decideLoadBalancerFactory_serviceConfigOverridesDefault() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("loadBalancingPolicy", "round_robin");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.EMPTY));
    LoadBalancer.Factory factory = AutoConfiguredLoadBalancer.decideLoadBalancerFactory(
        servers, serviceConfig);

    assertThat(factory).isInstanceOf(RoundRobinLoadBalancerFactory.class);
  }

  @Test
  public void decideLoadBalancerFactory_serviceConfigFailsOnUnknown() {
    Map<String, Object> serviceConfig = new HashMap<String, Object>();
    serviceConfig.put("loadBalancingPolicy", "MAGIC_BALANCER");
    List<EquivalentAddressGroup> servers =
        Collections.singletonList(
            new EquivalentAddressGroup(
                new SocketAddress(){},
                Attributes.EMPTY));
    try {
      AutoConfiguredLoadBalancer.decideLoadBalancerFactory(servers, serviceConfig);
      fail();
    } catch (IllegalArgumentException e) {
      // expected
    }
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

  public static class ForwardingLoadBalancerHelper extends Helper {

    private final Helper delegate;

    public ForwardingLoadBalancerHelper(Helper delegate) {
      this.delegate = delegate;
    }

    protected Helper delegate() {
      return delegate;
    }

    @Override
    public Subchannel createSubchannel(List<EquivalentAddressGroup> addrs, Attributes attrs) {
      return delegate().createSubchannel(addrs, attrs);
    }

    @Override
    public ManagedChannel createOobChannel(EquivalentAddressGroup eag, String authority) {
      return delegate().createOobChannel(eag, authority);
    }

    @Override
    public void updateBalancingState(
        @Nonnull ConnectivityState newState, @Nonnull SubchannelPicker newPicker) {
      delegate().updateBalancingState(newState, newPicker);
    }

    @Override
    public void runSerialized(Runnable task) {
      delegate().runSerialized(task);
    }

    @Override
    public Factory getNameResolverFactory() {
      return delegate().getNameResolverFactory();
    }

    @Override
    public String getAuthority() {
      return delegate().getAuthority();
    }
  }

  private static class TestLoadBalancer extends ForwardingLoadBalancer {
    TestLoadBalancer() {
      super(null);
    }
  }

  private static class TestHelper extends ForwardingLoadBalancerHelper {
    TestHelper() {
      super(null);
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
}
