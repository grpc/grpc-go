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

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.ChannelLogger;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.ChannelTrace;
import io.grpc.InternalChannelz.ChannelTrace.Event;
import io.grpc.InternalLogId;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.LogRecord;
import java.util.logging.Logger;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * Tracks a collections of channel tracing events for a channel/subchannel.
 */
final class ChannelTracer {
  // The logs go to ChannelLogger's logger so that user can control the logging level on that public
  // class rather than on this internal class.
  static final Logger logger = Logger.getLogger(ChannelLogger.class.getName());
  private final Object lock = new Object();
  private final InternalLogId logId;
  @GuardedBy("lock")
  @Nullable
  private final Collection<Event> events;
  private final long channelCreationTimeNanos;

  @GuardedBy("lock")
  private int eventsLogged;

  /**
   * Creates a channel tracer and log the creation event of the underlying channel.
   *
   * @param logId logId will be prepended to the logs logged to Java logger
   * @param maxEvents maximum number of events that are retained in memory.  If not a positive
   *        number no events will be retained, but they will still be sent to the Java logger.
   * @param channelCreationTimeNanos the creation time of the entity being traced
   * @param description a description of the entity being traced
   */
  ChannelTracer(
      InternalLogId logId, final int maxEvents, long channelCreationTimeNanos, String description) {
    checkNotNull(description, "description");
    this.logId = checkNotNull(logId, "logId");
    if (maxEvents > 0) {
      events = new ArrayDeque<Event>() {
          @GuardedBy("lock")
          @Override
          public boolean add(Event event) {
            if (size() == maxEvents) {
              removeFirst();
            }
            eventsLogged++;
            return super.add(event);
          }
        };
    } else {
      events = null;
    }
    this.channelCreationTimeNanos = channelCreationTimeNanos;

    reportEvent(new ChannelTrace.Event.Builder()
        .setDescription(description + " created")
        .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
        // passing the timestamp in as a parameter instead of computing it right here because when
        // parent channel and subchannel both report the same event of the subchannel (e.g. creation
        // event of the subchannel) we want the timestamps to be exactly the same.
        .setTimestampNanos(channelCreationTimeNanos)
        .build());
  }

  void reportEvent(Event event) {
    Level logLevel;
    switch (event.severity) {
      case CT_ERROR:
        logLevel = Level.FINE;
        break;
      case CT_WARNING:
        logLevel = Level.FINER;
        break;
      default:
        logLevel = Level.FINEST;
    }
    traceOnly(event);
    logOnly(logId, logLevel, event.description);
  }

  boolean isTraceEnabled() {
    synchronized (lock) {
      return events != null;
    }
  }

  void traceOnly(Event event) {
    synchronized (lock) {
      if (events != null) {
        events.add(event);
      }
    }
  }

  static void logOnly(InternalLogId logId, Level logLevel, String msg) {
    if (logger.isLoggable(logLevel)) {
      LogRecord lr = new LogRecord(logLevel, "[" + logId + "] " + msg);
      // No resource bundle as gRPC is not localized.
      lr.setLoggerName(logger.getName());
      lr.setSourceClassName(logger.getName());
      // Both logger methods are called log in ChannelLogger.
      lr.setSourceMethodName("log");
      logger.log(lr);
    }
  }

  InternalLogId getLogId() {
    return logId;
  }

  void updateBuilder(ChannelStats.Builder builder) {
    List<Event> eventsSnapshot;
    int eventsLoggedSnapshot;
    synchronized (lock) {
      if (events == null) {
        return;
      }
      eventsLoggedSnapshot = eventsLogged;
      eventsSnapshot = new ArrayList<>(events);
    }
    builder.setChannelTrace(new ChannelTrace.Builder()
        .setNumEventsLogged(eventsLoggedSnapshot)
        .setCreationTimeNanos(channelCreationTimeNanos)
        .setEvents(eventsSnapshot)
        .build());
  }
}
