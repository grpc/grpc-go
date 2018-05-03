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
 * Accesses internal data.  Do not use this.
 */
@Internal
public final class InternalStatus {
  private InternalStatus() {}

  /**
   * Key to bind status message to trailing metadata.
   */
  @Internal
  public static final Metadata.Key<String> MESSAGE_KEY = Status.MESSAGE_KEY;

  /**
   * Key to bind status code to trailing metadata.
   */
  @Internal
  public static final Metadata.Key<Status> CODE_KEY = Status.CODE_KEY;
}
