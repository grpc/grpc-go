/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.okhttp;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.Header;
import io.grpc.okhttp.internal.framed.Settings;
import java.util.EnumMap;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;
import okio.Buffer;
import okio.ByteString;

class OkHttpFrameLogger {
  private static final int BUFFER_LENGTH_THRESHOLD = 64;
  private final Logger logger;
  private final Level level;

  OkHttpFrameLogger(Level level, Class<?> clazz) {
    this(level, Logger.getLogger(clazz.getName()));
  }

  @VisibleForTesting
  OkHttpFrameLogger(Level level, Logger logger) {
    this.level = checkNotNull(level, "level");
    this.logger = checkNotNull(logger, "logger");
  }

  private static String toString(Settings settings) {
    EnumMap<SettingParams, Integer> map = new EnumMap<>(SettingParams.class);
    for (SettingParams p : SettingParams.values()) {
      // Only log set parameters.
      if (settings.isSet(p.getBit())) {
        map.put(p, settings.get(p.getBit()));
      }
    }
    return map.toString();
  }

  private static String toString(Buffer buf) {
    if (buf.size() <= BUFFER_LENGTH_THRESHOLD) {
      // Log the entire buffer.
      return buf.snapshot().hex();
    }

    // Otherwise just log the first 64 bytes.
    int length = (int) Math.min(buf.size(), BUFFER_LENGTH_THRESHOLD);
    return buf.snapshot(length).hex() + "...";
  }

  private boolean isEnabled() {
    return logger.isLoggable(level);
  }

  void logData(Direction direction, int streamId, Buffer data, int length, boolean endStream) {
    if (isEnabled()) {
      logger.log(
          level,
          direction
              + " DATA: streamId="
              + streamId
              + " endStream="
              + endStream
              + " length="
              + length
              + " bytes="
              + toString(data));
    }
  }

  void logHeaders(Direction direction, int streamId, List<Header> headers, boolean endStream) {
    if (isEnabled()) {
      logger.log(
          level,
          direction
              + " HEADERS: streamId="
              + streamId
              + " headers="
              + headers
              + " endStream="
              + endStream);
    }
  }

  public void logPriority(
      Direction direction, int streamId, int streamDependency, int weight, boolean exclusive) {
    if (isEnabled()) {
      logger.log(
          level,
          direction
              + " PRIORITY: streamId="
              + streamId
              + " streamDependency="
              + streamDependency
              + " weight="
              + weight
              + " exclusive="
              + exclusive);
    }
  }

  void logRstStream(Direction direction, int streamId, ErrorCode errorCode) {
    if (isEnabled()) {
      logger.log(
          level, direction + " RST_STREAM: streamId=" + streamId + " errorCode=" + errorCode);
    }
  }

  void logSettingsAck(Direction direction) {
    if (isEnabled()) {
      logger.log(level, direction + " SETTINGS: ack=true");
    }
  }

  void logSettings(Direction direction, Settings settings) {
    if (isEnabled()) {
      logger.log(level, direction + " SETTINGS: ack=false settings=" + toString(settings));
    }
  }

  void logPing(Direction direction, long data) {
    if (isEnabled()) {
      logger.log(level, direction + " PING: ack=false bytes=" + data);
    }
  }

  void logPingAck(Direction direction, long data) {
    if (isEnabled()) {
      logger.log(level, direction + " PING: ack=true bytes=" + data);
    }
  }

  void logPushPromise(
      Direction direction, int streamId, int promisedStreamId, List<Header> headers) {
    if (isEnabled()) {
      logger.log(
          level,
          direction
              + " PUSH_PROMISE: streamId="
              + streamId
              + " promisedStreamId="
              + promisedStreamId
              + " headers="
              + headers);
    }
  }

  void logGoAway(Direction direction, int lastStreamId, ErrorCode errorCode, ByteString debugData) {
    if (isEnabled()) {
      logger.log(
          level,
          direction
              + " GO_AWAY: lastStreamId="
              + lastStreamId
              + " errorCode="
              + errorCode
              + " length="
              + debugData.size()
              + " bytes="
              + toString(new Buffer().write(debugData)));
    }
  }

  void logWindowsUpdate(Direction direction, int streamId, long windowSizeIncrement) {
    if (isEnabled()) {
      logger.log(
          level,
          direction
              + " WINDOW_UPDATE: streamId="
              + streamId
              + " windowSizeIncrement="
              + windowSizeIncrement);
    }
  }

  enum Direction {
    INBOUND,
    OUTBOUND
  }

  // Note the set bits in OkHttp's Settings are different from HTTP2 Specifications.
  private enum SettingParams {
    HEADER_TABLE_SIZE(1),
    ENABLE_PUSH(2),
    MAX_CONCURRENT_STREAMS(4),
    MAX_FRAME_SIZE(5),
    MAX_HEADER_LIST_SIZE(6),
    INITIAL_WINDOW_SIZE(7);

    private final int bit;

    SettingParams(int bit) {
      this.bit = bit;
    }

    public int getBit() {
      return this.bit;
    }
  }
}
