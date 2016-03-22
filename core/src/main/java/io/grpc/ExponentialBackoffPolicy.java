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

package io.grpc;

import static com.google.common.base.MoreObjects.firstNonNull;
import static com.google.common.base.Preconditions.checkArgument;

import java.util.Random;
import java.util.concurrent.TimeUnit;

/**
 * Exponential backoff policy for reconnects.
 */
public final class ExponentialBackoffPolicy implements BackoffPolicy {
  /**
   * Provider tuned for connection retries.
   * https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md
   */
  public static final class Provider implements BackoffPolicy.Provider {
    @Override
    public BackoffPolicy get() {
      return ExponentialBackoffPolicy.builder()
          .initialBackoffMilis(1, TimeUnit.SECONDS)
          .maxBackoffMillis(2, TimeUnit.MINUTES)
          .multiplier(1.6)
          .jitter(.2)
          .build();
    }
  }

  private final Random random;
  private final long initialBackoffMillis;
  private final long maxBackoffMillis;
  private final double multiplier;
  private final double jitter;

  private long nextBackoffMillis;

  private ExponentialBackoffPolicy(Random random, long initialBackoffMilis, long maxBackoffMillis,
                                   double multiplier, double jitter) {
    this.random = random;
    this.initialBackoffMillis = initialBackoffMilis;
    this.maxBackoffMillis = maxBackoffMillis;
    this.multiplier = multiplier;
    this.jitter = jitter;

    this.nextBackoffMillis = initialBackoffMillis;
  }

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

  public static Builder builder() {
    return new Builder();
  }

  public static final class Builder {
    private Random random;
    private long initialBackoffMilis;
    private long maxBackoffMillis;
    private double multiplier;
    private double jitter;

    private Builder() {
    }

    public Builder random(Random random) {
      this.random = random;
      return this;
    }

    public Builder initialBackoffMilis(long initialBackoff, TimeUnit timeUnit) {
      this.initialBackoffMilis = timeUnit.toMillis(initialBackoff);
      return this;
    }

    public Builder maxBackoffMillis(long maxBackoff, TimeUnit timeUnit) {
      this.maxBackoffMillis = timeUnit.toMillis(maxBackoff);
      return this;
    }

    public Builder multiplier(double multiplier) {
      this.multiplier = multiplier;
      return this;
    }

    public Builder jitter(double jitter) {
      this.jitter = jitter;
      return this;
    }

    /**
     * Builds {@link BackoffPolicy} instance.
     *
     * @return BackoffPolicy object.
     */
    public ExponentialBackoffPolicy build() {
      checkArgument(initialBackoffMilis >= 0, "initialBackoffMilis must be greater or equal zero");
      checkArgument(maxBackoffMillis > 0, "maxBackoffMillis must be greater than zero");
      checkArgument(initialBackoffMilis <= maxBackoffMillis, "initialBackoffMilis can't be greater"
          + "or equal maxBackoffMillis");
      checkArgument(multiplier > 0, "multiplier must be greater than zero");
      checkArgument(jitter >= 0, "jitter must be greater or equal zero");

      return new ExponentialBackoffPolicy(firstNonNull(random, new Random()), initialBackoffMilis,
          maxBackoffMillis, multiplier, jitter);
    }
  }
}

