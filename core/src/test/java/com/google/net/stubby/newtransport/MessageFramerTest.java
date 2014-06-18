package com.google.net.stubby.newtransport;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import com.google.common.io.ByteBuffers;
import com.google.common.primitives.Bytes;
import com.google.net.stubby.GrpcFramingUtil;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Transport;
import com.google.protobuf.ByteString;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.ByteArrayInputStream;
import java.nio.ByteBuffer;
import java.util.Arrays;

/**
 * Tests for {@link MessageFramer}
 */
@RunWith(JUnit4.class)
public class MessageFramerTest {

  public static final int TRANSPORT_FRAME_SIZE = 57;

  @Test
  public void testPayload() throws Exception {
    CapturingSink sink = new CapturingSink();
    MessageFramer framer = new MessageFramer(sink, TRANSPORT_FRAME_SIZE);
    byte[] payload = new byte[]{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23};
    byte[] unframedStream =
        Bytes.concat(
            new byte[]{GrpcFramingUtil.PAYLOAD_FRAME},
            new byte[]{0, 0, 0, (byte) payload.length},
            payload);
    for (int i = 0; i < 1000; i++) {
      framer.writePayload(new ByteArrayInputStream(payload), payload.length);
      if ((i + 1) % 13 == 0) {
        // Test flushing periodically
        framer.flush();
      }
    }
    framer.flush();
    assertEquals(sink.deframedStream.length, unframedStream.length * 1000);
    for (int i = 0; i < 1000; i++) {
      assertArrayEquals(unframedStream,
          Arrays.copyOfRange(sink.deframedStream, i * unframedStream.length,
              (i + 1) * unframedStream.length));
    }
  }

  @Test
  public void testContext() throws Exception {
    CapturingSink sink = new CapturingSink();
    MessageFramer framer = new MessageFramer(sink, TRANSPORT_FRAME_SIZE);
    byte[] payload = new byte[]{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23};
    byte[] contextValue = Transport.ContextValue.newBuilder()
        .setKey("somekey")
        .setValue(ByteString.copyFrom(payload))
        .build().toByteArray();
    byte[] unframedStream =
        Bytes.concat(
            new byte[]{GrpcFramingUtil.CONTEXT_VALUE_FRAME},
            new byte[]{0, 0,
                (byte) (contextValue.length >> 8 & 0xff),
                (byte) (contextValue.length & 0xff)},
            contextValue);
    for (int i = 0; i < 1000; i++) {
      framer.writeContext("somekey", new ByteArrayInputStream(payload), payload.length);
      if ((i + 1) % 13 == 0) {
        framer.flush();
      }
    }
    framer.flush();
    assertEquals(unframedStream.length * 1000, sink.deframedStream.length);
    for (int i = 0; i < 1000; i++) {
      assertArrayEquals(unframedStream,
          Arrays.copyOfRange(sink.deframedStream, i * unframedStream.length,
              (i + 1) * unframedStream.length));
    }
  }

  @Test
  public void testStatus() throws Exception {
    CapturingSink sink = new CapturingSink();
    MessageFramer framer = new MessageFramer(sink, TRANSPORT_FRAME_SIZE);
    byte[] unframedStream = Bytes.concat(
            new byte[]{GrpcFramingUtil.STATUS_FRAME},
            new byte[]{0, 0, 0, 2},  // Len is 2 bytes
            new byte[]{0, 13});  // Internal==13
    for (int i = 0; i < 1000; i++) {
      framer.writeStatus(new Status(Transport.Code.INTERNAL));
      if ((i + 1) % 13 == 0) {
        framer.flush();
      }
    }
    framer.flush();
    assertEquals(sink.deframedStream.length, unframedStream.length * 1000);
    for (int i = 0; i < 1000; i++) {
      assertArrayEquals(unframedStream,
          Arrays.copyOfRange(sink.deframedStream, i * unframedStream.length,
              (i + 1) * unframedStream.length));
    }
  }

  static class CapturingSink implements Framer.Sink<ByteBuffer> {

    byte[] deframedStream = new byte[0];

    @Override
    public void deliverFrame(ByteBuffer frame, boolean endOfMessage) {
      assertTrue(frame.remaining() <= TRANSPORT_FRAME_SIZE);
      // Frame must contain compression flag & 24 bit length
      int header = frame.getInt();
      byte flag = (byte) (header >>> 24);
      int length = header & 0xFFFFFF;
      assertTrue(TransportFrameUtil.isNotCompressed(flag));
      assertEquals(frame.remaining(), length);
      // Frame must exceed dictated transport frame size
      deframedStream = Bytes.concat(deframedStream,  ByteBuffers.extractBytes(frame));
    }
  }
}
