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

import static org.junit.Assert.assertTrue;

import io.grpc.MethodDescriptor.MethodType;
import io.grpc.testing.TestMethodDescriptors;
import java.util.Arrays;
import java.util.Collection;
import java.util.Collections;
import java.util.List;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link ServiceDescriptor}.
 */
@RunWith(JUnit4.class)
public class ServiceDescriptorTest {

  @Rule
  public final ExpectedException thrown = ExpectedException.none();

  @Test
  public void failsOnNullName() {
    thrown.expect(NullPointerException.class);
    thrown.expectMessage("name");

    new ServiceDescriptor(null, Collections.<MethodDescriptor<?, ?>>emptyList());
  }

  @Test
  public void failsOnNullMethods() {
    thrown.expect(NullPointerException.class);
    thrown.expectMessage("methods");

    new ServiceDescriptor("name", (Collection<MethodDescriptor<?, ?>>) null);
  }

  @Test
  public void failsOnNullMethod() {
    thrown.expect(NullPointerException.class);
    thrown.expectMessage("method");

    new ServiceDescriptor("name", Collections.<MethodDescriptor<?, ?>>singletonList(null));
  }

  @Test
  public void failsOnNonMatchingNames() {
    List<MethodDescriptor<?, ?>> descriptors = Collections.<MethodDescriptor<?, ?>>singletonList(
        MethodDescriptor.<Void, Void>newBuilder()
          .setType(MethodType.UNARY)
          .setFullMethodName(MethodDescriptor.generateFullMethodName("wrongservice", "method"))
          .setRequestMarshaller(TestMethodDescriptors.voidMarshaller())
          .setResponseMarshaller(TestMethodDescriptors.voidMarshaller())
          .build());

    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("service names");

    new ServiceDescriptor("name", descriptors);
  }

  @Test
  public void failsOnNonDuplicateNames() {
    List<MethodDescriptor<?, ?>> descriptors = Arrays.<MethodDescriptor<?, ?>>asList(
        MethodDescriptor.<Void, Void>newBuilder()
          .setType(MethodType.UNARY)
          .setFullMethodName(MethodDescriptor.generateFullMethodName("name", "method"))
          .setRequestMarshaller(TestMethodDescriptors.voidMarshaller())
          .setResponseMarshaller(TestMethodDescriptors.voidMarshaller())
          .build(),
        MethodDescriptor.<Void, Void>newBuilder()
          .setType(MethodType.UNARY)
          .setFullMethodName(MethodDescriptor.generateFullMethodName("name", "method"))
          .setRequestMarshaller(TestMethodDescriptors.voidMarshaller())
          .setResponseMarshaller(TestMethodDescriptors.voidMarshaller())
          .build());

    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("duplicate");

    new ServiceDescriptor("name", descriptors);
  }

  @Test
  public void toStringTest() {
    ServiceDescriptor descriptor = new ServiceDescriptor("package.service",
        Arrays.<MethodDescriptor<?, ?>>asList(
        MethodDescriptor.<Void, Void>newBuilder()
          .setType(MethodType.UNARY)
          .setFullMethodName(MethodDescriptor.generateFullMethodName("package.service",
            "methodOne"))
          .setRequestMarshaller(TestMethodDescriptors.voidMarshaller())
          .setResponseMarshaller(TestMethodDescriptors.voidMarshaller())
          .build(),
        MethodDescriptor.<Void, Void>newBuilder()
          .setType(MethodType.UNARY)
          .setFullMethodName(MethodDescriptor.generateFullMethodName("package.service",
            "methodTwo"))
          .setRequestMarshaller(TestMethodDescriptors.voidMarshaller())
          .setResponseMarshaller(TestMethodDescriptors.voidMarshaller())
          .build()));

    String toString = descriptor.toString();
    assertTrue(toString.contains("ServiceDescriptor"));
    assertTrue(toString.contains("name=package.service"));
    assertTrue(toString.contains("package.service/methodOne"));
    assertTrue(toString.contains("package.service/methodTwo"));
  }
}
