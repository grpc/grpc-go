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

package io.grpc.proto;

import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.Message;
import com.google.protobuf.MessageLite;
import com.google.protobuf.Parser;

import io.grpc.Marshaller;
import io.grpc.Metadata;
import io.grpc.Status;

import java.io.InputStream;

/**
 * Utility methods for using protobuf with grpc.
 */
public class ProtoUtils {

  public static <T extends MessageLite> Marshaller<T> marshaller(final Parser<T> parser) {
    return new Marshaller<T>() {
      @Override
      public InputStream stream(T value) {
        return new DeferredProtoInputStream(value);
      }

      @Override
      public T parse(InputStream stream) {
        try {
          return parser.parseFrom(stream);
        } catch (InvalidProtocolBufferException ipbe) {
          throw Status.INTERNAL.withDescription("Invalid protobuf byte sequence")
            .withCause(ipbe).asRuntimeException();
        }
      }
    };
  }

  /**
   * Produce a metadata key for a generated protobuf type.
   */
  public static <T extends Message> Metadata.Key<T> keyForProto(final T instance) {
    return Metadata.Key.of(
        instance.getDescriptorForType().getFullName() + Metadata.BINARY_HEADER_SUFFIX,
        new Metadata.BinaryMarshaller<T>() {
          @Override
          public byte[] toBytes(T value) {
            return value.toByteArray();
          }

          @Override
          @SuppressWarnings("unchecked")
          public T parseBytes(byte[] serialized) {
            try {
              return (T) instance.getParserForType().parseFrom(serialized);
            } catch (InvalidProtocolBufferException ipbe) {
              throw new IllegalArgumentException(ipbe);
            }
          }
        });
  }

  private ProtoUtils() {
  }
}
