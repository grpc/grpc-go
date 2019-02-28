/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.mockito.Mockito.when;

import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.junit.MockitoJUnit;
import org.mockito.junit.MockitoRule;
import org.mockito.stubbing.OngoingStubbing;

/**
 * Tests for {@link AbstractReadableBuffer}.
 */
@RunWith(JUnit4.class)
public class AbstractReadableBufferTest {

  @Rule
  public final MockitoRule mocks = MockitoJUnit.rule();

  @Mock
  private AbstractReadableBuffer buffer;

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
