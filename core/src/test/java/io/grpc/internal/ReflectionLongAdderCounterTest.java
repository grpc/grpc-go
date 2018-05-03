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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class ReflectionLongAdderCounterTest {
  private ReflectionLongAdderCounter counter = new ReflectionLongAdderCounter();

  @Test
  public void testInitialValue() {
    assertEquals(0, counter.value());
  }

  @Test
  public void testIncrement() {
    counter.add(1);
    assertEquals(1, counter.value());
  }

  @Test
  public void testIncrementDelta() {
    counter.add(2);
    assertEquals(2, counter.value());
  }

  @Test
  public void testIncrementMulti() {
    counter.add(2);
    counter.add(1);
    assertEquals(3, counter.value());
  }

  @Test
  public void testDecrement() {
    counter.add(2);
    counter.add(-1);
    assertEquals(1, counter.value());
  }

  @Test
  public void testNegativeValue() {
    counter.add(-2);
    assertEquals(-2, counter.value());
  }
}
