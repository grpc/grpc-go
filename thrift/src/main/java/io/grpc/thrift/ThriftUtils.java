/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.thrift;

import io.grpc.ExperimentalApi;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.Status;
import io.grpc.internal.IoUtils;
import java.io.IOException;
import java.io.InputStream;
import org.apache.thrift.TBase;
import org.apache.thrift.TDeserializer;
import org.apache.thrift.TException;
import org.apache.thrift.TSerializer;

@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2170")
public final class ThriftUtils {

  /** Create a {@code Marshaller} for thrift messages. */
  public static <T extends TBase<T,?>> Marshaller<T> marshaller(final MessageFactory<T> factory) {

    return new Marshaller<T>() {

      @Override
      public InputStream stream(T value) {
        return new ThriftInputStream(value);
      }

      @Override
      public T parse(InputStream stream) {
        try {
          byte[] bytes = IoUtils.toByteArray(stream);
          TDeserializer deserializer = new TDeserializer();
          T message = factory.newInstance();
          deserializer.deserialize(message, bytes);
          return message;
        } catch (TException e) {
          throw Status.INTERNAL.withDescription("Invalid Stream")
              .withCause(e).asRuntimeException();
        } catch (IOException e) {
          throw Status.INTERNAL.withDescription("failed to read stream")
              .withCause(e).asRuntimeException();
        }
      }
    };
  }

  /** Produce a metadata marshaller. */
  public static <T extends TBase<T,?>> Metadata.BinaryMarshaller<T> metadataMarshaller(
      final MessageFactory<T> factory) {
    return new Metadata.BinaryMarshaller<T>() {

      @Override
      public byte[] toBytes(T value) {
        try {
          TSerializer serializer = new TSerializer();
          return serializer.serialize(value);
        } catch (TException e) {
          throw Status.INTERNAL.withDescription("Error in serializing Thrift Message")
              .withCause(e).asRuntimeException();
        }
      }

      @Override
      public T parseBytes(byte[] serialized) {
        try {
          TDeserializer deserializer = new TDeserializer();
          T message = factory.newInstance();
          deserializer.deserialize(message, serialized);
          return message;
        } catch (TException e) {
          throw Status.INTERNAL.withDescription("Invalid thrift Byte Sequence")
              .withCause(e).asRuntimeException();
        }
      }
    };
  }

  private ThriftUtils() {
  }

}
