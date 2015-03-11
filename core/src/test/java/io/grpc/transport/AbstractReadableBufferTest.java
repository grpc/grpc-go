/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc.transport;

import static org.junit.Assert.assertEquals;
import static org.mockito.Mockito.when;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.stubbing.OngoingStubbing;

/**
 * Tests for {@link AbstractReadableBuffer}.
 */
@RunWith(JUnit4.class)
public class AbstractReadableBufferTest {

  @Mock
  private AbstractReadableBuffer buffer;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);
  }

  @Test
  public void readUnsignedShortShouldSucceed() {
    mockBytes(0xFF, 0xEE);
    assertEquals(0xFFEE, buffer.readUnsignedShort());
  }

  @Test
  public void readUnsignedMediumShouldSucceed() {
    mockBytes(0xFF, 0xEE, 0xDD);
    assertEquals(0xFFEEDD, buffer.readUnsignedMedium());
  }

  @Test
  public void readPositiveIntShouldSucceed() {
    mockBytes(0x7F, 0xEE, 0xDD, 0xCC);
    assertEquals(0x7FEEDDCC, buffer.readInt());
  }

  @Test
  public void readNegativeIntShouldSucceed() {
    mockBytes(0xFF, 0xEE, 0xDD, 0xCC);
    assertEquals(0xFFEEDDCC, buffer.readInt());
  }

  private void mockBytes(int... bytes) {
    when(buffer.readableBytes()).thenReturn(bytes.length);
    OngoingStubbing<Integer> stub = when(buffer.readUnsignedByte());
    for (int b : bytes) {
      stub = stub.thenReturn(b);
    }
  }
}
