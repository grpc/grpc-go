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
import static org.junit.Assert.fail;
import static org.mockito.Mockito.mock;

import com.google.common.base.Objects;
import io.grpc.internal.SerializingExecutor;
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link CallOptions}. */
@RunWith(JUnit4.class)
public class CallOptionsTest {
  private String sampleAuthority = "authority";
  private String sampleCompressor = "compressor";
  private Deadline.Ticker ticker = new FakeTicker();
  private Deadline sampleDeadline = Deadline.after(1, NANOSECONDS, ticker);
  private CallCredentials sampleCreds = mock(CallCredentials.class);
  private CallOptions.Key<String> option1 = CallOptions.Key.of("option1", "default");
  private CallOptions.Key<String> option2 = CallOptions.Key.of("option2", "default");
  private ClientStreamTracer.Factory tracerFactory1 = new FakeTracerFactory("tracerFactory1");
  private ClientStreamTracer.Factory tracerFactory2 = new FakeTracerFactory("tracerFactory2");
  private CallOptions allSet = CallOptions.DEFAULT
      .withAuthority(sampleAuthority)
      .withDeadline(sampleDeadline)
      .withCallCredentials(sampleCreds)
      .withCompression(sampleCompressor)
      .withWaitForReady()
      .withExecutor(directExecutor())
      .withOption(option1, "value1")
      .withStreamTracerFactory(tracerFactory1)
      .withOption(option2, "value2")
      .withStreamTracerFactory(tracerFactory2);

  @Test
  public void defaultsAreAllNull() {
    assertThat(CallOptions.DEFAULT.getDeadline()).isNull();
    assertThat(CallOptions.DEFAULT.getAuthority()).isNull();
    assertThat(CallOptions.DEFAULT.getExecutor()).isNull();
    assertThat(CallOptions.DEFAULT.getCredentials()).isNull();
    assertThat(CallOptions.DEFAULT.getCompressor()).isNull();
    assertThat(CallOptions.DEFAULT.isWaitForReady()).isFalse();
    assertThat(CallOptions.DEFAULT.getStreamTracerFactories()).isEmpty();
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
    String actual = allSet
        .withDeadline(null)
        .withExecutor(new SerializingExecutor(directExecutor()))
        .withCallCredentials(null)
        .withMaxInboundMessageSize(44)
        .withMaxOutboundMessageSize(55)
        .toString();

    assertThat(actual).contains("deadline=null");
    assertThat(actual).contains("authority=authority");
    assertThat(actual).contains("callCredentials=null");
    assertThat(actual).contains("executor=class io.grpc.internal.SerializingExecutor");
    assertThat(actual).contains("compressorName=compressor");
    assertThat(actual).contains("customOptions=[[option1, value1], [option2, value2]]");
    assertThat(actual).contains("waitForReady=true");
    assertThat(actual).contains("maxInboundMessageSize=44");
    assertThat(actual).contains("maxOutboundMessageSize=55");
    assertThat(actual).contains("streamTracerFactories=[tracerFactory1, tracerFactory2]");
  }

  @Test
  public void toStringMatches_noDeadline() {
    String actual = CallOptions.DEFAULT.toString();
    assertThat(actual).contains("deadline=null");
  }

  @Test
  public void toStringMatches_withDeadline() {
    assertThat(allSet.toString()).contains("1 ns from now");
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

  @Test
  public void withStreamTracerFactory() {
    CallOptions opts1 = CallOptions.DEFAULT.withStreamTracerFactory(tracerFactory1);
    CallOptions opts2 = opts1.withStreamTracerFactory(tracerFactory2);
    CallOptions opts3 = opts2.withStreamTracerFactory(tracerFactory2);

    assertThat(opts1.getStreamTracerFactories()).containsExactly(tracerFactory1);
    assertThat(opts2.getStreamTracerFactories()).containsExactly(tracerFactory1, tracerFactory2)
        .inOrder();
    assertThat(opts3.getStreamTracerFactories())
        .containsExactly(tracerFactory1, tracerFactory2, tracerFactory2).inOrder();

    try {
      CallOptions.DEFAULT.getStreamTracerFactories().add(tracerFactory1);
      fail("Should have thrown. The list should be unmodifiable.");
    } catch (UnsupportedOperationException e) {
      // Expected
    }

    try {
      opts2.getStreamTracerFactories().clear();
      fail("Should have thrown. The list should be unmodifiable.");
    } catch (UnsupportedOperationException e) {
      // Expected
    }
  }

  // Only used in noStrayModifications()
  // TODO(carl-mastrangelo): consider making a CallOptionsSubject for Truth.
  private static boolean equal(CallOptions o1, CallOptions o2) {
    return Objects.equal(o1.getDeadline(), o2.getDeadline())
        && Objects.equal(o1.getAuthority(), o2.getAuthority())
        && Objects.equal(o1.getCredentials(), o2.getCredentials());
  }

  private static class FakeTicker extends Deadline.Ticker {
    private long time;

    @Override
    public long read() {
      return time;
    }

    public void reset(long time) {
      this.time = time;
    }

    public void increment(long period, TimeUnit unit) {
      if (period < 0) {
        throw new IllegalArgumentException();
      }
      this.time += unit.toNanos(period);
    }
  }

  private static class FakeTracerFactory extends ClientStreamTracer.Factory {
    final String name;

    FakeTracerFactory(String name) {
      this.name = name;
    }

    @Override
    public ClientStreamTracer newClientStreamTracer(Metadata headers) {
      return new ClientStreamTracer() {};
    }

    @Override
    public String toString() {
      return name;
    }
  }
}
