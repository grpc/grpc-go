/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import io.grpc.MethodDescriptor.MethodType;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link MethodDescriptor}.
 */
@RunWith(JUnit4.class)
public class MethodDescriptorTest {
  @Test
  public void createMethodDescriptor() {
    @SuppressWarnings("deprecation") // MethodDescriptor.create
    MethodDescriptor<String, String> descriptor = MethodDescriptor.<String, String>create(
        MethodType.CLIENT_STREAMING, "/package.service/method", new StringMarshaller(),
        new StringMarshaller());
    assertEquals(MethodType.CLIENT_STREAMING, descriptor.getType());
    assertEquals("/package.service/method", descriptor.getFullMethodName());
    assertFalse(descriptor.isIdempotent());
    assertFalse(descriptor.isSafe());
  }

  @Test
  public void idempotent() {
    MethodDescriptor<String, String> descriptor = MethodDescriptor.<String, String>newBuilder()
        .setType(MethodType.SERVER_STREAMING)
        .setFullMethodName("/package.service/method")
        .setRequestMarshaller(new StringMarshaller())
        .setResponseMarshaller(new StringMarshaller())
        .build();

    assertFalse(descriptor.isIdempotent());

    // Create a new desriptor by setting idempotent to true
    MethodDescriptor<String, String> newDescriptor =
        descriptor.toBuilder().setIdempotent(true).build();
    assertTrue(newDescriptor.isIdempotent());
    // All other fields should staty the same
    assertEquals(MethodType.SERVER_STREAMING, newDescriptor.getType());
    assertEquals("/package.service/method", newDescriptor.getFullMethodName());
  }

  @Test
  public void safe() {
    MethodDescriptor<String, String> descriptor = MethodDescriptor.<String, String>newBuilder()
        .setType(MethodType.UNARY)
        .setFullMethodName("/package.service/method")
        .setRequestMarshaller(new StringMarshaller())
        .setResponseMarshaller(new StringMarshaller())
        .build();
    assertFalse(descriptor.isSafe());

    // Create a new desriptor by setting safe to true
    MethodDescriptor<String, String> newDescriptor = descriptor.toBuilder().setSafe(true).build();
    assertTrue(newDescriptor.isSafe());
    // All other fields should staty the same
    assertEquals(MethodType.UNARY, newDescriptor.getType());
    assertEquals("/package.service/method", newDescriptor.getFullMethodName());
  }

  @Test(expected = IllegalArgumentException.class)
  public void safeAndNonUnary() {
    MethodDescriptor<String, String> descriptor = MethodDescriptor.<String, String>newBuilder()
        .setType(MethodType.SERVER_STREAMING)
        .setFullMethodName("/package.service/method")
        .setRequestMarshaller(new StringMarshaller())
        .setResponseMarshaller(new StringMarshaller())
        .build();


    MethodDescriptor<String, String> discard = descriptor.toBuilder().setSafe(true).build();
    // Never reached
    assert discard == null;
  }

  @Test
  public void generateTraceSpanName() {
    assertEquals(
        "Sent.io.grpc.Foo", MethodDescriptor.generateTraceSpanName(false, "io.grpc/Foo"));
    assertEquals(
        "Recv.io.grpc.Bar", MethodDescriptor.generateTraceSpanName(true, "io.grpc/Bar"));
  }
}
