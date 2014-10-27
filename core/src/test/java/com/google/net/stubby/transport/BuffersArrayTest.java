package com.google.net.stubby.transport;

import static com.google.common.base.Charsets.UTF_8;
import static com.google.net.stubby.transport.Buffers.wrap;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

/**
 * Tests for the array-backed {@link Buffer} returned by {@link Buffers#wrap(byte[], int, int)};
 */
public class BuffersArrayTest extends BufferTestBase {

  @Test
  public void bufferShouldExposeArray() {
    byte[] array = msg.getBytes(UTF_8);
    Buffer buffer = wrap(array, 1, msg.length() - 1);
    assertTrue(buffer.hasArray());
    assertSame(array, buffer.array());
    assertEquals(1, buffer.arrayOffset());

    // Now read a byte and verify that the offset changes.
    buffer.readUnsignedByte();
    assertEquals(2, buffer.arrayOffset());
  }

  @Override
  protected Buffer buffer() {
    return Buffers.wrap(msg.getBytes(UTF_8), 0, msg.length());
  }
}
