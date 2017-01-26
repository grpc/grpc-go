/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
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
