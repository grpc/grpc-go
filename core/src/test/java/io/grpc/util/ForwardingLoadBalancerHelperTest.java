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

import io.grpc.EquivalentAddressGroup;
import io.grpc.ForwardingTestUtil;
import io.grpc.LoadBalancer;
import java.lang.reflect.Method;
import java.net.SocketAddress;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ForwardingLoadBalancerHelper}. */
@RunWith(JUnit4.class)
public class ForwardingLoadBalancerHelperTest {
  private final LoadBalancer.Helper mockDelegate = mock(LoadBalancer.Helper.class);

  private final class TestHelper extends ForwardingLoadBalancerHelper {
    @Override
    protected LoadBalancer.Helper delegate() {
      return mockDelegate;
    }
  }

  @Test
  public void allMethodsForwarded() throws Exception {
    final SocketAddress mockAddr = mock(SocketAddress.class);
    ForwardingTestUtil.testMethodsForwarded(
        LoadBalancer.Helper.class,
        mockDelegate,
        new TestHelper(),
        Collections.<Method>emptyList(),
        new ForwardingTestUtil.ArgumentProvider() {
          @Override
          public Object get(Method method, int argPos, Class<?> clazz) {
            if (clazz.equals(EquivalentAddressGroup.class)) {
              return new EquivalentAddressGroup(Arrays.asList(mockAddr));
            } else if (clazz.equals(List.class)) {
              return Collections.emptyList();
            }
            return null;
          }
        });
  }
}
