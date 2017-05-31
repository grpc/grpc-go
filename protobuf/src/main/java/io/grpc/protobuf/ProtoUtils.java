/*
 * Copyright 2014, gRPC Authors All rights reserved.
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

import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.Message;
import com.google.protobuf.Message.Builder;
import com.google.protobuf.util.JsonFormat;
import com.google.protobuf.util.JsonFormat.Parser;
import com.google.protobuf.util.JsonFormat.Printer;
import io.grpc.ExperimentalApi;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.Status;
import io.grpc.protobuf.lite.ProtoLiteUtils;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.Reader;
import java.nio.charset.Charset;

/**
 * Utility methods for using protobuf with grpc.
 */
public class ProtoUtils {

  /** Create a {@code Marshaller} for protos of the same type as {@code defaultInstance}. */
  public static <T extends Message> Marshaller<T> marshaller(final T defaultInstance) {
    return ProtoLiteUtils.marshaller(defaultInstance);
  }

  /**
   * Create a {@code Marshaller} for json protos of the same type as {@code defaultInstance}.
   *
   * <p>This is an unstable API and has not been optimized yet for performance.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1786")
  public static <T extends Message> Marshaller<T> jsonMarshaller(final T defaultInstance) {
    final Parser parser = JsonFormat.parser();
    final Printer printer = JsonFormat.printer();
    return jsonMarshaller(defaultInstance, parser, printer);
  }

  /**
   * Create a {@code Marshaller} for json protos of the same type as {@code defaultInstance}.
   *
   * <p>This is an unstable API and has not been optimized yet for performance.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1786")
  public static <T extends Message> Marshaller<T> jsonMarshaller(
      final T defaultInstance, final Parser parser, final Printer printer) {

    final Charset charset = Charset.forName("UTF-8");

    return new Marshaller<T>() {
      @Override
      public InputStream stream(T value) {
        try {
          return new ByteArrayInputStream(printer.print(value).getBytes(charset));
        } catch (InvalidProtocolBufferException e) {
          throw Status.INTERNAL
              .withCause(e)
              .withDescription("Unable to print json proto")
              .asRuntimeException();
        }
      }

      @SuppressWarnings("unchecked")
      @Override
      public T parse(InputStream stream) {
        Builder builder = defaultInstance.newBuilderForType();
        Reader reader = new InputStreamReader(stream, charset);
        T proto;
        try {
          parser.merge(reader, builder);
          proto = (T) builder.build();
          reader.close();
        } catch (InvalidProtocolBufferException e) {
          throw Status.INTERNAL.withDescription("Invalid protobuf byte sequence")
              .withCause(e).asRuntimeException();
        } catch (IOException e) {
          // Same for now, might be unavailable
          throw Status.INTERNAL.withDescription("Invalid protobuf byte sequence")
              .withCause(e).asRuntimeException();
        }
        return proto;
      }
    };
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
