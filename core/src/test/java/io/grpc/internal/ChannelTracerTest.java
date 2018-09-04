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

import static com.google.common.truth.Truth.assertThat;

import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.ChannelTrace.Event;
import io.grpc.InternalChannelz.ChannelTrace.Event.Severity;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link ChannelTracer}.
 */
@RunWith(JUnit4.class)
public class ChannelTracerTest {
  @Rule
  public final ExpectedException thrown = ExpectedException.none();

  @Test
  public void channelTracerWithZeroMaxEventsShouldThrow() {
    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("maxEvents must be greater than zero");

    new ChannelTracer(/* maxEvents= */ 0, /* channelCreationTimeNanos= */ 3L, "fooType");
  }

  @Test
  public void reportEvents() {
    ChannelTracer channelTracer =
        new ChannelTracer(/* maxEvents= */ 2, /* channelCreationTimeNanos= */ 3L, "fooType");
    ChannelStats.Builder builder = new ChannelStats.Builder();
    Event e1 = new Event.Builder()
        .setDescription("e1").setSeverity(Severity.CT_ERROR).setTimestampNanos(1001).build();
    Event e2 = new Event.Builder()
        .setDescription("e2").setSeverity(Severity.CT_INFO).setTimestampNanos(1002).build();
    Event e3 = new Event.Builder()
        .setDescription("e3").setSeverity(Severity.CT_WARNING).setTimestampNanos(1003).build();
    Event e4 = new Event.Builder()
        .setDescription("e4").setSeverity(Severity.CT_UNKNOWN).setTimestampNanos(1004).build();

    channelTracer.updateBuilder(builder);
    ChannelStats stats = builder.build();
    assertThat(stats.channelTrace.events).hasSize(1);
    Event creationEvent = stats.channelTrace.events.get(0);
    assertThat(stats.channelTrace.numEventsLogged).isEqualTo(1);


    channelTracer.reportEvent(e1);
    channelTracer.updateBuilder(builder);
    stats = builder.build();

    assertThat(stats.channelTrace.events).containsExactly(creationEvent, e1);
    assertThat(stats.channelTrace.numEventsLogged).isEqualTo(2);

    channelTracer.reportEvent(e2);
    channelTracer.updateBuilder(builder);
    stats = builder.build();

    assertThat(stats.channelTrace.events).containsExactly(e1, e2);
    assertThat(stats.channelTrace.numEventsLogged).isEqualTo(3);

    channelTracer.reportEvent(e3);
    channelTracer.updateBuilder(builder);
    stats = builder.build();

    assertThat(stats.channelTrace.events).containsExactly(e2, e3);
    assertThat(stats.channelTrace.numEventsLogged).isEqualTo(4);

    channelTracer.reportEvent(e4);
    channelTracer.updateBuilder(builder);
    stats = builder.build();

    assertThat(stats.channelTrace.events).containsExactly(e3, e4);
    assertThat(stats.channelTrace.numEventsLogged).isEqualTo(5);
  }
}
