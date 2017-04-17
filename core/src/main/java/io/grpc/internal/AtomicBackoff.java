/*
 * Copyright 2017, Google Inc. All rights reserved.
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
