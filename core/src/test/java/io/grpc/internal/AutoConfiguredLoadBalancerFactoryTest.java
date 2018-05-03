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
import io.grpc.internal.AutoConfiguredLoadBalancerFactory.AutoConfiguredLoadBalancer;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;
import javax.annotation.Nonnull;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link AutoConfiguredLoadBalancerFactory}.
 */
// TODO(carl-mastrangelo): Add tests for selection logic.
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
    public Subchannel createSubchannel(EquivalentAddressGroup addrs, Attributes attrs) {
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
}
