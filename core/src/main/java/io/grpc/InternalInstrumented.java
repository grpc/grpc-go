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

package io.grpc;

import com.google.common.util.concurrent.ListenableFuture;

/**
 * An internal class. Do not use.
 *
 * <p>An interface for types that <b>may</b> support instrumentation. If the actual type does not
 * support instrumentation, then the future will return a {@code null}.
 */
@Internal
public interface InternalInstrumented<T> extends InternalWithLogId {

  /**
   * Returns the stats object.
   */
  ListenableFuture<T> getStats();
}
