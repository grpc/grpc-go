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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalChannelz.ChannelTrace;
import io.grpc.InternalChannelz.ChannelTrace.Event;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import javax.annotation.concurrent.GuardedBy;

/**
 * Tracks a collections of channel tracing events for a channel/subchannel.
 */
final class ChannelTracer {
  private final Object lock = new Object();
  @GuardedBy("lock")
  private final Collection<Event> events;
  private final long channelCreationTimeNanos;

  @GuardedBy("lock")
  private int eventsLogged;

  /**
   * Creates a channel tracer and log the creation event of the underlying channel.
   *
   * @param channelType Chennel, Subchannel, or OobChannel
   */
  ChannelTracer(final int maxEvents, long channelCreationTimeNanos, String channelType) {
    checkArgument(maxEvents > 0, "maxEvents must be greater than zero");
    checkNotNull(channelType, "channelType");
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
    this.channelCreationTimeNanos = channelCreationTimeNanos;

    reportEvent(new ChannelTrace.Event.Builder()
        .setDescription(channelType + " created")
        .setSeverity(ChannelTrace.Event.Severity.CT_INFO)
        // passing the timestamp in as a parameter instead of computing it right here because when
        // parent channel and subchannel both report the same event of the subchannel (e.g. creation
        // event of the subchannel) we want the timestamps to be exactly the same.
        .setTimestampNanos(channelCreationTimeNanos)
        .build());
  }

  void reportEvent(Event event) {
    synchronized (lock) {
      events.add(event);
    }
  }

  void updateBuilder(ChannelStats.Builder builder) {
    List<Event> eventsSnapshot;
    int eventsLoggedSnapshot;
    synchronized (lock) {
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
