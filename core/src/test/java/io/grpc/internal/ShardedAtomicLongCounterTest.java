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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for ShardedAtomicLongCounter.
 */
@RunWith(JUnit4.class)
public class ShardedAtomicLongCounterTest {
  @Test
  public void power2() throws Exception {
    assertEquals(8, ShardedAtomicLongCounter.forcePower2(1));
    assertEquals(8, ShardedAtomicLongCounter.forcePower2(15));
    assertEquals(16, ShardedAtomicLongCounter.forcePower2(16));
    assertEquals(16, ShardedAtomicLongCounter.forcePower2(31));
    assertEquals(32, ShardedAtomicLongCounter.forcePower2(32));
    assertEquals(32, ShardedAtomicLongCounter.forcePower2(63));
    assertEquals(64, ShardedAtomicLongCounter.forcePower2(64));
    assertEquals(64, ShardedAtomicLongCounter.forcePower2(100));
  }

  @Test
  public void getCounterIdx() throws Exception {
    int[] shards = new int[] {8, 16, 32, 64};
    for (int shardCount : shards) {
      ShardedAtomicLongCounter shardedAtomicLongCounter = new ShardedAtomicLongCounter(shardCount);
      for (int i = 0; i < 4 * shardCount; i++) {
        assertEquals(i % shardCount, shardedAtomicLongCounter.getCounterIdx(i));
      }
    }
  }
}
