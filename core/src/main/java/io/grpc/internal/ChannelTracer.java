/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

import com.google.common.annotations.VisibleForTesting;
import io.grpc.InternalChannelStats;

/**
 * A collection of channel level stats for channelz.
 */
final class ChannelTracer {
  private final TimeProvider timeProvider;
  private final LongCounter callsStarted = LongCounterFactory.create();
  private final LongCounter callsSucceeded = LongCounterFactory.create();
  private final LongCounter callsFailed = LongCounterFactory.create();
  private volatile long lastCallStartedMillis;

  ChannelTracer(TimeProvider timeProvider) {
    this.timeProvider = timeProvider;
  }

  public void reportCallStarted() {
    callsStarted.add(1);
    lastCallStartedMillis = timeProvider.currentTimeMillis();
  }

  public void reportCallEnded(boolean success) {
    if (success) {
      callsSucceeded.add(1);
    } else {
      callsFailed.add(1);
    }
  }

  public void updateBuilder(InternalChannelStats.Builder builder) {
    builder.setCallsStarted(callsStarted.value())
        .setCallsSucceeded(callsSucceeded.value())
        .setCallsFailed(callsFailed.value())
        .setLastCallStartedMillis(lastCallStartedMillis);
  }

  @VisibleForTesting
  interface TimeProvider {
    /** Returns the current milli time. */
    long currentTimeMillis();
  }

  public interface Factory {
    ChannelTracer create();
  }

  static final TimeProvider SYSTEM_TIME_PROVIDER = new TimeProvider() {
    @Override
    public long currentTimeMillis() {
      return System.currentTimeMillis();
    }
  };

  static final Factory DEFAULT_FACTORY = new Factory() {
    @Override
    public ChannelTracer create() {
      return new ChannelTracer(SYSTEM_TIME_PROVIDER);
    }
  };

  public static Factory getDefaultFactory() {
    return DEFAULT_FACTORY;
  }
}
