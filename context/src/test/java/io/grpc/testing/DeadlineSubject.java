/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.testing;

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Strings.lenientFormat;
import static com.google.common.truth.Fact.simpleFact;
import static java.util.concurrent.TimeUnit.NANOSECONDS;

import com.google.common.truth.ComparableSubject;
import com.google.common.truth.FailureMetadata;
import com.google.common.truth.Subject;
import io.grpc.Deadline;
import java.math.BigInteger;
import java.util.concurrent.TimeUnit;
import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;

/**
 * Propositions for {@link Deadline} subjects.
 */
public final class DeadlineSubject extends ComparableSubject<DeadlineSubject, Deadline> {
  private static final Subject.Factory<DeadlineSubject, Deadline> deadlineFactory =
      new Factory();

  public static Subject.Factory<DeadlineSubject, Deadline> deadline() {
    return deadlineFactory;
  }

  private DeadlineSubject(FailureMetadata metadata, Deadline subject) {
    super(metadata, subject);
  }

  /**
   * Prepares for a check that the subject is deadline within the given tolerance of an
   * expected value that will be provided in the next call in the fluent chain.
   */
  @CheckReturnValue
  public TolerantDeadlineComparison isWithin(final long delta, final TimeUnit timeUnit) {
    return new TolerantDeadlineComparison() {
      @Override
      public void of(Deadline expected) {
        Deadline actual = actual();
        checkNotNull(actual, "actual value cannot be null. expected=%s", expected);

        // This is probably overkill, but easier than thinking about overflow.
        BigInteger actualTimeRemaining = BigInteger.valueOf(actual.timeRemaining(NANOSECONDS));
        BigInteger expectedTimeRemaining = BigInteger.valueOf(expected.timeRemaining(NANOSECONDS));
        BigInteger deltaNanos = BigInteger.valueOf(timeUnit.toNanos(delta));
        if (actualTimeRemaining.subtract(expectedTimeRemaining).abs().compareTo(deltaNanos) > 0) {
          failWithoutActual(
              simpleFact(
                  lenientFormat(
                      "%s and <%s> should have been within <%sns> of each other",
                      actualAsString(), expected, deltaNanos)));
        }
      }
    };
  }

  // TODO(carl-mastrangelo):  Add a isNotWithin method once there is need for one.  Currently there
  // is no such method since there is no code that uses it, and would lower our coverage numbers.

  /**
   * A partially specified proposition about an approximate relationship to a {@code deadline}
   * subject using a tolerance.
   */
  public abstract static class TolerantDeadlineComparison {

    private TolerantDeadlineComparison() {}

    /**
     * Fails if the subject was expected to be within the tolerance of the given value but was not
     * <i>or</i> if it was expected <i>not</i> to be within the tolerance but was. The expectation,
     * subject, and tolerance are all specified earlier in the fluent call chain.
     */
    public abstract void of(Deadline expectedDeadline);

    /**
     * Do not call this method.
     *
     * @throws UnsupportedOperationException always
     * @deprecated {@link Object#equals(Object)} is not supported on TolerantDeadlineComparison
     *     If you meant to compare deadlines, use {@link #of(Deadline)} instead.
     */
    // Deprecation used to signal visual warning in IDE for the unaware users.
    // This method is created as a precaution and won't be removed as part of deprecation policy.
    @Deprecated
    @Override
    public boolean equals(@Nullable Object o) {
      throw new UnsupportedOperationException(
          "If you meant to compare deadlines, use .of(Deadline) instead.");
    }

    /**
     * Do not call this method.
     *
     * @throws UnsupportedOperationException always
     * @deprecated {@link Object#hashCode()} is not supported on TolerantDeadlineComparison
     */
    // Deprecation used to signal visual warning in IDE for the unaware users.
    // This method is created as a precaution and won't be removed as part of deprecation policy.
    @Deprecated
    @Override
    public int hashCode() {
      throw new UnsupportedOperationException("Subject.hashCode() is not supported.");
    }
  }

  private static final class Factory implements Subject.Factory<DeadlineSubject, Deadline>  {
    @Override
    public DeadlineSubject createSubject(FailureMetadata metadata, Deadline that) {
      return new DeadlineSubject(metadata, that);
    }
  }
}
