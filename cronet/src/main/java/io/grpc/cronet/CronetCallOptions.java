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

package io.grpc.cronet;

import io.grpc.CallOptions;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;

/** Call options for use with the Cronet transport. */
public final class CronetCallOptions {
  private CronetCallOptions() {}

  /**
   * Used for attaching annotation objects to Cronet streams. When the stream finishes, the user can
   * get Cronet metrics from {@link org.chromium.net.RequestFinishedInfo.Listener} with the same
   * annotation object.
   *
   * <p>The Object must not be null.
   *
   * @deprecated Use {@link CronetCallOptions#withAnnotation} instead.
   */
  @Deprecated
  public static final CallOptions.Key<Object> CRONET_ANNOTATION_KEY =
      CallOptions.Key.create("cronet-annotation");

  /**
   * Returns a copy of {@code callOptions} with {@code annotation} included as one of the Cronet
   * annotation objects. When an RPC is made using a {@link CallOptions} instance returned by this
   * method, the annotation objects will be attached to the underlying Cronet bidirectional stream.
   * When the stream finishes, the user can retrieve the annotation objects via {@link
   * org.chromium.net.RequestFinishedInfo.Listener}.
   *
   * @param annotation the object to attach to the Cronet stream
   */
  public static CallOptions withAnnotation(CallOptions callOptions, Object annotation) {
    Collection<Object> existingAnnotations = callOptions.getOption(CRONET_ANNOTATIONS_KEY);
    ArrayList<Object> newAnnotations;
    if (existingAnnotations == null) {
      newAnnotations = new ArrayList<>();
    } else {
      newAnnotations = new ArrayList<>(existingAnnotations);
    }
    newAnnotations.add(annotation);
    return callOptions.withOption(
        CronetCallOptions.CRONET_ANNOTATIONS_KEY, Collections.unmodifiableList(newAnnotations));
  }

  static final CallOptions.Key<Collection<Object>> CRONET_ANNOTATIONS_KEY =
      CallOptions.Key.create("cronet-annotations");
}
