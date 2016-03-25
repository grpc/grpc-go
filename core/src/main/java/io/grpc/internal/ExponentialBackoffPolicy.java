/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import static com.google.common.base.Preconditions.checkArgument;

import com.google.common.annotations.VisibleForTesting;

import java.util.Random;
import java.util.concurrent.TimeUnit;

/**
 * Retry Policy for Transport reconnection.  Initial parameters from
 * https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md
 *
 * <p>TODO(carl-mastrangelo): add unit tests for this class
 */
final class ExponentialBackoffPolicy implements BackoffPolicy {
  static final class Provider implements BackoffPolicy.Provider {
    @Override
    public BackoffPolicy get() {
      return new ExponentialBackoffPolicy();
    }
  }

  private Random random = new Random();
  private long initialBackoffMillis = TimeUnit.SECONDS.toMillis(1);
  private long maxBackoffMillis = TimeUnit.MINUTES.toMillis(2);
  private double multiplier = 1.6;
  private double jitter = .2;

  private long nextBackoffMillis = initialBackoffMillis;

  @Override
  public long nextBackoffMillis() {
    long currentBackoffMillis = nextBackoffMillis;
    nextBackoffMillis = Math.min((long) (currentBackoffMillis * multiplier), maxBackoffMillis);
    return currentBackoffMillis
        + uniformRandom(-jitter * currentBackoffMillis, jitter * currentBackoffMillis);
  }

  private long uniformRandom(double low, double high) {
    checkArgument(high >= low);
    double mag = high - low;
    return (long) (random.nextDouble() * mag + low);
  }

  /*
   * No guice and no flags means we get to implement these setters for testing ourselves.  Do not
   * call these from non-test code.
   */

  @VisibleForTesting
  ExponentialBackoffPolicy setRandom(Random random) {
    this.random = random;
    return this;
  }

  @VisibleForTesting
  ExponentialBackoffPolicy setInitialBackoffMillis(long initialBackoffMillis) {
    this.initialBackoffMillis = initialBackoffMillis;
    return this;
  }

  @VisibleForTesting
  ExponentialBackoffPolicy setMaxBackoffMillis(long maxBackoffMillis) {
    this.maxBackoffMillis = maxBackoffMillis;
    return this;
  }

  @VisibleForTesting
  ExponentialBackoffPolicy setMultiplier(double multiplier) {
    this.multiplier = multiplier;
    return this;
  }

  @VisibleForTesting
  ExponentialBackoffPolicy setJitter(double jitter) {
    this.jitter = jitter;
    return this;
  }
}

