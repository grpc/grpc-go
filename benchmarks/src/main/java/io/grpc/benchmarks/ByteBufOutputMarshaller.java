/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.benchmarks;

import io.grpc.MethodDescriptor;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.EmptyByteBuf;
import io.netty.buffer.PooledByteBufAllocator;
import java.io.IOException;
import java.io.InputStream;

/**
 * Simple {@link io.grpc.MethodDescriptor.Marshaller} for Netty's {@link ByteBuf}.
 */
public class ByteBufOutputMarshaller implements MethodDescriptor.Marshaller<ByteBuf> {

  public static final EmptyByteBuf EMPTY_BYTE_BUF =
      new EmptyByteBuf(PooledByteBufAllocator.DEFAULT);

  @Override
  public InputStream stream(ByteBuf value) {
    return new ByteBufInputStream(value);
  }

  @Override
  public ByteBuf parse(InputStream stream) {
    try {
      // We don't do anything with the message and it's already been read into buffers
      // so just skip copying it.
      stream.skip(stream.available());
      return EMPTY_BYTE_BUF;
    } catch (IOException ioe) {
      throw new RuntimeException(ioe);
    }
  }
}
