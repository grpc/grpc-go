package com.google.net.stubby.transport.netty;

import static com.google.common.base.Charsets.UTF_8;
import static com.google.net.stubby.transport.Buffers.readAsStringUtf8;
import static com.google.net.stubby.transport.TransportFrameUtil.COMPRESSION_HEADER_LENGTH;
import static com.google.net.stubby.transport.TransportFrameUtil.FLATE_FLAG;
import static com.google.net.stubby.transport.TransportFrameUtil.NO_COMPRESS_FLAG;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import com.google.net.stubby.transport.Buffer;
import com.google.net.stubby.transport.Buffers;
import com.google.net.stubby.transport.CompositeBuffer;

import io.netty.buffer.Unpooled;
import io.netty.buffer.UnpooledByteBufAllocator;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.util.Arrays;
import java.util.zip.DeflaterOutputStream;

/**
 * Tests for {@link NettyDecompressor}.
 */
@RunWith(JUnit4.class)
public class NettyDecompressorTest {
  private static final String MESSAGE = "hello world";

  private NettyDecompressor decompressor;

  @Before
  public void setup() {
    decompressor = new NettyDecompressor(UnpooledByteBufAllocator.DEFAULT);
  }

  @Test
  public void uncompressedDataShouldSucceed() throws Exception {
    fullMessageShouldSucceed(false);
  }

  @Test
  public void compressedDataShouldSucceed() throws Exception {
    fullMessageShouldSucceed(true);
  }

  @Test
  public void uncompressedFrameShouldNotBeReadableUntilComplete() throws Exception {
    byte[] frame = frame(false);
    byte[] chunk1 = Arrays.copyOf(frame, 5);
    byte[] chunk2 = Arrays.copyOfRange(frame, 5, frame.length);

    // Decompress the first chunk and verify it's not readable yet.
    decompressor.decompress(Buffers.wrap(chunk1));

    CompositeBuffer composite = new CompositeBuffer();
    Buffer buffer = decompressor.readBytes(2);
    assertEquals(1, buffer.readableBytes());
    composite.addBuffer(buffer);

    // Decompress the rest of the frame and verify it's readable.
    decompressor.decompress(Buffers.wrap(chunk2));
    composite.addBuffer(decompressor.readBytes(MESSAGE.length() - 1));
    assertEquals(MESSAGE, readAsStringUtf8(composite));
  }

  @Test
  public void compressedFrameShouldNotBeReadableUntilComplete() throws Exception {
    byte[] frame = frame(true);
    byte[] chunk1 = Arrays.copyOf(frame, 5);
    byte[] chunk2 = Arrays.copyOfRange(frame, 5, frame.length);

    // Decompress the first chunk and verify it's not readable yet.
    decompressor.decompress(Buffers.wrap(chunk1));
    Buffer buffer = decompressor.readBytes(2);
    assertNull(buffer);

    // Decompress the rest of the frame and verify it's readable.
    decompressor.decompress(Buffers.wrap(chunk2));
    CompositeBuffer composite = new CompositeBuffer();
    for(int remaining = MESSAGE.length(); remaining > 0; ) {
      Buffer buf = decompressor.readBytes(remaining);
      if (buf == null) {
        break;
      }
      composite.addBuffer(buf);
      remaining -= buf.readableBytes();
    }
    assertEquals(MESSAGE, readAsStringUtf8(composite));
  }

  @Test
  public void nettyBufferShouldBeReleasedAfterRead() throws Exception {
    byte[] frame = frame(false);
    byte[] chunk1 = Arrays.copyOf(frame, 5);
    byte[] chunk2 = Arrays.copyOfRange(frame, 5, frame.length);
    NettyBuffer buffer1 = new NettyBuffer(Unpooled.wrappedBuffer(chunk1));
    NettyBuffer buffer2 = new NettyBuffer(Unpooled.wrappedBuffer(chunk2));
    // CompositeByteBuf always keeps at least one buffer internally, so we add a second so
    // that it will release the first after it is read.
    decompressor.decompress(buffer1);
    decompressor.decompress(buffer2);
    NettyBuffer readBuffer = (NettyBuffer) decompressor.readBytes(buffer1.readableBytes());
    assertEquals(0, buffer1.buffer().refCnt());
    assertEquals(1, readBuffer.buffer().refCnt());
  }

  @Test
  public void closeShouldReleasedBuffers() throws Exception {
    byte[] frame = frame(false);
    byte[] chunk1 = Arrays.copyOf(frame, 5);
    NettyBuffer buffer1 = new NettyBuffer(Unpooled.wrappedBuffer(chunk1));
    decompressor.decompress(buffer1);
    assertEquals(1, buffer1.buffer().refCnt());
    decompressor.close();
    assertEquals(0, buffer1.buffer().refCnt());
  }

  private void fullMessageShouldSucceed(boolean compress) throws Exception {
    // Decompress the entire frame all at once.
    byte[] frame = frame(compress);
    decompressor.decompress(Buffers.wrap(frame));

    // Read some bytes and verify.
    int chunkSize = MESSAGE.length() / 2;
    assertEquals(MESSAGE.substring(0, chunkSize),
        readAsStringUtf8(decompressor.readBytes(chunkSize)));

    // Read the rest and verify.
    assertEquals(MESSAGE.substring(chunkSize),
        readAsStringUtf8(decompressor.readBytes(MESSAGE.length() - chunkSize)));
  }

  /**
   * Creates a compression frame from {@link #MESSAGE}, applying compression if requested.
   */
  private byte[] frame(boolean compress) throws Exception {
    byte[] msgBytes = bytes(MESSAGE);
    if (compress) {
      msgBytes = compress(msgBytes);
    }
    ByteArrayOutputStream os =
        new ByteArrayOutputStream(msgBytes.length + COMPRESSION_HEADER_LENGTH);
    DataOutputStream dos = new DataOutputStream(os);
    int frameFlag = compress ? FLATE_FLAG : NO_COMPRESS_FLAG;
    // Header = 1b flag | 3b length of GRPC frame
    int header = (frameFlag << 24) | msgBytes.length;
    dos.writeInt(header);
    dos.write(msgBytes);
    dos.close();
    return os.toByteArray();
  }

  private byte[] bytes(String str) {
    return str.getBytes(UTF_8);
  }

  private byte[] compress(byte[] data) throws Exception {
    ByteArrayOutputStream out = new ByteArrayOutputStream();
    DeflaterOutputStream dos = new DeflaterOutputStream(out);
    dos.write(data);
    dos.close();
    return out.toByteArray();
  }
}
