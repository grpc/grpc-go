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

import java.util.concurrent.TimeUnit;

/**
 * Time source representing the current system time in nanos. Used to inject a fake clock
 * into unit tests.
 */
public interface TimeProvider {
  /** Returns the current nano time. */
  long currentTimeNanos();

  TimeProvider SYSTEM_TIME_PROVIDER = new TimeProvider() {
    @Override
    public long currentTimeNanos() {
      return TimeUnit.MILLISECONDS.toNanos(System.currentTimeMillis());
    }
  };
}
