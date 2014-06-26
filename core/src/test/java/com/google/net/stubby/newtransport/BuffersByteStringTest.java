package com.google.net.stubby.newtransport;

import com.google.protobuf.ByteString;

/**
 * Tests for the array-backed {@link Buffer} returned by {@link Buffers#wrap(ByteString)}.
 */
public class BuffersByteStringTest extends BufferTestBase {

  @Override
  protected Buffer buffer() {
    return Buffers.wrap(ByteString.copyFromUtf8(msg));
  }
}
