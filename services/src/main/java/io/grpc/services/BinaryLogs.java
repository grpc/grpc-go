/*
 * Copyright 2018, gRPC Authors All rights reserved.
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

package io.grpc.services;

import io.grpc.BinaryLog;
import io.grpc.ExperimentalApi;
import java.io.IOException;

@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4017")
public final class BinaryLogs {
  /**
   * Creates a binary log that writes to a temp file. <b>Warning:</b> this implementation is
   * not performance optimized, and RPCs will experience back pressure if disk IO does not keep
   * up.
   */
  public static BinaryLog createBinaryLog() throws IOException {
    return new BinaryLogProviderImpl();
  }

  /**
   * Deprecated and will be removed in a future version of gRPC.
   */
  @Deprecated
  public static BinaryLog createBinaryLog(BinaryLogSink sink) throws IOException {
    return new BinaryLogProviderImpl(sink);
  }

  /**
   * Creates a binary log with a custom {@link BinaryLogSink} for receiving the logged data,
   * and a config string as defined by
   * <a href="https://github.com/grpc/proposal/blob/master/A16-binary-logging.md">
   *   A16-binary-logging</a>.
   */
  public static BinaryLog createBinaryLog(BinaryLogSink sink, String configStr) throws IOException {
    return new BinaryLogProviderImpl(sink, configStr);
  }

  private BinaryLogs() {}
}
