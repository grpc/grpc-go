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

/**
 * An internal class. Do not use.
 *
 * <p>A loggable ID, unique for the duration of the program.
 */
@Internal
public interface InternalWithLogId {
  /**
   * Returns an ID that is primarily used in debug logs. It usually contains the class name and a
   * numeric ID that is unique among the instances.
   *
   * <p>The subclasses of this interface usually want to include the log ID in their {@link
   * #toString} results.
   */
  InternalLogId getLogId();
}
