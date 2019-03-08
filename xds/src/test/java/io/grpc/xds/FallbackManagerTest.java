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
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

import io.grpc.Attributes;
import io.grpc.ChannelLogger;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;
import io.grpc.SynchronizationContext;
import io.grpc.internal.FakeClock;
import io.grpc.internal.ServiceConfigUtil.LbConfig;
import io.grpc.xds.XdsLbState.SubchannelStoreImpl;
import io.grpc.xds.XdsLoadBalancer.FallbackManager;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit test for {@link FallbackManager}.
 */
@RunWith(JUnit4.class)
public class FallbackManagerTest {

  private static final long FALLBACK_TIMEOUT_MS = TimeUnit.SECONDS.toMillis(10);

  private final FakeClock fakeClock = new FakeClock();
  private final LoadBalancerRegistry lbRegistry = new LoadBalancerRegistry();

  private final LoadBalancerProvider fakeLbProvider = new LoadBalancerProvider() {
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
      return "test_policy";
    }

    @Override
    public LoadBalancer newLoadBalancer(Helper helper) {
      return fakeLb;
    }
  };

  private final SynchronizationContext syncContext = new SynchronizationContext(
      new Thread.UncaughtExceptionHandler() {
        @Override
        public void uncaughtException(Thread t, Throwable e) {
          throw new AssertionError(e);
        }
      });

  @Mock
  private Helper helper;
  @Mock
  private LoadBalancer fakeLb;
  @Mock
  private ChannelLogger channelLogger;

  private FallbackManager fallbackManager;
  private LbConfig fallbackPolicy;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    doReturn(syncContext).when(helper).getSynchronizationContext();
    doReturn(fakeClock.getScheduledExecutorService()).when(helper).getScheduledExecutorService();
    doReturn(channelLogger).when(helper).getChannelLogger();
    fallbackManager = new FallbackManager(helper, new SubchannelStoreImpl(), lbRegistry);
    fallbackPolicy = new LbConfig("test_policy", new HashMap<String, Void>());
    lbRegistry.register(fakeLbProvider);
  }

  @After
  public void tearDown() {
    assertThat(fakeClock.getPendingTasks()).isEmpty();
  }

  @Test
  public void useFallbackWhenTimeout() {
    fallbackManager.maybeStartFallbackTimer();
    List<EquivalentAddressGroup> eags = new ArrayList<>();
    fallbackManager.updateFallbackServers(
        eags, Attributes.EMPTY, fallbackPolicy);

    verify(fakeLb, never()).handleResolvedAddressGroups(
        Matchers.<List<EquivalentAddressGroup>>any(), Matchers.<Attributes>any());

    fakeClock.forwardTime(FALLBACK_TIMEOUT_MS, TimeUnit.MILLISECONDS);

    verify(fakeLb).handleResolvedAddressGroups(
        same(eags),
        eq(Attributes.newBuilder()
            .set(LoadBalancer.ATTR_LOAD_BALANCING_CONFIG, fallbackPolicy.getRawConfigValue())
            .build()));
  }

  @Test
  public void cancelFallback() {
    fallbackManager.maybeStartFallbackTimer();
    List<EquivalentAddressGroup> eags = new ArrayList<>();
    fallbackManager.updateFallbackServers(
        eags, Attributes.EMPTY, fallbackPolicy);

    fallbackManager.cancelFallback();

    fakeClock.forwardTime(FALLBACK_TIMEOUT_MS, TimeUnit.MILLISECONDS);

    verify(fakeLb, never()).handleResolvedAddressGroups(
        Matchers.<List<EquivalentAddressGroup>>any(), Matchers.<Attributes>any());
  }
}
