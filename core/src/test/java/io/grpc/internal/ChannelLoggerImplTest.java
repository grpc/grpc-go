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
import io.grpc.ChannelLogger.ChannelLogLevel;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.ChannelTrace.Event;
import io.grpc.InternalChannelz.ChannelTrace.Event.Severity;
import io.grpc.InternalLogId;
import java.util.ArrayList;
import java.util.logging.Handler;
import java.util.logging.LogRecord;
import java.util.logging.Logger;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link ChannelLoggerImpl}.
 */
@RunWith(JUnit4.class)
public class ChannelLoggerImplTest {
  private static final Logger javaLogger = Logger.getLogger(ChannelLogger.class.getName());
  private final FakeClock clock = new FakeClock();
  private final InternalLogId logId = InternalLogId.allocate("test", /*details=*/ null);
  private final String logPrefix = "[" + logId + "] ";
  private final ArrayList<String> logs = new ArrayList<>();
  private final Handler handler = new Handler() {
      @Override
      public void publish(LogRecord record) {
        logs.add(record.getLevel() + ": " + record.getMessage());
      }

      @Override
      public void flush() {
      }

      @Override
      public void close() throws SecurityException {
      }
    };

  @Before
  public void setUp() {
    javaLogger.addHandler(handler);
    javaLogger.setLevel(java.util.logging.Level.ALL);
  }

  @After
  public void tearDown() {
    javaLogger.removeHandler(handler);
  }

  @Test
  public void logging() {
    ChannelTracer tracer = new ChannelTracer(
        logId, /* maxEvents= */ 1, /* channelCreationTimeNanos= */ 3L, "fooType");
    ChannelLoggerImpl logger = new ChannelLoggerImpl(tracer, clock.getTimeProvider());
    ChannelStats.Builder builder = new ChannelStats.Builder();
    logs.clear();

    clock.forwardNanos(100);
    logger.log(ChannelLogLevel.ERROR, "Error message");
    tracer.updateBuilder(builder);
    ChannelStats stats = builder.build();
    Event event = new Event.Builder()
        .setDescription("Error message")
        .setSeverity(Severity.CT_ERROR)
        .setTimestampNanos(100)
        .build();
    assertThat(stats.channelTrace.events).containsExactly(event);
    assertThat(logs).contains("FINE: " + logPrefix + "Error message");

    clock.forwardNanos(100);
    logger.log(ChannelLogLevel.WARNING, "Warning message");
    tracer.updateBuilder(builder);
    stats = builder.build();
    event = new Event.Builder()
        .setDescription("Warning message")
        .setSeverity(Severity.CT_WARNING)
        .setTimestampNanos(200)
        .build();
    assertThat(stats.channelTrace.events).containsExactly(event);
    assertThat(logs).contains("FINER: " + logPrefix + "Warning message");

    clock.forwardNanos(100);
    logger.log(ChannelLogLevel.INFO, "Info message");
    tracer.updateBuilder(builder);
    stats = builder.build();
    event = new Event.Builder()
        .setDescription("Info message")
        .setSeverity(Severity.CT_INFO)
        .setTimestampNanos(300)
        .build();
    assertThat(stats.channelTrace.events).containsExactly(event);
    assertThat(logs).contains("FINEST: " + logPrefix + "Info message");

    clock.forwardNanos(100);
    logger.log(ChannelLogLevel.DEBUG, "Debug message");
    tracer.updateBuilder(builder);
    stats = builder.build();
    // DEBUG level messages are not logged to channelz, thus channelz still has the
    // last event.
    assertThat(stats.channelTrace.events).containsExactly(event);
    assertThat(logs).contains("FINEST: " + logPrefix + "Debug message");
  }

  @Test
  public void formatLogging() {
    ChannelTracer tracer = new ChannelTracer(
        logId, /* maxEvents= */ 1, /* channelCreationTimeNanos= */ 3L, "fooType");
    ChannelLoggerImpl logger = new ChannelLoggerImpl(tracer, clock.getTimeProvider());
    ChannelStats.Builder builder = new ChannelStats.Builder();
    logs.clear();

    clock.forwardNanos(100);
    logger.log(ChannelLogLevel.ERROR, "Error message {0}", "foo");
    tracer.updateBuilder(builder);
    ChannelStats stats = builder.build();
    Event event = new Event.Builder()
        .setDescription("Error message foo")
        .setSeverity(Severity.CT_ERROR)
        .setTimestampNanos(100)
        .build();
    assertThat(stats.channelTrace.events).containsExactly(event);
    assertThat(logs).contains("FINE: " + logPrefix + "Error message foo");

    clock.forwardNanos(100);
    logger.log(ChannelLogLevel.WARNING, "Warning message {0}, {1}", "foo", "bar");
    tracer.updateBuilder(builder);
    stats = builder.build();
    event = new Event.Builder()
        .setDescription("Warning message foo, bar")
        .setSeverity(Severity.CT_WARNING)
        .setTimestampNanos(200)
        .build();
    assertThat(stats.channelTrace.events).containsExactly(event);
    assertThat(logs).contains("FINER: " + logPrefix + "Warning message foo, bar");

    clock.forwardNanos(100);
    logger.log(ChannelLogLevel.INFO, "Info message {0}", "bar");
    tracer.updateBuilder(builder);
    stats = builder.build();
    event = new Event.Builder()
        .setDescription("Info message bar")
        .setSeverity(Severity.CT_INFO)
        .setTimestampNanos(300)
        .build();
    assertThat(stats.channelTrace.events).containsExactly(event);
    assertThat(logs).contains("FINEST: " + logPrefix + "Info message bar");

    clock.forwardNanos(100);
    logger.log(ChannelLogLevel.DEBUG, "Debug message {0}", "foo");
    tracer.updateBuilder(builder);
    stats = builder.build();
    // DEBUG level messages are not logged to channelz, thus channelz still has the
    // last event.
    assertThat(stats.channelTrace.events).containsExactly(event);
    assertThat(logs).contains("FINEST: " + logPrefix + "Debug message foo");
  }
}
