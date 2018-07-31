/*
 * Copyright 2016 The gRPC Authors
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
 * The level of security guarantee in communications.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4692")
public enum SecurityLevel {
  /**
   * No security guarantee.
   */
  NONE,

  /**
   * The other party is authenticated and the data is not tampered with.
   */
  INTEGRITY,

  /**
   * In addition to {@code INTEGRITY}, the data is only visible to the intended communication
   * parties.
   */
  PRIVACY_AND_INTEGRITY
}
