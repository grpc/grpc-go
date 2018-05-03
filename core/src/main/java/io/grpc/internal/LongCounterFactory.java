/*
 * Copyright 2017 The gRPC Authors
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

/**
 * A factory for creating {@link LongCounter} objects. The concrete implementation returned may
 * be platform dependent.
 */
final class LongCounterFactory {
  /**
   * Creates a LongCounter.
   */
  public static LongCounter create() {
    if (ReflectionLongAdderCounter.isAvailable()) {
      return new ReflectionLongAdderCounter();
    } else {
      return new AtomicLongCounter();
    }
  }
}
