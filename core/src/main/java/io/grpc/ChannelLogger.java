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

package io.grpc;

import javax.annotation.concurrent.ThreadSafe;

/**
 * A Channel-specific logger provided by GRPC library to {@link LoadBalancer} implementations.
 * Information logged here goes to <strong>Channelz</strong>, and to the Java logger of this class
 * as well.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/5029")
@ThreadSafe
public abstract class ChannelLogger {
  /**
   * Log levels.  See the table below for the mapping from the ChannelLogger levels to Channelz
   * severity level (see {@code ChannelTraceEvent} from <a
   * href="https://github.com/grpc/grpc-java/blob/master/services/src/main/proto/grpc/channelz/v1/channelz.proto">channelz.proto</a>)
   * and Java logger levels.  Note that {@code DEBUG} level is not recorded on Channelz.
   * <pre>
   * +---------------------+-------------------+-------------------+
   * | ChannelLogger Level | Channelz Severity | Java Logger Level |
   * +---------------------+-------------------+-------------------+
   * | DEBUG               | N/A               | FINEST            |
   * | INFO                | CT_INFO           | FINEST            |
   * | WARNING             | CT_WARNING        | FINER             |
   * | ERROR               | CT_ERROR          | FINE              |
   * +---------------------+-------------------+-------------------+
   * </pre>
   */
  public enum ChannelLogLevel {
    DEBUG,
    INFO,
    WARNING,
    ERROR
  }

  /**
   * Logs a message.
   */
  public abstract void log(ChannelLogLevel level, String message);

  /**
   * Logs a message, using a message format and a list of arguments used to generate the log
   * message with {@link java.text.MessageFormat}.
   */
  public abstract void log(ChannelLogLevel level, String messageFormat, Object... args);
}
