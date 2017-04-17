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
