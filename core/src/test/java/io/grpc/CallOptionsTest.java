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

import static io.grpc.DeadlineTest.assertDeadlineEquals;
import static io.grpc.DeadlineTest.extractRemainingTime;
import static java.util.concurrent.TimeUnit.MILLISECONDS;
import static java.util.concurrent.TimeUnit.MINUTES;
import static java.util.concurrent.TimeUnit.NANOSECONDS;
import static java.util.concurrent.TimeUnit.SECONDS;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import com.google.common.base.Objects;
import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.Attributes.Key;
import io.grpc.internal.SerializingExecutor;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;

/** Unit tests for {@link CallOptions}. */
@RunWith(JUnit4.class)
public class CallOptionsTest {
  private String sampleAuthority = "authority";
  private Deadline sampleDeadline = Deadline.after(1, NANOSECONDS);
  private Key<String> sampleKey = Attributes.Key.of("sample");
  private Attributes sampleAffinity = Attributes.newBuilder().set(sampleKey, "blah").build();
  private CallOptions allSet = CallOptions.DEFAULT
      .withAuthority(sampleAuthority)
      .withDeadline(sampleDeadline)
      .withAffinity(sampleAffinity);

  @Test
  public void defaultsAreAllNull() {
    assertNull(CallOptions.DEFAULT.getDeadline());
    assertNull(CallOptions.DEFAULT.getAuthority());
    assertEquals(Attributes.EMPTY, CallOptions.DEFAULT.getAffinity());
    assertNull(CallOptions.DEFAULT.getExecutor());
  }

  @Test
  public void allWiths() {
    assertSame(sampleAuthority, allSet.getAuthority());
    assertSame(sampleDeadline, allSet.getDeadline());
    assertSame(sampleAffinity, allSet.getAffinity());
  }

  @Test
  public void noStrayModifications() {
    assertTrue(equal(allSet,
        allSet.withAuthority("blah").withAuthority(sampleAuthority)));
    assertTrue(equal(allSet,
        allSet.withDeadline(Deadline.after(314, NANOSECONDS))
            .withDeadline(sampleDeadline)));
    assertTrue(equal(allSet,
          allSet.withAffinity(Attributes.EMPTY).withAffinity(sampleAffinity)));
  }

  @Test
  public void mutation() {
    Deadline deadline = Deadline.after(10, SECONDS);
    CallOptions options1 = CallOptions.DEFAULT.withDeadline(deadline);
    assertNull(CallOptions.DEFAULT.getDeadline());
    assertEquals(deadline, options1.getDeadline());
    CallOptions options2 = options1.withDeadline(null);
    assertEquals(deadline, options1.getDeadline());
    assertNull(options2.getDeadline());
  }

  @Test
  public void mutateExecutor() {
    Executor executor = MoreExecutors.directExecutor();
    CallOptions options1 = CallOptions.DEFAULT.withExecutor(executor);
    assertNull(CallOptions.DEFAULT.getExecutor());
    assertSame(executor, options1.getExecutor());
    CallOptions options2 = options1.withExecutor(null);
    assertSame(executor, options1.getExecutor());
    assertNull(options2.getExecutor());
  }

  @Test
  public void withDeadlineAfter() {
    Deadline deadline = CallOptions.DEFAULT.withDeadlineAfter(1, MINUTES).getDeadline();
    Deadline expected = Deadline.after(1, MINUTES);
    assertDeadlineEquals(deadline, expected, 10, MILLISECONDS);
  }

  @Test
  public void toStringMatches() {
    assertEquals("CallOptions{deadline=null, authority=null, "
        + "affinity={}, executor=null, compressorName=null}", CallOptions.DEFAULT.toString());

    // Deadline makes it hard to check string for equality.
    assertEquals("CallOptions{deadline=null, authority=authority, "
        + "affinity={sample=blah}, executor=class io.grpc.internal.SerializingExecutor, "
        + "compressorName=null}",
        allSet.withDeadline(null)
            .withExecutor(new SerializingExecutor(MoreExecutors.directExecutor())).toString());

    assertDeadlineEquals(
        allSet.getDeadline(), extractRemainingTime(allSet.toString()), 20, MILLISECONDS);
  }

  @Test
  @SuppressWarnings("deprecation")
  public void withDeadlineNanoTime() {
    CallOptions opts = CallOptions.DEFAULT.withDeadlineNanoTime(System.nanoTime());
    assertNotNull(opts.getDeadlineNanoTime());
    assertTrue(opts.getDeadlineNanoTime() <= System.nanoTime());
    assertTrue(opts.getDeadline().isExpired());

    assertDeadlineEquals(opts.getDeadline(), Deadline.after(0, SECONDS), 20, TimeUnit.MILLISECONDS);
  }

  private static boolean equal(CallOptions o1, CallOptions o2) {
    return Objects.equal(o1.getDeadline(), o2.getDeadline())
        && Objects.equal(o1.getAuthority(), o2.getAuthority())
        && Objects.equal(o1.getAffinity(), o2.getAffinity());
  }
}
