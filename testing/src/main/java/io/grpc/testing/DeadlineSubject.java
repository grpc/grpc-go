/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.testing;

import static com.google.common.base.Preconditions.checkNotNull;
import static java.util.concurrent.TimeUnit.NANOSECONDS;

import com.google.common.truth.ComparableSubject;
import com.google.common.truth.FailureStrategy;
import com.google.common.truth.SubjectFactory;
import io.grpc.Deadline;
import java.math.BigInteger;
import java.util.concurrent.TimeUnit;
import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;

/**
 * Propositions for {@link Deadline} subjects.
 */
public final class DeadlineSubject extends ComparableSubject<DeadlineSubject, Deadline> {
  private static final SubjectFactory<DeadlineSubject, Deadline> deadlineFactory =
      new DeadlineSubjectFactory();

  public static SubjectFactory<DeadlineSubject, Deadline> deadline() {
    return deadlineFactory;
  }

  private DeadlineSubject(FailureStrategy failureStrategy, Deadline subject) {
    super(failureStrategy, subject);
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
        Deadline actual = getSubject();
        checkNotNull(actual, "actual value cannot be null. expected=%s", expected);

        // This is probably overkill, but easier than thinking about overflow.
        BigInteger actualTimeRemaining = BigInteger.valueOf(actual.timeRemaining(NANOSECONDS));
        BigInteger expectedTimeRemaining = BigInteger.valueOf(expected.timeRemaining(NANOSECONDS));
        BigInteger deltaNanos = BigInteger.valueOf(timeUnit.toNanos(delta));
        if (actualTimeRemaining.subtract(expectedTimeRemaining).abs().compareTo(deltaNanos) > 0) {
          failWithRawMessage(
              "%s and <%s> should have been within <%sns> of each other",
              getDisplaySubject(),
              expected,
              deltaNanos);
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

  private static final class DeadlineSubjectFactory
      extends SubjectFactory<DeadlineSubject, Deadline>  {
    @Override
    public DeadlineSubject getSubject(FailureStrategy fs, Deadline that) {
      return new DeadlineSubject(fs, that);
    }
  }
}
