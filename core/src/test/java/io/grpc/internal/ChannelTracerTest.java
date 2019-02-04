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

import io.grpc.ChannelLogger;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.ChannelTrace.Event;
import io.grpc.InternalChannelz.ChannelTrace.Event.Severity;
import io.grpc.InternalLogId;
import java.util.ArrayList;
import java.util.logging.Handler;
import java.util.logging.Level;
import java.util.logging.LogRecord;
import java.util.logging.Logger;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link ChannelTracer}.
 */
@RunWith(JUnit4.class)
public class ChannelTracerTest {
  private static final Logger logger = Logger.getLogger(ChannelLogger.class.getName());
  private final ArrayList<LogRecord> logs = new ArrayList<>();
  private final Handler handler = new Handler() {
      @Override
      public void publish(LogRecord record) {
        logs.add(record);
      }

      @Override
      public void flush() {
      }

      @Override
      public void close() throws SecurityException {
      }
    };

  private final InternalLogId logId = InternalLogId.allocate("test", /*details=*/ null);
  private final String logPrefix = "[" + logId + "] ";

  @Before
  public void setUp() {
    logger.addHandler(handler);
    logger.setLevel(Level.ALL);
  }

  @After
  public void tearDown() {
    logger.removeHandler(handler);
  }

  @Test
  public void channelTracerWithZeroMaxEvents() {
    ChannelTracer channelTracer =
        new ChannelTracer(logId, /* maxEvents= */ 0, /* channelCreationTimeNanos= */ 3L, "fooType");
    ChannelStats.Builder builder = new ChannelStats.Builder();
    Event e1 = new Event.Builder()
        .setDescription("e1").setSeverity(Severity.CT_ERROR).setTimestampNanos(1001).build();
    channelTracer.reportEvent(e1);

    channelTracer.updateBuilder(builder);
    ChannelStats stats = builder.build();
    assertThat(stats.channelTrace).isNull();

    assertThat(logs).hasSize(2);
    LogRecord log = logs.remove(0);
    assertThat(log.getMessage()).isEqualTo(logPrefix + "fooType created");
    assertThat(log.getLevel()).isEqualTo(Level.FINEST);
    log = logs.remove(0);
    assertThat(log.getMessage()).isEqualTo(logPrefix + "e1");
    assertThat(log.getLevel()).isEqualTo(Level.FINE);
  }

  @Test
  public void reportEvents() {
    ChannelTracer channelTracer =
        new ChannelTracer(logId, /* maxEvents= */ 2, /* channelCreationTimeNanos= */ 3L, "fooType");
    ChannelStats.Builder builder = new ChannelStats.Builder();
    Event e1 = new Event.Builder()
        .setDescription("e1").setSeverity(Severity.CT_ERROR).setTimestampNanos(1001).build();
    Event e2 = new Event.Builder()
        .setDescription("e2").setSeverity(Severity.CT_INFO).setTimestampNanos(1002).build();
    Event e3 = new Event.Builder()
        .setDescription("e3").setSeverity(Severity.CT_WARNING).setTimestampNanos(1003).build();
    Event e4 = new Event.Builder()
        .setDescription("e4").setSeverity(Severity.CT_UNKNOWN).setTimestampNanos(1004).build();

    // Check Channelz
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

    // Check logs
    assertThat(logs).hasSize(5);
    LogRecord log = logs.remove(0);
    assertThat(log.getMessage()).isEqualTo(logPrefix + "fooType created");
    assertThat(log.getLevel()).isEqualTo(Level.FINEST);

    log = logs.remove(0);
    assertThat(log.getMessage()).isEqualTo(logPrefix + "e1");
    assertThat(log.getLevel()).isEqualTo(Level.FINE);

    log = logs.remove(0);
    assertThat(log.getMessage()).isEqualTo(logPrefix + "e2");
    assertThat(log.getLevel()).isEqualTo(Level.FINEST);

    log = logs.remove(0);
    assertThat(log.getMessage()).isEqualTo(logPrefix + "e3");
    assertThat(log.getLevel()).isEqualTo(Level.FINER);

    log = logs.remove(0);
    assertThat(log.getMessage()).isEqualTo(logPrefix + "e4");
    assertThat(log.getLevel()).isEqualTo(Level.FINEST);
  }
}
