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

package io.grpc.protobuf.nano;

import com.google.common.io.ByteStreams;
import com.google.protobuf.nano.CodedInputByteBufferNano;
import com.google.protobuf.nano.MessageNano;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.Status;
import java.io.IOException;
import java.io.InputStream;

/**
 * Utility methods for using nano proto with grpc.
 */
public class NanoUtils {

  private NanoUtils() {}

  /** Adapt {@code parser} to a {@code Marshaller}. */
  public static <T extends MessageNano> Marshaller<T> marshaller(
      final MessageNanoFactory<T> factory) {
    return new Marshaller<T>() {
      @Override
      public InputStream stream(T value) {
        return new NanoProtoInputStream(value);
      }

      @Override
      public T parse(InputStream stream) {
        try {
          // TODO(simonma): Investigate whether we can do 0-copy here. 
          CodedInputByteBufferNano input =
              CodedInputByteBufferNano.newInstance(ByteStreams.toByteArray(stream));
          input.setSizeLimit(Integer.MAX_VALUE);
          T message = factory.newInstance();
          message.mergeFrom(input);
          return message;
        } catch (IOException ipbe) {
          throw Status.INTERNAL.withDescription("Failed parsing nano proto message").withCause(ipbe)
              .asRuntimeException();
        }
      }
    };
  }
}
