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

package io.grpc.cronet;

import io.grpc.CallOptions;

/** Call options for use with the Cronet transport. */
public final class CronetCallOptions {
  private CronetCallOptions() {}

  /**
   * Used for attaching annotation objects to Cronet streams. When the stream finishes, the user can
   * get Cronet metrics from {@link org.chromium.net.RequestFinishedInfo.Listener} with the same
   * annotation object.
   *
   * The Object must not be null.
   */
    public static final CallOptions.Key<Object> CRONET_ANNOTATION_KEY =
      CallOptions.Key.of("cronet-annotation", null);
}
