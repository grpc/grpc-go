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

import static com.google.common.truth.Truth.assertAbout;
import static com.google.common.truth.Truth.assertThat;
import static com.google.common.util.concurrent.MoreExecutors.directExecutor;
import static io.grpc.testing.DeadlineSubject.deadline;
import static java.util.concurrent.TimeUnit.MILLISECONDS;
import static java.util.concurrent.TimeUnit.MINUTES;
import static java.util.concurrent.TimeUnit.NANOSECONDS;
import static java.util.concurrent.TimeUnit.SECONDS;
import static org.mockito.Mockito.mock;

import com.google.common.base.Objects;

import io.grpc.Attributes.Key;
import io.grpc.internal.SerializingExecutor;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.concurrent.Executor;

/** Unit tests for {@link CallOptions}. */
@RunWith(JUnit4.class)
public class CallOptionsTest {
  private String sampleAuthority = "authority";
  private String sampleCompressor = "compressor";
  private Deadline.Ticker ticker = new DeadlineTest.FakeTicker();
  private Deadline sampleDeadline = Deadline.after(1, NANOSECONDS, ticker);
  private Key<String> sampleKey = Attributes.Key.of("sample");
  private Attributes sampleAffinity = Attributes.newBuilder().set(sampleKey, "blah").build();
  private CallCredentials sampleCreds = mock(CallCredentials.class);
  private CallOptions.Key<String> option1 = CallOptions.Key.of("option1", "default");
  private CallOptions.Key<String> option2 = CallOptions.Key.of("option2", "default");
  private CallOptions allSet = CallOptions.DEFAULT
      .withAuthority(sampleAuthority)
      .withDeadline(sampleDeadline)
      .withAffinity(sampleAffinity)
      .withCallCredentials(sampleCreds)
      .withCompression(sampleCompressor)
      .withWaitForReady()
      .withExecutor(directExecutor())
      .withOption(option1, "value1")
      .withOption(option2, "value2");

  @Test
  public void defaultsAreAllNull() {
    assertThat(CallOptions.DEFAULT.getDeadline()).isNull();
    assertThat(CallOptions.DEFAULT.getAuthority()).isNull();
    assertThat(CallOptions.DEFAULT.getAffinity()).isEqualTo(Attributes.EMPTY);
    assertThat(CallOptions.DEFAULT.getExecutor()).isNull();
    assertThat(CallOptions.DEFAULT.getCredentials()).isNull();
    assertThat(CallOptions.DEFAULT.getCompressor()).isNull();
    assertThat(CallOptions.DEFAULT.isWaitForReady()).isFalse();
  }

  @Test
  public void withAndWithoutWaitForReady() {
    assertThat(CallOptions.DEFAULT.withWaitForReady().isWaitForReady()).isTrue();
    assertThat(CallOptions.DEFAULT.withWaitForReady().withoutWaitForReady().isWaitForReady())
        .isFalse();
  }

  @Test
  public void allWiths() {
    assertThat(allSet.getAuthority()).isSameAs(sampleAuthority);
    assertThat(allSet.getDeadline()).isSameAs(sampleDeadline);
    assertThat(allSet.getAffinity()).isSameAs(sampleAffinity);
    assertThat(allSet.getCredentials()).isSameAs(sampleCreds);
    assertThat(allSet.getCompressor()).isSameAs(sampleCompressor);
    assertThat(allSet.getExecutor()).isSameAs(directExecutor());
    assertThat(allSet.getOption(option1)).isSameAs("value1");
    assertThat(allSet.getOption(option2)).isSameAs("value2");
    assertThat(allSet.isWaitForReady()).isTrue();
  }

  @Test
  public void noStrayModifications() {
    assertThat(equal(allSet, allSet.withAuthority("blah").withAuthority(sampleAuthority)))
        .isTrue();
    assertThat(
        equal(allSet,
            allSet.withDeadline(Deadline.after(314, NANOSECONDS)).withDeadline(sampleDeadline)))
        .isTrue();
    assertThat(
        equal(allSet,
            allSet.withAffinity(Attributes.EMPTY).withAffinity(sampleAffinity)))
        .isTrue();
    assertThat(
        equal(allSet,
            allSet.withCallCredentials(mock(CallCredentials.class))
            .withCallCredentials(sampleCreds)))
        .isTrue();
  }

  @Test
  public void mutation() {
    Deadline deadline = Deadline.after(10, SECONDS);
    CallOptions options1 = CallOptions.DEFAULT.withDeadline(deadline);
    assertThat(CallOptions.DEFAULT.getDeadline()).isNull();
    assertThat(deadline).isSameAs(options1.getDeadline());

    CallOptions options2 = options1.withDeadline(null);
    assertThat(deadline).isSameAs(options1.getDeadline());
    assertThat(options2.getDeadline()).isNull();
  }

  @Test
  public void mutateExecutor() {
    Executor executor = directExecutor();
    CallOptions options1 = CallOptions.DEFAULT.withExecutor(executor);
    assertThat(CallOptions.DEFAULT.getExecutor()).isNull();
    assertThat(executor).isSameAs(options1.getExecutor());

    CallOptions options2 = options1.withExecutor(null);
    assertThat(executor).isSameAs(options1.getExecutor());
    assertThat(options2.getExecutor()).isNull();
  }

  @Test
  public void withDeadlineAfter() {
    Deadline actual = CallOptions.DEFAULT.withDeadlineAfter(1, MINUTES).getDeadline();
    Deadline expected = Deadline.after(1, MINUTES);

    assertAbout(deadline()).that(actual).isWithin(10, MILLISECONDS).of(expected);
  }

  @Test
  public void toStringMatches_noDeadline_default() {
    String expected = "CallOptions{deadline=null, authority=authority, callCredentials=null, "
        + "affinity={sample=blah}, "
        + "executor=class io.grpc.internal.SerializingExecutor, compressorName=compressor, "
        + "customOptions=[[option1, value1], [option2, value2]], waitForReady=true}";
    String actual = allSet
        .withDeadline(null)
        .withExecutor(new SerializingExecutor(directExecutor()))
        .withCallCredentials(null)
        .toString();

    assertThat(actual).isEqualTo(expected);
  }

  @Test
  public void toStringMatches_noDeadline() {
    assertThat("CallOptions{deadline=null, authority=null, callCredentials=null, "
        + "affinity={}, executor=null, compressorName=null, customOptions=[], "
        + "waitForReady=false}")
        .isEqualTo(CallOptions.DEFAULT.toString());
  }

  @Test
  public void toStringMatches_withDeadline() {
    assertThat(allSet.toString()).contains("1 ns from now");
  }

  @Test
  @SuppressWarnings("deprecation")
  public void withDeadlineNanoTime() {
    // Create Deadline near calling System.nanoTime to reduce clock differences
    Deadline reference = Deadline.after(-1, NANOSECONDS);
    long rawDeadline = System.nanoTime() - 1;
    CallOptions opts = CallOptions.DEFAULT.withDeadlineNanoTime(rawDeadline);
    assertThat(opts.getDeadlineNanoTime()).isNotNull();
    // This is not technically correct, since nanoTime is permitted to overflow, but the chances of
    // that impacting this test are very remote.
    assertThat(opts.getDeadlineNanoTime()).isAtMost(System.nanoTime());
    assertThat(opts.getDeadline().isExpired()).isTrue();

    assertAbout(deadline()).that(opts.getDeadline()).isWithin(50, MILLISECONDS).of(reference);
  }

  @Test
  public void withCustomOptionDefault() {
    CallOptions opts = CallOptions.DEFAULT;
    assertThat(opts.getOption(option1)).isEqualTo("default");
  }
  
  @Test
  public void withCustomOption() {
    CallOptions opts = CallOptions.DEFAULT.withOption(option1, "v1");
    assertThat(opts.getOption(option1)).isEqualTo("v1");
  }
  
  @Test
  public void withCustomOptionLastOneWins() {
    CallOptions opts = CallOptions.DEFAULT.withOption(option1, "v1").withOption(option1, "v2");
    assertThat(opts.getOption(option1)).isEqualTo("v2");
  }
  
  @Test
  public void withMultipleCustomOption() {
    CallOptions opts = CallOptions.DEFAULT.withOption(option1, "v1").withOption(option2, "v2");
    assertThat(opts.getOption(option1)).isEqualTo("v1");
    assertThat(opts.getOption(option2)).isEqualTo("v2");
  }

  // TODO(carl-mastrangelo): consider making a CallOptionsSubject for Truth.
  private static boolean equal(CallOptions o1, CallOptions o2) {
    return Objects.equal(o1.getDeadline(), o2.getDeadline())
        && Objects.equal(o1.getAuthority(), o2.getAuthority())
        && Objects.equal(o1.getAffinity(), o2.getAffinity())
        && Objects.equal(o1.getCredentials(), o2.getCredentials());
  }
}
