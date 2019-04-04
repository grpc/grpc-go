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

package io.grpc.okhttp;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.FrameWriter;
import io.grpc.okhttp.internal.framed.Header;
import io.grpc.okhttp.internal.framed.Settings;
import java.io.IOException;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.logging.Level;
import java.util.logging.Logger;
import okio.Buffer;
import okio.ByteString;

final class ExceptionHandlingFrameWriter implements FrameWriter {

  private static final Logger log = Logger.getLogger(OkHttpClientTransport.class.getName());

  // Some exceptions are not very useful and add too much noise to the log
  private static final Set<String> QUIET_ERRORS =
      Collections.unmodifiableSet(new HashSet<>(Arrays.asList("Socket closed")));

  private final TransportExceptionHandler transportExceptionHandler;

  private final FrameWriter frameWriter;

  private final OkHttpFrameLogger frameLogger;

  ExceptionHandlingFrameWriter(
      TransportExceptionHandler transportExceptionHandler, FrameWriter frameWriter) {
    this(transportExceptionHandler, frameWriter,
        new OkHttpFrameLogger(Level.FINE, OkHttpClientTransport.class));
  }

  @VisibleForTesting
  ExceptionHandlingFrameWriter(
      TransportExceptionHandler transportExceptionHandler,
      FrameWriter frameWriter,
      OkHttpFrameLogger frameLogger) {
    this.transportExceptionHandler =
        checkNotNull(transportExceptionHandler, "transportExceptionHandler");
    this.frameWriter = Preconditions.checkNotNull(frameWriter, "frameWriter");
    this.frameLogger = Preconditions.checkNotNull(frameLogger, "frameLogger");
  }

  @Override
  public void connectionPreface() {
    try {
      frameWriter.connectionPreface();
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void ackSettings(Settings peerSettings) {
    frameLogger.logSettingsAck(OkHttpFrameLogger.Direction.OUTBOUND);
    try {
      frameWriter.ackSettings(peerSettings);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void pushPromise(int streamId, int promisedStreamId, List<Header> requestHeaders) {
    frameLogger.logPushPromise(OkHttpFrameLogger.Direction.OUTBOUND,
        streamId, promisedStreamId, requestHeaders);
    try {
      frameWriter.pushPromise(streamId, promisedStreamId, requestHeaders);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void flush() {
    try {
      frameWriter.flush();
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void synStream(
      boolean outFinished,
      boolean inFinished,
      int streamId,
      int associatedStreamId,
      List<Header> headerBlock) {
    try {
      frameWriter.synStream(outFinished, inFinished, streamId, associatedStreamId, headerBlock);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void synReply(boolean outFinished, int streamId,
      List<Header> headerBlock) {
    try {
      frameWriter.synReply(outFinished, streamId, headerBlock);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void headers(int streamId, List<Header> headerBlock) {
    frameLogger.logHeaders(OkHttpFrameLogger.Direction.OUTBOUND, streamId, headerBlock, false);
    try {
      frameWriter.headers(streamId, headerBlock);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void rstStream(int streamId, ErrorCode errorCode) {
    frameLogger.logRstStream(OkHttpFrameLogger.Direction.OUTBOUND, streamId, errorCode);
    try {
      frameWriter.rstStream(streamId, errorCode);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public int maxDataLength() {
    return frameWriter.maxDataLength();
  }

  @Override
  public void data(boolean outFinished, int streamId, Buffer source, int byteCount) {
    frameLogger.logData(OkHttpFrameLogger.Direction.OUTBOUND,
        streamId, source.buffer(), byteCount, outFinished);
    try {
      frameWriter.data(outFinished, streamId, source, byteCount);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void settings(Settings okHttpSettings) {
    frameLogger.logSettings(OkHttpFrameLogger.Direction.OUTBOUND, okHttpSettings);
    try {
      frameWriter.settings(okHttpSettings);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void ping(boolean ack, int payload1, int payload2) {
    frameLogger.logPing(OkHttpFrameLogger.Direction.OUTBOUND,
        ((long)payload1 << 32) | (payload2 & 0xFFFFFFFFL));
    try {
      frameWriter.ping(ack, payload1, payload2);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void goAway(int lastGoodStreamId, ErrorCode errorCode,
      byte[] debugData) {
    frameLogger.logGoAway(OkHttpFrameLogger.Direction.OUTBOUND,
        lastGoodStreamId, errorCode, ByteString.of(debugData));
    try {
      frameWriter.goAway(lastGoodStreamId, errorCode, debugData);
      // Flush it since after goAway, we are likely to close this writer.
      frameWriter.flush();
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void windowUpdate(int streamId, long windowSizeIncrement) {
    frameLogger.logWindowsUpdate(OkHttpFrameLogger.Direction.OUTBOUND,
        streamId, windowSizeIncrement);
    try {
      frameWriter.windowUpdate(streamId, windowSizeIncrement);
    } catch (IOException e) {
      transportExceptionHandler.onException(e);
    }
  }

  @Override
  public void close() {
    try {
      frameWriter.close();
    } catch (IOException e) {
      log.log(getLogLevel(e), "Failed closing connection", e);
    }
  }

  /**
   * Accepts a throwable and returns the appropriate logging level. Uninteresting exceptions
   * should not clutter the log.
   */
  @VisibleForTesting
  static Level getLogLevel(Throwable t) {
    if (t instanceof IOException
        && t.getMessage() != null
        && QUIET_ERRORS.contains(t.getMessage())) {
      return Level.FINE;
    }
    return Level.INFO;
  }

  /** A class that handles transport exception. */
  interface TransportExceptionHandler {
    /** Handles exception. */
    void onException(Throwable throwable);
  }
}
