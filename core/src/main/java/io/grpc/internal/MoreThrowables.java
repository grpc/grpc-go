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

import com.google.common.base.Preconditions;

/** Utility functions when interacting with {@link Throwable}s. */
// TODO(ejona): Delete this once we've upgraded to Guava 20 or later.
public final class MoreThrowables {
  /**
   * Throws {code t} if it is an instance of {@link RuntimeException} or {@link Error}.
   *
   * <p>This is intended to mimic Guava's method by the same name, but which is unavailable to us
   * due to compatibility with older Guava versions.
   */
  public static void throwIfUnchecked(Throwable t) {
    Preconditions.checkNotNull(t);
    if (t instanceof RuntimeException) {
      throw (RuntimeException) t;
    }
    if (t instanceof Error) {
      throw (Error) t;
    }
  }

  // Prevent instantiation
  private MoreThrowables() {}
}
