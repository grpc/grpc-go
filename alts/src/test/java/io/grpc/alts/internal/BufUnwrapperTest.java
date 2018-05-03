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

import static org.junit.Assert.assertEquals;

import com.google.common.truth.Truth;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.buffer.CompositeByteBuf;
import io.netty.buffer.UnpooledByteBufAllocator;
import java.nio.ByteBuffer;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class BufUnwrapperTest {

  private final ByteBufAllocator alloc = UnpooledByteBufAllocator.DEFAULT;

  @Test
  public void closeEmptiesBuffers() {
    BufUnwrapper unwrapper = new BufUnwrapper();
    ByteBuf buf = alloc.buffer();
    try {
      ByteBuffer[] readableBufs = unwrapper.readableNioBuffers(buf);
      ByteBuffer[] writableBufs = unwrapper.writableNioBuffers(buf);
      Truth.assertThat(readableBufs).hasLength(1);
      Truth.assertThat(readableBufs[0]).isNotNull();
      Truth.assertThat(writableBufs).hasLength(1);
      Truth.assertThat(writableBufs[0]).isNotNull();

      unwrapper.close();

      Truth.assertThat(readableBufs[0]).isNull();
      Truth.assertThat(writableBufs[0]).isNull();
    } finally {
      buf.release();
    }
  }

  @Test
  public void readableNioBuffers_worksWithNormal() {
    ByteBuf buf = alloc.buffer(1).writeByte('a');
    try (BufUnwrapper unwrapper = new BufUnwrapper()) {
      ByteBuffer[] internalBufs = unwrapper.readableNioBuffers(buf);
      Truth.assertThat(internalBufs).hasLength(1);

      assertEquals('a', internalBufs[0].get(0));
    } finally {
      buf.release();
    }
  }

  @Test
  public void readableNioBuffers_worksWithComposite() {
    CompositeByteBuf buf = alloc.compositeBuffer();
    buf.addComponent(true, alloc.buffer(1).writeByte('a'));
    try (BufUnwrapper unwrapper = new BufUnwrapper()) {
      ByteBuffer[] internalBufs = unwrapper.readableNioBuffers(buf);
      Truth.assertThat(internalBufs).hasLength(1);

      assertEquals('a', internalBufs[0].get(0));
    } finally {
      buf.release();
    }
  }

  @Test
  public void writableNioBuffers_indexesPreserved() {
    ByteBuf buf = alloc.buffer(1);
    int ridx = buf.readerIndex();
    int widx = buf.writerIndex();
    int cap = buf.capacity();
    try (BufUnwrapper unwrapper = new BufUnwrapper()) {
      ByteBuffer[] internalBufs = unwrapper.writableNioBuffers(buf);
      Truth.assertThat(internalBufs).hasLength(1);

      internalBufs[0].put((byte) 'a');

      assertEquals(ridx, buf.readerIndex());
      assertEquals(widx, buf.writerIndex());
      assertEquals(cap, buf.capacity());
    } finally {
      buf.release();
    }
  }

  @Test
  public void writableNioBuffers_worksWithNormal() {
    ByteBuf buf = alloc.buffer(1);
    try (BufUnwrapper unwrapper = new BufUnwrapper()) {
      ByteBuffer[] internalBufs = unwrapper.writableNioBuffers(buf);
      Truth.assertThat(internalBufs).hasLength(1);

      internalBufs[0].put((byte) 'a');

      buf.writerIndex(1);
      assertEquals('a', buf.readByte());
    } finally {
      buf.release();
    }
  }

  @Test
  public void writableNioBuffers_worksWithComposite() {
    CompositeByteBuf buf = alloc.compositeBuffer();
    buf.addComponent(alloc.buffer(1));
    buf.capacity(1);
    try (BufUnwrapper unwrapper = new BufUnwrapper()) {
      ByteBuffer[] internalBufs = unwrapper.writableNioBuffers(buf);
      Truth.assertThat(internalBufs).hasLength(1);

      internalBufs[0].put((byte) 'a');

      buf.writerIndex(1);
      assertEquals('a', buf.readByte());
    } finally {
      buf.release();
    }
  }
}
