/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc;

import static com.google.common.truth.Truth.assertThat;
import static org.mockito.Mockito.mock;

import io.grpc.ClientStreamTracer;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.Status;
import java.net.SocketAddress;
import java.util.Arrays;
import java.util.List;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for the inner classes in {@link LoadBalancer}. */
@RunWith(JUnit4.class)
public class LoadBalancerTest {
  private final Subchannel subchannel = mock(Subchannel.class);
  private final Subchannel subchannel2 = mock(Subchannel.class);
  private final ClientStreamTracer.Factory tracerFactory = mock(ClientStreamTracer.Factory.class);
  private final Status status = Status.UNAVAILABLE.withDescription("for test");
  private final Status status2 = Status.UNAVAILABLE.withDescription("for test 2");
  private final EquivalentAddressGroup eag = new EquivalentAddressGroup(new SocketAddress() {});
  private final Attributes attrs = Attributes.newBuilder()
      .set(Attributes.Key.create("trash"), new Object())
      .build();
  private final Subchannel emptySubchannel = new EmptySubchannel();

  @Test
  public void pickResult_withSubchannel() {
    PickResult result = PickResult.withSubchannel(subchannel);
    assertThat(result.getSubchannel()).isSameAs(subchannel);
    assertThat(result.getStatus()).isSameAs(Status.OK);
    assertThat(result.getStreamTracerFactory()).isNull();
    assertThat(result.isDrop()).isFalse();
  }

  @Test
  public void pickResult_withSubchannelAndTracer() {
    PickResult result = PickResult.withSubchannel(subchannel, tracerFactory);
    assertThat(result.getSubchannel()).isSameAs(subchannel);
    assertThat(result.getStatus()).isSameAs(Status.OK);
    assertThat(result.getStreamTracerFactory()).isSameAs(tracerFactory);
    assertThat(result.isDrop()).isFalse();
  }

  @Test
  public void pickResult_withNoResult() {
    PickResult result = PickResult.withNoResult();
    assertThat(result.getSubchannel()).isNull();
    assertThat(result.getStatus()).isSameAs(Status.OK);
    assertThat(result.getStreamTracerFactory()).isNull();
    assertThat(result.isDrop()).isFalse();
  }

  @Test
  public void pickResult_withError() {
    PickResult result = PickResult.withError(status);
    assertThat(result.getSubchannel()).isNull();
    assertThat(result.getStatus()).isSameAs(status);
    assertThat(result.getStreamTracerFactory()).isNull();
    assertThat(result.isDrop()).isFalse();
  }

  @Test
  public void pickResult_withDrop() {
    PickResult result = PickResult.withDrop(status);
    assertThat(result.getSubchannel()).isNull();
    assertThat(result.getStatus()).isSameAs(status);
    assertThat(result.getStreamTracerFactory()).isNull();
    assertThat(result.isDrop()).isTrue();
  }

  @Test
  public void pickResult_equals() {
    PickResult sc1 = PickResult.withSubchannel(subchannel);
    PickResult sc2 = PickResult.withSubchannel(subchannel);
    PickResult sc3 = PickResult.withSubchannel(subchannel, tracerFactory);
    PickResult sc4 = PickResult.withSubchannel(subchannel2);
    PickResult nr = PickResult.withNoResult();
    PickResult error1 = PickResult.withError(status);
    PickResult error2 = PickResult.withError(status2);
    PickResult error3 = PickResult.withError(status2);
    PickResult drop1 = PickResult.withDrop(status);
    PickResult drop2 = PickResult.withDrop(status);
    PickResult drop3 = PickResult.withDrop(status2);

    assertThat(sc1).isNotEqualTo(nr);
    assertThat(sc1).isNotEqualTo(error1);
    assertThat(sc1).isNotEqualTo(drop1);
    assertThat(sc1).isEqualTo(sc2);
    assertThat(sc1).isNotEqualTo(sc3);
    assertThat(sc1).isNotEqualTo(sc4);

    assertThat(error1).isNotEqualTo(error2);
    assertThat(error2).isEqualTo(error3);

    assertThat(drop1).isEqualTo(drop2);
    assertThat(drop1).isNotEqualTo(drop3);

    assertThat(error1.getStatus()).isEqualTo(drop1.getStatus());
    assertThat(error1).isNotEqualTo(drop1);
  }

  @Test
  public void helper_createSubchannel_delegates() {
    class OverrideCreateSubchannel extends NoopHelper {
      boolean ran;

      @Override
      public Subchannel createSubchannel(List<EquivalentAddressGroup> addrsIn, Attributes attrsIn) {
        assertThat(addrsIn).hasSize(1);
        assertThat(addrsIn.get(0)).isSameAs(eag);
        assertThat(attrsIn).isSameAs(attrs);
        ran = true;
        return subchannel;
      }
    }

    OverrideCreateSubchannel helper = new OverrideCreateSubchannel();
    assertThat(helper.createSubchannel(eag, attrs)).isSameAs(subchannel);
    assertThat(helper.ran).isTrue();
  }

  @Test(expected = UnsupportedOperationException.class)
  public void helper_createSubchannelList_throws() {
    new NoopHelper().createSubchannel(Arrays.asList(eag), attrs);
  }

  @Test
  public void helper_updateSubchannelAddresses_delegates() {
    class OverrideUpdateSubchannel extends NoopHelper {
      boolean ran;

      @Override
      public void updateSubchannelAddresses(
          Subchannel subchannelIn, List<EquivalentAddressGroup> addrsIn) {
        assertThat(subchannelIn).isSameAs(emptySubchannel);
        assertThat(addrsIn).hasSize(1);
        assertThat(addrsIn.get(0)).isSameAs(eag);
        ran = true;
      }
    }

    OverrideUpdateSubchannel helper = new OverrideUpdateSubchannel();
    helper.updateSubchannelAddresses(emptySubchannel, eag);
    assertThat(helper.ran).isTrue();
  }

  @Test(expected = UnsupportedOperationException.class)
  public void helper_updateSubchannelAddressesList_throws() {
    new NoopHelper().updateSubchannelAddresses(null, Arrays.asList(eag));
  }

  @Test
  public void subchannel_getAddresses_delegates() {
    class OverrideGetAllAddresses extends EmptySubchannel {
      boolean ran;

      @Override public List<EquivalentAddressGroup> getAllAddresses() {
        ran = true;
        return Arrays.asList(eag);
      }
    }

    OverrideGetAllAddresses subchannel = new OverrideGetAllAddresses();
    assertThat(subchannel.getAddresses()).isEqualTo(eag);
    assertThat(subchannel.ran).isTrue();
  }

  @Test(expected = IllegalStateException.class)
  public void subchannel_getAddresses_throwsOnTwoAddrs() {
    new EmptySubchannel() {
      boolean ran;

      @Override public List<EquivalentAddressGroup> getAllAddresses() {
        ran = true;
        // Doubling up eag is technically a bad idea, but nothing here cares
        return Arrays.asList(eag, eag);
      }
    }.getAddresses();
  }

  private static class NoopHelper extends LoadBalancer.Helper {
    @Override
    public ManagedChannel createOobChannel(EquivalentAddressGroup eag, String authority) {
      return null;
    }

    @Override
    public void updateBalancingState(
        ConnectivityState newState, LoadBalancer.SubchannelPicker newPicker) {}

    @Override public void runSerialized(Runnable task) {}

    @Override public NameResolver.Factory getNameResolverFactory() {
      return null;
    }

    @Override public String getAuthority() {
      return null;
    }
  }

  private static class EmptySubchannel extends LoadBalancer.Subchannel {
    @Override public void shutdown() {}

    @Override public void requestConnection() {}

    @Override public Attributes getAttributes() {
      return null;
    }
  }
}
