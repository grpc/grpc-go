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

package io.grpc.grpclb;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.grpclb.CachedSubchannelPool.SHUTDOWN_TIMEOUT_MS;
import static java.util.concurrent.TimeUnit.MILLISECONDS;
import static org.mockito.Mockito.any;
import static org.mockito.Mockito.atMost;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.same;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.grpclb.CachedSubchannelPool.ShutdownSubchannelScheduledTask;
import io.grpc.grpclb.CachedSubchannelPool.ShutdownSubchannelTask;
import io.grpc.internal.FakeClock;
import io.grpc.internal.SerializingExecutor;
import java.util.ArrayList;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/** Unit tests for {@link CachedSubchannelPool}. */
@RunWith(JUnit4.class)
public class CachedSubchannelPoolTest {
  private static final EquivalentAddressGroup EAG1 =
      new EquivalentAddressGroup(new FakeSocketAddress("fake-address-1"), Attributes.EMPTY);
  private static final EquivalentAddressGroup EAG2 =
      new EquivalentAddressGroup(new FakeSocketAddress("fake-address-2"), Attributes.EMPTY);
  private static final Attributes.Key<String> ATTR_KEY = Attributes.Key.create("test-attr");
  private static final Attributes ATTRS1 = Attributes.newBuilder().set(ATTR_KEY, "1").build();
  private static final Attributes ATTRS2 = Attributes.newBuilder().set(ATTR_KEY, "2").build();
  private static final FakeClock.TaskFilter SHUTDOWN_SCHEDULED_TASK_FILTER =
      new FakeClock.TaskFilter() {
        @Override
        public boolean shouldAccept(Runnable command) {
          return command instanceof ShutdownSubchannelScheduledTask;
        }
      };

  private final SerializingExecutor channelExecutor =
      new SerializingExecutor(MoreExecutors.directExecutor());
  private final Helper helper = mock(Helper.class);
  private final FakeClock clock = new FakeClock();
  private final CachedSubchannelPool pool = new CachedSubchannelPool();
  private final ArrayList<Subchannel> mockSubchannels = new ArrayList<>();

  @Before
  public void setUp() {
    doAnswer(new Answer<Subchannel>() {
        @Override
        public Subchannel answer(InvocationOnMock invocation) throws Throwable {
          Subchannel subchannel = mock(Subchannel.class);
          EquivalentAddressGroup eag = (EquivalentAddressGroup) invocation.getArguments()[0];
          Attributes attrs = (Attributes) invocation.getArguments()[1];
          when(subchannel.getAddresses()).thenReturn(eag);
          when(subchannel.getAttributes()).thenReturn(attrs);
          mockSubchannels.add(subchannel);
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
    pool.init(helper, clock.getScheduledExecutorService());
  }

  @After
  public void wrapUp() {
    // Sanity checks
    for (Subchannel subchannel : mockSubchannels) {
      verify(subchannel, atMost(1)).shutdown();
    }
  }

  @Test
  public void subchannelExpireAfterReturned() {
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1);
    assertThat(subchannel1).isNotNull();
    verify(helper).createSubchannel(same(EAG1), same(ATTRS1));

    Subchannel subchannel2 = pool.takeOrCreateSubchannel(EAG2, ATTRS2);
    assertThat(subchannel2).isNotNull();
    assertThat(subchannel2).isNotSameAs(subchannel1);
    verify(helper).createSubchannel(same(EAG2), same(ATTRS2));

    pool.returnSubchannel(subchannel1);

    // subchannel1 is 1ms away from expiration.
    clock.forwardTime(SHUTDOWN_TIMEOUT_MS - 1, MILLISECONDS);
    verify(subchannel1, never()).shutdown();

    pool.returnSubchannel(subchannel2);

    // subchannel1 expires. subchannel2 is (SHUTDOWN_TIMEOUT_MS - 1) away from expiration.
    clock.forwardTime(1, MILLISECONDS);
    verify(subchannel1).shutdown();

    // subchanne2 expires.
    clock.forwardTime(SHUTDOWN_TIMEOUT_MS - 1, MILLISECONDS);
    verify(subchannel2).shutdown();

    assertThat(clock.numPendingTasks()).isEqualTo(0);
    verify(helper, times(2)).runSerialized(any(ShutdownSubchannelTask.class));
  }

  @Test
  public void subchannelReused() {
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1);
    assertThat(subchannel1).isNotNull();
    verify(helper).createSubchannel(same(EAG1), same(ATTRS1));

    Subchannel subchannel2 = pool.takeOrCreateSubchannel(EAG2, ATTRS2);
    assertThat(subchannel2).isNotNull();
    assertThat(subchannel2).isNotSameAs(subchannel1);
    verify(helper).createSubchannel(same(EAG2), same(ATTRS2));

    pool.returnSubchannel(subchannel1);

    // subchannel1 is 1ms away from expiration.
    clock.forwardTime(SHUTDOWN_TIMEOUT_MS - 1, MILLISECONDS);

    // This will cancel the shutdown timer for subchannel1
    Subchannel subchannel1a = pool.takeOrCreateSubchannel(EAG1, ATTRS1);
    assertThat(subchannel1a).isSameAs(subchannel1);

    pool.returnSubchannel(subchannel2);

    // subchannel2 expires SHUTDOWN_TIMEOUT_MS after being returned
    clock.forwardTime(SHUTDOWN_TIMEOUT_MS - 1, MILLISECONDS);
    verify(subchannel2, never()).shutdown();
    clock.forwardTime(1, MILLISECONDS);
    verify(subchannel2).shutdown();

    // pool will create a new channel for EAG2 when requested
    Subchannel subchannel2a = pool.takeOrCreateSubchannel(EAG2, ATTRS2);
    assertThat(subchannel2a).isNotSameAs(subchannel2);
    verify(helper, times(2)).createSubchannel(same(EAG2), same(ATTRS2));

    // subchannel1 expires SHUTDOWN_TIMEOUT_MS after being returned
    pool.returnSubchannel(subchannel1a);
    clock.forwardTime(SHUTDOWN_TIMEOUT_MS - 1, MILLISECONDS);
    verify(subchannel1a, never()).shutdown();
    clock.forwardTime(1, MILLISECONDS);
    verify(subchannel1a).shutdown();

    assertThat(clock.numPendingTasks()).isEqualTo(0);
    verify(helper, times(2)).runSerialized(any(ShutdownSubchannelTask.class));
  }

  @Test
  public void returnDuplicateAddressSubchannel() {
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1);
    Subchannel subchannel2 = pool.takeOrCreateSubchannel(EAG1, ATTRS2);
    Subchannel subchannel3 = pool.takeOrCreateSubchannel(EAG2, ATTRS1);
    assertThat(subchannel1).isNotSameAs(subchannel2);

    assertThat(clock.getPendingTasks(SHUTDOWN_SCHEDULED_TASK_FILTER)).isEmpty();
    pool.returnSubchannel(subchannel2);
    assertThat(clock.getPendingTasks(SHUTDOWN_SCHEDULED_TASK_FILTER)).hasSize(1);

    // If the subchannel being returned has an address that is the same as a subchannel in the pool,
    // the returned subchannel will be shut down.
    verify(subchannel1, never()).shutdown();
    pool.returnSubchannel(subchannel1);
    assertThat(clock.getPendingTasks(SHUTDOWN_SCHEDULED_TASK_FILTER)).hasSize(1);
    verify(subchannel1).shutdown();

    pool.returnSubchannel(subchannel3);
    assertThat(clock.getPendingTasks(SHUTDOWN_SCHEDULED_TASK_FILTER)).hasSize(2);
    // Returning the same subchannel twice has no effect.
    pool.returnSubchannel(subchannel3);
    assertThat(clock.getPendingTasks(SHUTDOWN_SCHEDULED_TASK_FILTER)).hasSize(2);

    verify(subchannel2, never()).shutdown();
    verify(subchannel3, never()).shutdown();
    verify(helper, never()).runSerialized(any(ShutdownSubchannelTask.class));
  }

  @Test
  public void clear() {
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1);
    Subchannel subchannel2 = pool.takeOrCreateSubchannel(EAG2, ATTRS2);
    Subchannel subchannel3 = pool.takeOrCreateSubchannel(EAG2, ATTRS2);

    pool.returnSubchannel(subchannel1);
    pool.returnSubchannel(subchannel2);

    verify(subchannel1, never()).shutdown();
    verify(subchannel2, never()).shutdown();
    pool.clear();
    verify(subchannel1).shutdown();
    verify(subchannel2).shutdown();

    verify(subchannel3, never()).shutdown();
    assertThat(clock.numPendingTasks()).isEqualTo(0);
    verify(helper, never()).runSerialized(any(ShutdownSubchannelTask.class));
  }
}
