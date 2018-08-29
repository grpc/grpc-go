/*
 * Copyright 2018 The gRPC Authors
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

import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import com.google.common.collect.ImmutableSet;
import io.grpc.Status.Code;
import java.util.Collections;
import java.util.Set;
import javax.annotation.concurrent.Immutable;

/**
 * Hedging policy data object.
 */
@Immutable
final class HedgingPolicy {
  final int maxAttempts;
  final long hedgingDelayNanos;
  final Set<Code> nonFatalStatusCodes;

  /** No hedging. */
  static final HedgingPolicy DEFAULT =
      new HedgingPolicy(1, 0, Collections.<Code>emptySet());

  /**
   * The caller is supposed to have validated the arguments and handled throwing exception or
   * logging warnings already, so we avoid repeating args check here.
   */
  HedgingPolicy(int maxAttempts, long hedgingDelayNanos, Set<Code> nonFatalStatusCodes) {
    this.maxAttempts = maxAttempts;
    this.hedgingDelayNanos = hedgingDelayNanos;
    this.nonFatalStatusCodes = ImmutableSet.copyOf(nonFatalStatusCodes);
  }

  @Override
  public boolean equals(Object other) {
    if (this == other) {
      return true;
    }
    if (other == null || getClass() != other.getClass()) {
      return false;
    }
    HedgingPolicy that = (HedgingPolicy) other;
    return maxAttempts == that.maxAttempts
        && hedgingDelayNanos == that.hedgingDelayNanos
        && Objects.equal(nonFatalStatusCodes, that.nonFatalStatusCodes);
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(maxAttempts, hedgingDelayNanos, nonFatalStatusCodes);
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("maxAttempts", maxAttempts)
        .add("hedgingDelayNanos", hedgingDelayNanos)
        .add("nonFatalStatusCodes", nonFatalStatusCodes)
        .toString();
  }

  /**
   * Provides the most suitable hedging policy for a call.
   */
  interface Provider {

    /**
     * This method is used no more than once for each call. Never returns null.
     */
    HedgingPolicy get();
  }
}
