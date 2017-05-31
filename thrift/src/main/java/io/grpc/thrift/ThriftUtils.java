/*
 * Copyright 2016, gRPC Authors All rights reserved.
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
