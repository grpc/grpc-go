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

package io.grpc.services;

import com.google.protobuf.MessageLite;
import io.grpc.ExperimentalApi;
import java.io.Closeable;

/**
 * A class that accepts binary log messages.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4017")
public interface BinaryLogSink extends Closeable {
  /**
   * Writes the {@code message} to the destination.
   */
  void write(MessageLite message);
}
