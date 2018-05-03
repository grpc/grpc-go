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

import com.google.common.base.Preconditions;
import java.util.concurrent.atomic.AtomicLong;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A {@code long} atomically updated due to errors caused by the value being too small.
 */
@ThreadSafe
public final class AtomicBackoff {
  private static final Logger log = Logger.getLogger(AtomicBackoff.class.getName());

  private final String name;
  private final AtomicLong value = new AtomicLong();

  /** Construct an atomic with initial value {@code value}. {@code name} is used for logging. */
  public AtomicBackoff(String name, long value) {
    Preconditions.checkArgument(value > 0, "value must be positive");
    this.name = name;
    this.value.set(value);
  }

  /** Returns the current state. The state instance's value does not change of time. */
  public State getState() {
    return new State(value.get());
  }

  @ThreadSafe
  public final class State {
    private final long savedValue;

    private State(long value) {
      this.savedValue = value;
    }

    public long get() {
      return savedValue;
    }

    /**
     * Causes future invocations of {@link AtomicBackoff#getState} to have a value at least double
     * this state's value. Subsequent calls to this method will not increase the value further.
     */
    public void backoff() {
      // Use max to handle overflow
      long newValue = Math.max(savedValue * 2, savedValue);
      boolean swapped = value.compareAndSet(savedValue, newValue);
      // Even if swapped is false, the current value should be at least as large as newValue
      assert value.get() >= newValue;
      if (swapped) {
        log.log(Level.WARNING, "Increased {0} to {1}", new Object[] {name, newValue});
      }
    }
  }
}
