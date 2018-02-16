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

package io.grpc.services;

import io.grpc.InternalServiceProviders;
import java.util.Collections;
import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Subclasses must be thread safe, and are responsible for writing the binary log message to
 * the appropriate destination.
 */
@ThreadSafe
final class BinaryLogSinkProvider {
  private static final BinaryLogSink INSTANCE = InternalServiceProviders.load(
      BinaryLogSink.class,
      Collections.<Class<?>>emptyList(),
      BinaryLogSinkProvider.class.getClassLoader(),
      new InternalServiceProviders.PriorityAccessor<BinaryLogSink>() {
        @Override
        public boolean isAvailable(BinaryLogSink provider) {
          return provider.isAvailable();
        }

        @Override
        public int getPriority(BinaryLogSink provider) {
          return provider.priority();
        }
      });

  /**
   * Returns the {@code BinaryLogSink} that should be used.
   */
  @Nullable
  static BinaryLogSink provider() {
    return INSTANCE;
  }
}
