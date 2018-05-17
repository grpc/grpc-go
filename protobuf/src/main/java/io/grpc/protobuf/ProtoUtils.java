/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc.protobuf;

import com.google.protobuf.Message;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.protobuf.lite.ProtoLiteUtils;

/**
 * Utility methods for using protobuf with grpc.
 */
public class ProtoUtils {

  /** Create a {@code Marshaller} for protos of the same type as {@code defaultInstance}. */
  public static <T extends Message> Marshaller<T> marshaller(final T defaultInstance) {
    return ProtoLiteUtils.marshaller(defaultInstance);
  }

  /**
   * Produce a metadata key for a generated protobuf type.
   */
  public static <T extends Message> Metadata.Key<T> keyForProto(T instance) {
    return Metadata.Key.of(
        instance.getDescriptorForType().getFullName() + Metadata.BINARY_HEADER_SUFFIX,
        ProtoLiteUtils.metadataMarshaller(instance));
  }

  private ProtoUtils() {
  }
}
