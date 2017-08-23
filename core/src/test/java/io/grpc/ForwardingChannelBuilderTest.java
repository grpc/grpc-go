/*
 * Copyright 2017, gRPC Authors All rights reserved.
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
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import com.google.common.base.Defaults;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link ForwardingChannelBuilder}.
 */
@RunWith(JUnit4.class)
public class ForwardingChannelBuilderTest {
  private final ManagedChannelBuilder<?> mockDelegate = mock(ManagedChannelBuilder.class);

  private final ForwardingChannelBuilder<?> testChannelBuilder = new TestBuilder();

  private final class TestBuilder extends ForwardingChannelBuilder<TestBuilder> {
    @Override
    protected ManagedChannelBuilder<?> delegate() {
      return mockDelegate;
    }
  }

  @Test
  public void allBuilderMethodsForwarded() throws Exception {
    for (Method method : ManagedChannelBuilder.class.getDeclaredMethods()) {
      if (Modifier.isStatic(method.getModifiers()) || Modifier.isPrivate(method.getModifiers())) {
        continue;
      }
      Class<?>[] argTypes = method.getParameterTypes();
      Object[] args = new Object[argTypes.length];
      for (int i = 0; i < argTypes.length; i++) {
        args[i] = Defaults.defaultValue(argTypes[i]);
      }

      method.invoke(testChannelBuilder, args);
      method.invoke(verify(mockDelegate), args);
    }
  }

  @Test
  public void allBuilderMethodsReturnThis() throws Exception {
    for (Method method : ManagedChannelBuilder.class.getDeclaredMethods()) {
      if (Modifier.isStatic(method.getModifiers()) || Modifier.isPrivate(method.getModifiers())) {
        continue;
      }
      if (method.getName().equals("build")) {
        continue;
      }
      Class<?>[] argTypes = method.getParameterTypes();
      Object[] args = new Object[argTypes.length];
      for (int i = 0; i < argTypes.length; i++) {
        args[i] = Defaults.defaultValue(argTypes[i]);
      }

      Object returnedValue = method.invoke(testChannelBuilder, args);

      assertThat(returnedValue).isSameAs(testChannelBuilder);
    }
  }

  @Test
  public void buildReturnsDelegateBuildByDefualt() {
    ManagedChannel mockChannel = mock(ManagedChannel.class);
    doReturn(mockChannel).when(mockDelegate).build();

    assertThat(testChannelBuilder.build()).isSameAs(mockChannel);
  }
}
