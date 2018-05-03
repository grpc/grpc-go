/*
 * Copyright 2015 The gRPC Authors
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
import static org.junit.Assert.assertNotNull;

import java.util.Random;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Test for {@link ExponentialBackoffPolicy}.
 */
@RunWith(JUnit4.class)
public class ExponentialBackoffPolicyTest {
  private ExponentialBackoffPolicy policy = new ExponentialBackoffPolicy();
  private Random notRandom = new Random() {
    @Override
    public double nextDouble() {
      return .5;
    }
  };

  @Test
  public void maxDelayReached() {
    long maxBackoffNanos = 120 * 1000;
    policy.setMaxBackoffNanos(maxBackoffNanos)
        .setJitter(0)
        .setRandom(notRandom);
    for (int i = 0; i < 50; i++) {
      if (maxBackoffNanos == policy.nextBackoffNanos()) {
        return; // Success
      }
    }
    assertEquals("max delay not reached", maxBackoffNanos, policy.nextBackoffNanos());
  }

  @Test public void canProvide() {
    assertNotNull(new ExponentialBackoffPolicy.Provider().get());
  }
}

