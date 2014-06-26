package com.google.net.stubby.newtransport;

import static java.nio.charset.StandardCharsets.UTF_8;
import static org.junit.Assert.assertEquals;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.ByteArrayOutputStream;
import java.nio.ByteBuffer;
import java.util.Arrays;

/**
 * Abstract base class for tests of {@link Buffer} subclasses.
 */
@RunWith(JUnit4.class)
public abstract class BufferTestBase {
  protected final String msg = "hello";

  @Test
  public void bufferShouldReadAllBytes() {
    Buffer buffer = buffer();
    for (int ix = 0; ix < msg.length(); ++ix) {
      assertEquals(msg.length() - ix, buffer.readableBytes());
      assertEquals(msg.charAt(ix), buffer.readUnsignedByte());
    }
    assertEquals(0, buffer.readableBytes());
  }

  @Test
  public void readToArrayShouldSucceed() {
    Buffer buffer = buffer();
    byte[] array = new byte[msg.length()];
    buffer.readBytes(array, 0, array.length);
    Arrays.equals(msg.getBytes(UTF_8), array);
    assertEquals(0, buffer.readableBytes());
  }

  @Test
  public void partialReadToArrayShouldSucceed() {
    Buffer buffer = buffer();
    byte[] array = new byte[msg.length()];
    buffer.readBytes(array, 1, 2);
    Arrays.equals(new byte[] {'h', 'e'}, Arrays.copyOfRange(array, 1, 3));
    assertEquals(3, buffer.readableBytes());
  }

  @Test
  public void readToStreamShouldSucceed() throws Exception {
    Buffer buffer = buffer();
    ByteArrayOutputStream stream = new ByteArrayOutputStream();
    buffer.readBytes(stream, msg.length());
    Arrays.equals(msg.getBytes(UTF_8), stream.toByteArray());
    assertEquals(0, buffer.readableBytes());
  }

  @Test
  public void partialReadToStreamShouldSucceed() throws Exception {
    Buffer buffer = buffer();
    ByteArrayOutputStream stream = new ByteArrayOutputStream();
    buffer.readBytes(stream, 2);
    Arrays.equals(new byte[]{'h', 'e'}, Arrays.copyOfRange(stream.toByteArray(), 0, 2));
    assertEquals(3, buffer.readableBytes());
  }

  @Test
  public void readToByteBufferShouldSucceed() {
    Buffer buffer = buffer();
    ByteBuffer byteBuffer = ByteBuffer.allocate(msg.length());
    buffer.readBytes(byteBuffer);
    byteBuffer.flip();
    byte[] array = new byte[msg.length()];
    byteBuffer.get(array);
    Arrays.equals(msg.getBytes(UTF_8), array);
    assertEquals(0, buffer.readableBytes());
  }

  @Test
  public void partialReadToByteBufferShouldSucceed() {
    Buffer buffer = buffer();
    ByteBuffer byteBuffer = ByteBuffer.allocate(2);
    buffer.readBytes(byteBuffer);
    byteBuffer.flip();
    byte[] array = new byte[2];
    byteBuffer.get(array);
    Arrays.equals(new byte[]{'h', 'e'}, array);
    assertEquals(3, buffer.readableBytes());
  }

  protected abstract Buffer buffer();
}
