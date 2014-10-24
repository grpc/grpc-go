package com.google.net.stubby.newtransport;

import static com.google.common.base.Charsets.UTF_8;

import java.nio.ByteBuffer;

/**
 * Tests for the array-backed {@link Buffer} returned by {@link Buffers#wrap(ByteBuffer)}.
 */
public class BuffersByteBufferTest extends BufferTestBase {

  @Override
  protected Buffer buffer() {
    return Buffers.wrap(ByteBuffer.wrap(msg.getBytes(UTF_8)));
  }
}
