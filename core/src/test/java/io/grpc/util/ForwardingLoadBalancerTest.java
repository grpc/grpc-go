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

package io.grpc.util;

import static org.mockito.Mockito.mock;

import io.grpc.ForwardingTestUtil;
import io.grpc.LoadBalancer;
import java.lang.reflect.Method;
import java.util.Collections;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ForwardingLoadBalancer}. */
@RunWith(JUnit4.class)
public class ForwardingLoadBalancerTest {
  private final LoadBalancer mockDelegate = mock(LoadBalancer.class);

  private final class TestBalancer extends ForwardingLoadBalancer {
    @Override
    protected LoadBalancer delegate() {
      return mockDelegate;
    }
  }

  @Test
  public void allMethodsForwarded() throws Exception {
    ForwardingTestUtil.testMethodsForwarded(
        LoadBalancer.class,
        mockDelegate,
        new TestBalancer(),
        Collections.<Method>emptyList());
  }
}
