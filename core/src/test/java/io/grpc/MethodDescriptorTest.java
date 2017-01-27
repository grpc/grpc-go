/*
 * Copyright 2016, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
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
}
