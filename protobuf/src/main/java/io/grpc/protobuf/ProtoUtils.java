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

import com.google.protobuf.ExtensionRegistry;
import com.google.protobuf.Message;
import io.grpc.ExperimentalApi;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.protobuf.lite.ProtoLiteUtils;

/**
 * Utility methods for using protobuf with grpc.
 */
public final class ProtoUtils {

  /**
   * Sets the global registry for proto marshalling shared across all servers and clients.
   *
   * <p>Warning:  This API will likely change over time.  It is not possible to have separate
   * registries per Process, Server, Channel, Service, or Method.  This is intentional until there
   * is a more appropriate API to set them.
   *
   * <p>Warning:  Do NOT modify the extension registry after setting it.  It is thread safe to call
   * {@link #setExtensionRegistry}, but not to modify the underlying object.
   *
   * <p>If you need custom parsing behavior for protos, you will need to make your own
   * {@code MethodDescriptor.Marshaller} for the time being.
   *
   * @since 1.16.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1787")
  public static void setExtensionRegistry(ExtensionRegistry registry) {
    ProtoLiteUtils.setExtensionRegistry(registry);
  }

  /**
   * Create a {@link Marshaller} for protos of the same type as {@code defaultInstance}.
   *
   * @since 1.0.0
   */
  public static <T extends Message> Marshaller<T> marshaller(final T defaultInstance) {
    return ProtoLiteUtils.marshaller(defaultInstance);
  }

  /**
   * Produce a metadata key for a generated protobuf type.
   *
   * @since 1.0.0
   */
  public static <T extends Message> Metadata.Key<T> keyForProto(T instance) {
    return Metadata.Key.of(
        instance.getDescriptorForType().getFullName() + Metadata.BINARY_HEADER_SUFFIX,
        metadataMarshaller(instance));
  }

  /**
   * Produce a metadata marshaller for a protobuf type.
   * 
   * @since 1.13.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4477")
  public static <T extends Message> Metadata.BinaryMarshaller<T> metadataMarshaller(T instance) {
    return ProtoLiteUtils.metadataMarshaller(instance);
  }

  private ProtoUtils() {
  }
}
