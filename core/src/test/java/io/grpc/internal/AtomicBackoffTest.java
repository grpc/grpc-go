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

/** Unit tests for {@link AtomicBackoff}. */
@RunWith(JUnit4.class)
public class AtomicBackoffTest {
  @Test(expected = IllegalArgumentException.class)
  public void mustBePositive() {
    new AtomicBackoff("test", 0);
  }

  @Test
  public void backoff_doesNotChangeStateGet() {
    AtomicBackoff backoff = new AtomicBackoff("test", 8);
    AtomicBackoff.State state = backoff.getState();
    assertEquals(8, state.get());
    state.backoff();
    assertEquals(8, state.get());
  }

  @Test
  public void backoff_doubles() {
    AtomicBackoff backoff = new AtomicBackoff("test", 3);
    backoff.getState().backoff();
    assertEquals(6, backoff.getState().get());

    backoff.getState().backoff();
    assertEquals(12, backoff.getState().get());

    backoff.getState().backoff();
    assertEquals(24, backoff.getState().get());

    backoff = new AtomicBackoff("test", 13);
    backoff.getState().backoff();
    assertEquals(26, backoff.getState().get());
  }

  @Test
  public void backoff_oncePerState() {
    AtomicBackoff backoff = new AtomicBackoff("test", 8);
    AtomicBackoff.State state = backoff.getState();

    state.backoff();
    assertEquals(8, state.get());
    assertEquals(16, backoff.getState().get());
    state.backoff(); // Nothing happens the second time
    assertEquals(8, state.get());
    assertEquals(16, backoff.getState().get());
  }

  @Test
  public void backoff_twiceForEquivalentState_noChange() {
    AtomicBackoff backoff = new AtomicBackoff("test", 8);
    AtomicBackoff.State state1 = backoff.getState();
    AtomicBackoff.State state2 = backoff.getState();
    state1.backoff();
    state2.backoff();
    assertEquals(16, backoff.getState().get());
  }

  @Test
  public void backoff_delayed() {
    AtomicBackoff backoff = new AtomicBackoff("test", 8);
    AtomicBackoff.State state = backoff.getState();
    backoff.getState().backoff();
    backoff.getState().backoff();
    assertEquals(32, backoff.getState().get());

    // Shouldn't decrease value
    state.backoff();
    assertEquals(32, backoff.getState().get());
  }

  @Test
  public void largeLong() {
    // Don't use MAX_VALUE because it is easy to check for explicitly
    long tooLarge = Long.MAX_VALUE - 100;
    AtomicBackoff backoff = new AtomicBackoff("test", tooLarge);
    backoff.getState().backoff();
    // It would also be okay if it became MAX_VALUE
    assertEquals(tooLarge, backoff.getState().get());
  }
}
