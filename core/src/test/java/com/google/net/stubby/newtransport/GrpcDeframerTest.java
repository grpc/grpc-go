package com.google.net.stubby.newtransport;

import static com.google.net.stubby.newtransport.TransportFrameUtil.PAYLOAD_FRAME;
import static com.google.net.stubby.newtransport.TransportFrameUtil.STATUS_FRAME;
import static java.nio.charset.StandardCharsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.Status;
import com.google.protobuf.ByteString;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.util.Arrays;

import javax.annotation.Nullable;

/**
 * Tests for {@link GrpcDeframer}.
 */
@RunWith(JUnit4.class)
public class GrpcDeframerTest {
  private static final String MESSAGE = "hello world";
  private static final ByteString MESSAGE_BSTR = ByteString.copyFromUtf8(MESSAGE);
  private static final Status STATUS_CODE = Status.CANCELLED;

  private GrpcDeframer reader;

  private StubDecompressor decompressor;

  @Mock
  private GrpcDeframer.Sink sink;

  private SettableFuture<Void> messageFuture;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);
    messageFuture = SettableFuture.create();
    when(sink.messageRead(any(InputStream.class), anyInt())).thenReturn(messageFuture);

    decompressor = new StubDecompressor();
    reader = new GrpcDeframer(decompressor, sink, MoreExecutors.directExecutor());
  }

  @Test
  public void payloadShouldCallTarget() throws Exception {
    decompressor.init(payloadFrame());
    reader.deframe(Buffers.empty(), false);
    verifyPayload();
    verifyNoStatus();
  }

  @Test
  public void payloadWithEndOfStreamShouldNotifyStatus() throws Exception {
    decompressor.init(payloadFrame());
    reader.deframe(Buffers.empty(), true);
    verifyPayload();
    verifyNoStatus();

    messageFuture.set(null);
    verifyStatus(Status.Code.OK);
  }

  @Test
  public void statusShouldCallTarget() throws Exception {
    decompressor.init(statusFrame());
    reader.deframe(Buffers.empty(), false);
    verifyNoPayload();
    verifyStatus();
  }

  @Test
  public void statusWithEndOfStreamShouldNotifyStatusOnce() throws Exception {
    decompressor.init(statusFrame());
    reader.deframe(Buffers.empty(), true);
    verifyNoPayload();
    verifyStatus();
  }

  @Test
  public void multipleFramesShouldCallTarget() throws Exception {
    ByteArrayOutputStream os = new ByteArrayOutputStream();
    DataOutputStream dos = new DataOutputStream(os);

    // Write a payload frame.
    writeFrame(PAYLOAD_FRAME, MESSAGE_BSTR.toByteArray(), dos);

    // Write a status frame.
    byte[] statusBytes = new byte[] {0, (byte) STATUS_CODE.getCode().value()};
    writeFrame(STATUS_FRAME, statusBytes, dos);

    // Now write the complete frame: compression header followed by the 3 message frames.
    dos.close();
    byte[] bodyBytes = os.toByteArray();

    decompressor.init(bodyBytes);
    reader.deframe(Buffers.empty(), false);

    // Verify that all callbacks were called.
    verifyPayload();
    verifyNoStatus();

    messageFuture.set(null);
    verifyStatus();
  }

  @Test
  public void partialFrameShouldSucceed() throws Exception {
    byte[] frame = payloadFrame();

    // Create a buffer that contains 2 payload frames.
    byte[] fullBuffer = Arrays.copyOf(frame, frame.length * 2);
    System.arraycopy(frame, 0, fullBuffer, frame.length, frame.length);

    // Use only a portion of the frame. Should not call the sink.
    int startIx = 0;
    int endIx = 10;
    byte[] chunk = Arrays.copyOfRange(fullBuffer, startIx, endIx);
    decompressor.init(chunk);
    reader.deframe(Buffers.empty(), false);
    verifyNoPayload();
    verifyNoStatus();

    // Supply the rest of the frame and a portion of a second frame. Should call the sink.
    startIx = endIx;
    endIx = startIx + frame.length;
    chunk = Arrays.copyOfRange(fullBuffer, startIx, endIx);
    decompressor.init(chunk);
    reader.deframe(Buffers.empty(), false);
    verifyPayload();
    verifyNoStatus();
  }

  private void verifyPayload() {
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(sink).messageRead(captor.capture(), eq(MESSAGE.length()));
    assertEquals(MESSAGE, readString(captor.getValue(), MESSAGE.length()));
  }

  private String readString(InputStream in, int length) {
    try {
      byte[] bytes = new byte[length];
      ByteStreams.readFully(in, bytes);
      return new String(bytes, UTF_8);
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }

  private void verifyStatus() {
    verifyStatus(Status.Code.CANCELLED);
  }

  private void verifyStatus(Status.Code code) {
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(sink).statusRead(captor.capture());
    verify(sink).endOfStream();
    assertEquals(code, captor.getValue().getCode());
  }

  private void verifyNoPayload() {
    verify(sink, never()).messageRead(any(InputStream.class), anyInt());
  }

  private void verifyNoStatus() {
    verify(sink, never()).statusRead(any(Status.class));
    verify(sink, never()).endOfStream();
  }

  private static byte[] payloadFrame() throws IOException {
    return frame(PAYLOAD_FRAME, MESSAGE_BSTR.toByteArray());
  }

  private static byte[] statusFrame() throws IOException {
    byte[] bytes = new byte[] {0, (byte) STATUS_CODE.getCode().value()};
    return frame(STATUS_FRAME, bytes);
  }

  private static byte[] frame(int frameType, byte[] data) throws IOException {
    ByteArrayOutputStream bos = new ByteArrayOutputStream();
    OutputStream os = bos;
    DataOutputStream dos = new DataOutputStream(os);
    writeFrame(frameType, data, dos);
    dos.close();
    return bos.toByteArray();
  }

  private static void writeFrame(int frameType, byte[] data, DataOutputStream out)
      throws IOException {
    out.write(frameType);
    out.writeInt(data.length);
    out.write(data);
  }

  private static final class StubDecompressor implements Decompressor {
    byte[] bytes;
    int offset;

    void init(byte[] bytes) {
      this.bytes = bytes;
      this.offset = 0;
    }

    @Override
    public void decompress(Buffer data) {
      // Do nothing.
    }

    @Override
    public void close() {
      // Do nothing.
    }

    @Override
    @Nullable
    public Buffer readBytes(int length) {
      length = Math.min(length, bytes.length - offset);
      if (length == 0) {
        return null;
      }

      Buffer buffer = Buffers.wrap(bytes, offset, length);
      offset += length;
      return buffer;
    }
  }
}
