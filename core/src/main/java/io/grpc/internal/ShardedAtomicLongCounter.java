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

import com.google.common.annotations.VisibleForTesting;
import java.util.concurrent.atomic.AtomicLongArray;

/**
 * An implementation of {@link LongCounter} that works by sharded across an array of
 * {@link AtomicLong} objects. Do not instantiate directly, instead use {@link LongCounterFactory}.
 */
final class ShardedAtomicLongCounter implements LongCounter {
  private final AtomicLongArray counters;
  private final int mask;

  /**
   * Accepts a hint on how many shards should be created. The actual number of shards may differ.
   */
  ShardedAtomicLongCounter(int numShardsHint) {
    int numShards = forcePower2(numShardsHint);
    counters = new AtomicLongArray(numShards);
    mask = numShards - 1;
  }

  /**
   * Force the shard size to a power of 2, with a reasonable ceiling value.
   * Let's avoid clever bit twiddling and keep it simple.
   */
  static int forcePower2(int numShardsHint) {
    if (numShardsHint >= 64) {
      return 64;
    } else if (numShardsHint >= 32) {
      return 32;
    } else if (numShardsHint >= 16) {
      return 16;
    } else {
      return 8;
    }
  }

  @VisibleForTesting
  int getCounterIdx(int hashCode) {
    return hashCode & mask;
  }

  @Override
  public void add(long delta) {
    // TODO(zpencer): replace fixed hashcode with a lightweight RNG. See guava's Striped64.
    counters.addAndGet(getCounterIdx(Thread.currentThread().hashCode()), delta);
  }

  @Override
  public long value() {
    long val = 0;
    for (int i = 0; i < counters.length(); i++) {
      val += counters.get(i);
    }
    return val;
  }
}
