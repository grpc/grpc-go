package com.google.net.stubby.newtransport;

import static com.google.net.stubby.newtransport.TransportFrameUtil.CONTEXT_VALUE_FRAME;
import static com.google.net.stubby.newtransport.TransportFrameUtil.PAYLOAD_FRAME;
import static com.google.net.stubby.newtransport.TransportFrameUtil.STATUS_FRAME;
import static java.nio.charset.StandardCharsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

import com.google.common.io.ByteStreams;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Transport;
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
  private static final String KEY = "key";
  private static final String MESSAGE = "hello world";
  private static final ByteString MESSAGE_BSTR = ByteString.copyFromUtf8(MESSAGE);
  private static final Transport.Code STATUS_CODE = Transport.Code.CANCELLED;

  private GrpcDeframer reader;

  private Transport.ContextValue contextProto;

  private StubDecompressor decompressor;

  @Mock
  private GrpcMessageListener listener;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);
    decompressor = new StubDecompressor();
    reader = new GrpcDeframer(decompressor, listener);

    contextProto = Transport.ContextValue.newBuilder().setKey(KEY).setValue(MESSAGE_BSTR).build();
  }

  @Test
  public void contextShouldCallTarget() throws Exception {
    decompressor.init(contextFrame());
    reader.deframe(Buffers.empty(), false);
    verifyContext();
    verifyNoPayload();
    verifyNoStatus();
  }

  @Test
  public void contextWithEndOfStreamShouldNotifyStatus() throws Exception {
    decompressor.init(contextFrame());
    reader.deframe(Buffers.empty(), true);
    verifyContext();
    verifyNoPayload();
    verifyStatus(Transport.Code.OK);
  }

  @Test
  public void payloadShouldCallTarget() throws Exception {
    decompressor.init(payloadFrame());
    reader.deframe(Buffers.empty(), false);
    verifyNoContext();
    verifyPayload();
    verifyNoStatus();
  }

  @Test
  public void payloadWithEndOfStreamShouldNotifyStatus() throws Exception {
    decompressor.init(payloadFrame());
    reader.deframe(Buffers.empty(), true);
    verifyNoContext();
    verifyPayload();
    verifyStatus(Transport.Code.OK);
  }

  @Test
  public void statusShouldCallTarget() throws Exception {
    decompressor.init(statusFrame());
    reader.deframe(Buffers.empty(), false);
    verifyNoContext();
    verifyNoPayload();
    verifyStatus();
  }

  @Test
  public void statusWithEndOfStreamShouldNotifyStatusOnce() throws Exception {
    decompressor.init(statusFrame());
    reader.deframe(Buffers.empty(), true);
    verifyNoContext();
    verifyNoPayload();
    verifyStatus();
  }

  @Test
  public void multipleFramesShouldCallTarget() throws Exception {
    ByteArrayOutputStream os = new ByteArrayOutputStream();
    DataOutputStream dos = new DataOutputStream(os);

    // Write a context frame.
    writeFrame(CONTEXT_VALUE_FRAME, contextProto.toByteArray(), dos);

    // Write a payload frame.
    writeFrame(PAYLOAD_FRAME, MESSAGE_BSTR.toByteArray(), dos);

    // Write a status frame.
    byte[] statusBytes = new byte[] {0, (byte) STATUS_CODE.getNumber()};
    writeFrame(STATUS_FRAME, statusBytes, dos);

    // Now write the complete frame: compression header followed by the 3 message frames.
    dos.close();
    byte[] bodyBytes = os.toByteArray();

    decompressor.init(bodyBytes);
    reader.deframe(Buffers.empty(), false);

    // Verify that all callbacks were called.
    verifyContext();
    verifyPayload();
    verifyStatus();
  }

  @Test
  public void partialFrameShouldSucceed() throws Exception {
    byte[] frame = payloadFrame();

    // Create a buffer that contains 2 payload frames.
    byte[] fullBuffer = Arrays.copyOf(frame, frame.length * 2);
    System.arraycopy(frame, 0, fullBuffer, frame.length, frame.length);

    // Use only a portion of the frame. Should not call the listener.
    int startIx = 0;
    int endIx = 10;
    byte[] chunk = Arrays.copyOfRange(fullBuffer, startIx, endIx);
    decompressor.init(chunk);
    reader.deframe(Buffers.empty(), false);
    verifyNoContext();
    verifyNoPayload();
    verifyNoStatus();

    // Supply the rest of the frame and a portion of a second frame. Should call the listener.
    startIx = endIx;
    endIx = startIx + frame.length;
    chunk = Arrays.copyOfRange(fullBuffer, startIx, endIx);
    decompressor.init(chunk);
    reader.deframe(Buffers.empty(), false);
    verifyNoContext();
    verifyPayload();
    verifyNoStatus();
  }

  private void verifyContext() {
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(listener).onContext(eq(KEY), captor.capture(), eq(MESSAGE.length()));
    assertEquals(MESSAGE, readString(captor.getValue(), MESSAGE.length()));
  }

  private void verifyPayload() {
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(listener).onPayload(captor.capture(), eq(MESSAGE.length()));
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
    verifyStatus(Transport.Code.CANCELLED);
  }

  private void verifyStatus(Transport.Code code) {
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).onStatus(captor.capture());
    assertEquals(code, captor.getValue().getCode());
  }

  private void verifyNoContext() {
    verify(listener, never()).onContext(any(String.class), any(InputStream.class), anyInt());
  }

  private void verifyNoPayload() {
    verify(listener, never()).onPayload(any(InputStream.class), anyInt());
  }

  private void verifyNoStatus() {
    verify(listener, never()).onStatus(any(Status.class));
  }

  private byte[] contextFrame() throws IOException {
    return frame(CONTEXT_VALUE_FRAME, contextProto.toByteArray());
  }

  private static byte[] payloadFrame() throws IOException {
    return frame(PAYLOAD_FRAME, MESSAGE_BSTR.toByteArray());
  }

  private static byte[] statusFrame() throws IOException {
    byte[] bytes = new byte[] {0, (byte) STATUS_CODE.getNumber()};
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

      Buffer buffer = Buffers.wrap(ByteString.copyFrom(bytes, offset, length));
      offset += length;
      return buffer;
    }
  }
}
