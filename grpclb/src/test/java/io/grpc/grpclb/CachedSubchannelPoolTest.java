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
import static org.junit.Assert.fail;
import static org.mockito.Mockito.any;
import static org.mockito.Mockito.atMost;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.same;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.ConnectivityState;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer.CreateSubchannelArgs;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelStateListener;
import io.grpc.Status;
import io.grpc.SynchronizationContext;
import io.grpc.grpclb.CachedSubchannelPool.ShutdownSubchannelTask;
import io.grpc.internal.FakeClock;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.hamcrest.MockitoHamcrest;
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
  private static final ConnectivityStateInfo READY_STATE =
      ConnectivityStateInfo.forNonError(ConnectivityState.READY);
  private static final ConnectivityStateInfo TRANSIENT_FAILURE_STATE =
      ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE.withDescription("Simulated"));
  private static final FakeClock.TaskFilter SHUTDOWN_TASK_FILTER =
      new FakeClock.TaskFilter() {
        @Override
        public boolean shouldAccept(Runnable command) {
          // The task is wrapped by SynchronizationContext, so we can't compare the type
          // directly.
          return command.toString().contains(ShutdownSubchannelTask.class.getSimpleName());
        }
      };

  private final Helper helper = mock(Helper.class);
  private final SubchannelStateListener mockListener = mock(SubchannelStateListener.class);
  private final FakeClock clock = new FakeClock();
  private final SynchronizationContext syncContext = new SynchronizationContext(
      new Thread.UncaughtExceptionHandler() {
        @Override
        public void uncaughtException(Thread t, Throwable e) {
          throw new AssertionError(e);
        }
      });
  private final CachedSubchannelPool pool = new CachedSubchannelPool();
  private final ArrayList<Subchannel> mockSubchannels = new ArrayList<>();
  // Listeners seen by the Helper
  private final Map<Subchannel, SubchannelStateListener> stateListeners = new HashMap<>();

  @Before
  @SuppressWarnings("unchecked")
  public void setUp() {
    doAnswer(new Answer<Subchannel>() {
        @Override
        public Subchannel answer(InvocationOnMock invocation) throws Throwable {
          Subchannel subchannel = mock(Subchannel.class);
          CreateSubchannelArgs args = (CreateSubchannelArgs) invocation.getArguments()[0];
          when(subchannel.getAllAddresses()).thenReturn(args.getAddresses());
          when(subchannel.getAttributes()).thenReturn(args.getAttributes());
          mockSubchannels.add(subchannel);
          stateListeners.put(subchannel, args.getStateListener());
          return subchannel;
        }
      }).when(helper).createSubchannel(any(CreateSubchannelArgs.class));
    when(helper.getSynchronizationContext()).thenReturn(syncContext);
    when(helper.getScheduledExecutorService()).thenReturn(clock.getScheduledExecutorService());
    pool.init(helper);
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
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener);
    assertThat(subchannel1).isNotNull();
    verify(helper).createSubchannel(argsWith(EAG1, "1"));

    Subchannel subchannel2 = pool.takeOrCreateSubchannel(EAG2, ATTRS2, mockListener);
    assertThat(subchannel2).isNotNull();
    assertThat(subchannel2).isNotSameAs(subchannel1);
    verify(helper).createSubchannel(argsWith(EAG2, "2"));

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
  }

  @Test
  public void subchannelReused() {
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener);
    assertThat(subchannel1).isNotNull();
    verify(helper).createSubchannel(argsWith(EAG1, "1"));

    Subchannel subchannel2 = pool.takeOrCreateSubchannel(EAG2, ATTRS2, mockListener);
    assertThat(subchannel2).isNotNull();
    assertThat(subchannel2).isNotSameAs(subchannel1);
    verify(helper).createSubchannel(argsWith(EAG2, "2"));

    pool.returnSubchannel(subchannel1);

    // subchannel1 is 1ms away from expiration.
    clock.forwardTime(SHUTDOWN_TIMEOUT_MS - 1, MILLISECONDS);

    // This will cancel the shutdown timer for subchannel1
    Subchannel subchannel1a = pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener);
    assertThat(subchannel1a).isSameAs(subchannel1);

    pool.returnSubchannel(subchannel2);

    // subchannel2 expires SHUTDOWN_TIMEOUT_MS after being returned
    clock.forwardTime(SHUTDOWN_TIMEOUT_MS - 1, MILLISECONDS);
    verify(subchannel2, never()).shutdown();
    clock.forwardTime(1, MILLISECONDS);
    verify(subchannel2).shutdown();

    // pool will create a new channel for EAG2 when requested
    Subchannel subchannel2a = pool.takeOrCreateSubchannel(EAG2, ATTRS2, mockListener);
    assertThat(subchannel2a).isNotSameAs(subchannel2);
    verify(helper, times(2)).createSubchannel(argsWith(EAG2, "2"));

    // subchannel1 expires SHUTDOWN_TIMEOUT_MS after being returned
    pool.returnSubchannel(subchannel1a);
    clock.forwardTime(SHUTDOWN_TIMEOUT_MS - 1, MILLISECONDS);
    verify(subchannel1a, never()).shutdown();
    clock.forwardTime(1, MILLISECONDS);
    verify(subchannel1a).shutdown();

    assertThat(clock.numPendingTasks()).isEqualTo(0);
  }

  @Test
  public void updateStateWhileInPool() {
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener);
    Subchannel subchannel2 = pool.takeOrCreateSubchannel(EAG2, ATTRS2, mockListener);

    // Simulate state updates while they are in the pool
    stateListeners.get(subchannel1).onSubchannelState(subchannel1, TRANSIENT_FAILURE_STATE);
    stateListeners.get(subchannel2).onSubchannelState(subchannel1, TRANSIENT_FAILURE_STATE);

    verify(mockListener).onSubchannelState(same(subchannel1), same(TRANSIENT_FAILURE_STATE));
    verify(mockListener).onSubchannelState(same(subchannel2), same(TRANSIENT_FAILURE_STATE));

    pool.returnSubchannel(subchannel1);
    pool.returnSubchannel(subchannel2);

    ConnectivityStateInfo anotherFailureState =
        ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE.withDescription("Another"));

    // Simulate a subchannel state update while it's in the pool
    stateListeners.get(subchannel1).onSubchannelState(subchannel1, anotherFailureState);

    SubchannelStateListener mockListener1 = mock(SubchannelStateListener.class);
    SubchannelStateListener mockListener2 = mock(SubchannelStateListener.class);

    // Saved state is populated to new mockListeners
    assertThat(pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener1)).isSameAs(subchannel1);
    verify(mockListener1).onSubchannelState(same(subchannel1), same(anotherFailureState));
    verifyNoMoreInteractions(mockListener1);

    assertThat(pool.takeOrCreateSubchannel(EAG2, ATTRS2, mockListener2)).isSameAs(subchannel2);
    verify(mockListener2).onSubchannelState(same(subchannel2), same(TRANSIENT_FAILURE_STATE));
    verifyNoMoreInteractions(mockListener2);

    // The old mockListener doesn't receive more updates
    verifyNoMoreInteractions(mockListener);
  }

  @Test
  public void takeTwice_willThrow() {
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener);
    try {
      pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener);
      fail("Should throw");
    } catch (IllegalStateException e) {
      assertThat(e).hasMessageThat().contains("Already out of pool");
    }
  }

  @Test
  public void returnTwice_willThrow() {
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener);
    pool.returnSubchannel(subchannel1);
    try {
      pool.returnSubchannel(subchannel1);
      fail("Should throw");
    } catch (IllegalStateException e) {
      assertThat(e).hasMessageThat().contains("Already in pool");
    }
  }

  @Test
  public void returnNonPoolSubchannelWillThrow_noSuchAddress() {
    Subchannel subchannel1 = helper.createSubchannel(
        CreateSubchannelArgs.newBuilder()
            .setAddresses(EAG1).setStateListener(mockListener)
            .build());
    try {
      pool.returnSubchannel(subchannel1);
      fail("Should throw");
    } catch (IllegalArgumentException e) {
      assertThat(e).hasMessageThat().contains("not found");
    }
  }

  @Test
  public void returnNonPoolSubchannelWillThrow_unmatchedSubchannel() {
    Subchannel subchannel1 = helper.createSubchannel(
        CreateSubchannelArgs.newBuilder()
            .setAddresses(EAG1).setStateListener(mockListener)
            .build());
    Subchannel subchannel1c = pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener);
    assertThat(subchannel1).isNotSameAs(subchannel1c);
    try {
      pool.returnSubchannel(subchannel1);
      fail("Should throw");
    } catch (IllegalArgumentException e) {
      assertThat(e).hasMessageThat().contains("doesn't match the cache");
    }
  }

  @Test
  public void clear() {
    Subchannel subchannel1 = pool.takeOrCreateSubchannel(EAG1, ATTRS1, mockListener);
    Subchannel subchannel2 = pool.takeOrCreateSubchannel(EAG2, ATTRS2, mockListener);

    pool.returnSubchannel(subchannel1);

    verify(subchannel1, never()).shutdown();
    verify(subchannel2, never()).shutdown();
    pool.clear();
    verify(subchannel1).shutdown();
    verify(subchannel2).shutdown();
    assertThat(clock.numPendingTasks()).isEqualTo(0);
  }

  private CreateSubchannelArgs argsWith(
      final EquivalentAddressGroup expectedEag, final Object expectedValue) {
    return MockitoHamcrest.argThat(
        new org.hamcrest.BaseMatcher<CreateSubchannelArgs>() {
          @Override
          public boolean matches(Object item) {
            if (!(item instanceof CreateSubchannelArgs)) {
              return false;
            }
            CreateSubchannelArgs that = (CreateSubchannelArgs) item;
            List<EquivalentAddressGroup> expectedEagList = Collections.singletonList(expectedEag);
            if (!expectedEagList.equals(that.getAddresses())) {
              return false;
            }
            if (!expectedValue.equals(that.getAttributes().get(ATTR_KEY))) {
              return false;
            }
            return true;
          }

          @Override
          public void describeTo(org.hamcrest.Description desc) {
            desc.appendText(
                "Matches Attributes that includes " + expectedEag + " and "
                + ATTR_KEY + "=" + expectedValue);
          }
        });
  }

}
