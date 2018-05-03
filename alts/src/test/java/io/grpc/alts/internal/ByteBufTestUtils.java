/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts.internal;

import com.google.common.base.Preconditions;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import java.security.SecureRandom;
import java.util.ArrayList;
import java.util.List;
import java.util.Random;

public final class ByteBufTestUtils {
  public interface RegisterRef {
    ByteBuf register(ByteBuf buf);
  }

  private static final Random random = new SecureRandom();

  // The {@code ref} argument can be used to register the buffer for {@code release}.
  // TODO: allow the allocator to be passed in.
  public static ByteBuf getDirectBuffer(int len, RegisterRef ref) {
    return ref.register(Unpooled.directBuffer(len));
  }

  /** Get random bytes. */
  public static ByteBuf getRandom(int len, RegisterRef ref) {
    ByteBuf buf = getDirectBuffer(len, ref);
    byte[] bytes = new byte[len];
    random.nextBytes(bytes);
    buf.writeBytes(bytes);
    return buf;
  }

  /** Fragment byte buffer into multiple pieces. */
  public static List<ByteBuf> fragmentByteBuf(ByteBuf in, int num, RegisterRef ref) {
    ByteBuf buf = in.slice();
    Preconditions.checkArgument(num > 0);
    List<ByteBuf> fragmentedBufs = new ArrayList<>(num);
    int fragmentSize = buf.readableBytes() / num;
    while (buf.isReadable()) {
      int readBytes = num == 0 ? buf.readableBytes() : fragmentSize;
      ByteBuf tmpBuf = getDirectBuffer(readBytes, ref);
      tmpBuf.writeBytes(buf, readBytes);
      fragmentedBufs.add(tmpBuf);
      num--;
    }
    return fragmentedBufs;
  }

  static ByteBuf writeSlice(ByteBuf in, int len) {
    Preconditions.checkArgument(len <= in.writableBytes());
    ByteBuf out = in.slice(in.writerIndex(), len);
    in.writerIndex(in.writerIndex() + len);
    return out.writerIndex(0);
  }
}
