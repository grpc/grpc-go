/*
 * Copyright 2015 The gRPC Authors
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
  private static final CallOptions.Key<String> OPTION_1
      = CallOptions.Key.createWithDefault("option1", "default");
  private static final CallOptions.Key<String> OPTION_2
      = CallOptions.Key.createWithDefault("option2", "default");
  private final String sampleAuthority = "authority";
  private final String sampleCompressor = "compressor";
  private final Deadline.Ticker ticker = new FakeTicker();
  private final Deadline sampleDeadline = Deadline.after(1, NANOSECONDS, ticker);
  private final CallCredentials sampleCreds = mock(CallCredentials.class);
  private final ClientStreamTracer.Factory tracerFactory1 = new FakeTracerFactory("tracerFactory1");
  private final ClientStreamTracer.Factory tracerFactory2 = new FakeTracerFactory("tracerFactory2");
  private final CallOptions allSet = CallOptions.DEFAULT
      .withAuthority(sampleAuthority)
      .withDeadline(sampleDeadline)
      .withCallCredentials(sampleCreds)
      .withCompression(sampleCompressor)
      .withWaitForReady()
      .withExecutor(directExecutor())
      .withOption(OPTION_1, "value1")
      .withStreamTracerFactory(tracerFactory1)
      .withOption(OPTION_2, "value2")
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
    assertThat(allSet.getOption(OPTION_1)).isSameAs("value1");
    assertThat(allSet.getOption(OPTION_2)).isSameAs("value2");
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
    assertThat(opts.getOption(OPTION_1)).isEqualTo("default");
  }

  @Test
  public void withCustomOption() {
    CallOptions opts = CallOptions.DEFAULT.withOption(OPTION_1, "v1");
    assertThat(opts.getOption(OPTION_1)).isEqualTo("v1");
  }

  @Test
  public void withCustomOptionLastOneWins() {
    CallOptions opts = CallOptions.DEFAULT.withOption(OPTION_1, "v1").withOption(OPTION_1, "v2");
    assertThat(opts.getOption(OPTION_1)).isEqualTo("v2");
  }

  @Test
  public void withMultipleCustomOption() {
    CallOptions opts = CallOptions.DEFAULT.withOption(OPTION_1, "v1").withOption(OPTION_2, "v2");
    assertThat(opts.getOption(OPTION_1)).isEqualTo("v1");
    assertThat(opts.getOption(OPTION_2)).isEqualTo("v2");
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
    public ClientStreamTracer newClientStreamTracer(CallOptions callOptions, Metadata headers) {
      return new ClientStreamTracer() {};
    }

    @Override
    public String toString() {
      return name;
    }
  }
}
