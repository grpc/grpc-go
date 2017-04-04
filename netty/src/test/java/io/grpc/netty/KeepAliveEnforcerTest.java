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

package io.grpc.netty;

import static com.google.common.truth.Truth.assertThat;

import java.util.concurrent.TimeUnit;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link KeepAliveEnforcer}. */
@RunWith(JUnit4.class)
public class KeepAliveEnforcerTest {
  private static final int LARGE_NUMBER = KeepAliveEnforcer.MAX_PING_STRIKES * 5;

  private FakeTicker ticker = new FakeTicker();

  @Test(expected = IllegalArgumentException.class)
  public void negativeTime() {
    new KeepAliveEnforcer(true, -1, TimeUnit.NANOSECONDS);
  }

  @Test(expected = NullPointerException.class)
  public void nullTimeUnit() {
    new KeepAliveEnforcer(true, 1, null);
  }

  @Test
  public void permitLimitless() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(true, 0, TimeUnit.NANOSECONDS, ticker);
    for (int i = 0; i < LARGE_NUMBER; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    enforcer.onTransportActive();
    for (int i = 0; i < LARGE_NUMBER; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    enforcer.onTransportIdle();
    for (int i = 0; i < LARGE_NUMBER; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    enforcer.resetCounters();
    for (int i = 0; i < LARGE_NUMBER; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
  }

  @Test
  public void strikeOutBecauseNoOutstandingCalls() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(false, 0, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportIdle();
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    assertThat(enforcer.pingAcceptable()).isFalse();
  }

  @Test
  public void startsIdle() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(false, 0, TimeUnit.NANOSECONDS, ticker);
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    assertThat(enforcer.pingAcceptable()).isFalse();
  }

  @Test
  public void strikeOutBecauseRateTooHighWhileActive() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(true, 1, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportActive();
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    assertThat(enforcer.pingAcceptable()).isFalse();
  }

  @Test
  public void strikeOutBecauseRateTooHighWhileIdle() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(true, 1, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportIdle();
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    assertThat(enforcer.pingAcceptable()).isFalse();
  }

  @Test
  public void permitInRateWhileActive() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(false, 1, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportActive();
    for (int i = 0; i < LARGE_NUMBER; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
      ticker.nanoTime += 1;
    }
  }

  @Test
  public void permitInRateWhileIdle() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(true, 1, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportIdle();
    for (int i = 0; i < LARGE_NUMBER; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
      ticker.nanoTime += 1;
    }
  }

  @Test
  public void implicitPermittedWhileIdle() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(
        false, KeepAliveEnforcer.IMPLICIT_PERMIT_TIME_NANOS * 10, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportIdle();
    for (int i = 0; i < LARGE_NUMBER; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
      ticker.nanoTime += KeepAliveEnforcer.IMPLICIT_PERMIT_TIME_NANOS;
    }
  }

  @Test
  public void implicitOverridesWhileActive() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(
        false, KeepAliveEnforcer.IMPLICIT_PERMIT_TIME_NANOS * 10, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportActive();
    for (int i = 0; i < LARGE_NUMBER; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
      ticker.nanoTime += KeepAliveEnforcer.IMPLICIT_PERMIT_TIME_NANOS;
    }
  }

  @Test
  public void implicitOverridesWhileIdle() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(
        true, KeepAliveEnforcer.IMPLICIT_PERMIT_TIME_NANOS * 10, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportIdle();
    for (int i = 0; i < LARGE_NUMBER; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
      ticker.nanoTime += KeepAliveEnforcer.IMPLICIT_PERMIT_TIME_NANOS;
    }
  }

  @Test
  public void permitsWhenTimeOverflows() {
    ticker.nanoTime = Long.MAX_VALUE;
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(false, 1, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportActive();
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    // Should have the maximum number of strikes now
    ticker.nanoTime++;
    assertThat(enforcer.pingAcceptable()).isTrue();
  }

  @Test
  public void resetCounters_resetsStrikes() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(false, 1, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportActive();
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    // Should have the maximum number of strikes now
    enforcer.resetCounters();
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
    assertThat(enforcer.pingAcceptable()).isFalse();
  }

  @Test
  public void resetCounters_resetsPingTime() {
    KeepAliveEnforcer enforcer = new KeepAliveEnforcer(false, 1, TimeUnit.NANOSECONDS, ticker);
    enforcer.onTransportActive();
    ticker.nanoTime += 1;
    assertThat(enforcer.pingAcceptable()).isTrue();
    enforcer.resetCounters();
    // Should not cause a strike
    assertThat(enforcer.pingAcceptable()).isTrue();
    for (int i = 0; i < KeepAliveEnforcer.MAX_PING_STRIKES; i++) {
      assertThat(enforcer.pingAcceptable()).isTrue();
    }
  }

  @Test
  public void systemTickerIsSystemNanoTime() {
    long before = System.nanoTime();
    long returned = KeepAliveEnforcer.SystemTicker.INSTANCE.nanoTime();
    long after = System.nanoTime();
    assertThat(returned).isAtLeast(before);
    assertThat(returned).isAtMost(after);
  }

  private static class FakeTicker implements KeepAliveEnforcer.Ticker {
    long nanoTime;

    @Override
    public long nanoTime() {
      return nanoTime;
    }
  }
}
