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

package io.grpc.protobuf.nano;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.protobuf.nano.CodedInputByteBufferNano;
import com.google.protobuf.nano.MessageNano;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.Status;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;

/**
 * Utility methods for using nano proto with grpc.
 */
public final class NanoUtils {

  private NanoUtils() {}

  /**
   * Adapt {@code parser} to a {@link Marshaller}.
   *
   * @since 1.0.0
   */
  public static <T extends MessageNano> Marshaller<T> marshaller(MessageNanoFactory<T> factory) {
    return new MessageMarshaller<T>(factory);
  }

  private static final class MessageMarshaller<T extends MessageNano> implements Marshaller<T> {
    private static final int BUF_SIZE = 8192;

    private final MessageNanoFactory<T> factory;

    MessageMarshaller(MessageNanoFactory<T> factory) {
      this.factory = factory;
    }

    @Override
    public InputStream stream(T value) {
      return new NanoProtoInputStream(value);
    }

    @Override
    public T parse(InputStream stream) {
      try {
        // TODO(simonma): Investigate whether we can do 0-copy here.
        CodedInputByteBufferNano input =
            CodedInputByteBufferNano.newInstance(toByteArray(stream));
        input.setSizeLimit(Integer.MAX_VALUE);
        T message = factory.newInstance();
        message.mergeFrom(input);
        return message;
      } catch (IOException ipbe) {
        throw Status.INTERNAL.withDescription("Failed parsing nano proto message").withCause(ipbe)
            .asRuntimeException();
      }
    }

    // Copied from guava com.google.common.io.ByteStreams because its API is unstable (beta)
    private static byte[] toByteArray(InputStream in) throws IOException {
      ByteArrayOutputStream out = new ByteArrayOutputStream();
      copy(in, out);
      return out.toByteArray();
    }

    // Copied from guava com.google.common.io.ByteStreams because its API is unstable (beta)
    private static long copy(InputStream from, OutputStream to) throws IOException {
      checkNotNull(from);
      checkNotNull(to);
      byte[] buf = new byte[BUF_SIZE];
      long total = 0;
      while (true) {
        int r = from.read(buf);
        if (r == -1) {
          break;
        }
        to.write(buf, 0, r);
        total += r;
      }
      return total;
    }
  }
}
