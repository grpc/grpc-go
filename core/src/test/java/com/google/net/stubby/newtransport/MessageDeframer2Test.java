package com.google.net.stubby.newtransport;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;
import com.google.common.primitives.Bytes;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.newtransport.MessageDeframer2.Sink;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.List;
import java.util.zip.GZIPOutputStream;

/**
 * Tests for {@link MessageDeframer2}.
 */
@RunWith(JUnit4.class)
public class MessageDeframer2Test {
  private Sink sink = mock(Sink.class);
  private MessageDeframer2 deframer = new MessageDeframer2(sink, MoreExecutors.directExecutor());
  private ArgumentCaptor<InputStream> messages = ArgumentCaptor.forClass(InputStream.class);

  @Test
  public void simplePayload() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 2, 3, 14}), false);
    verify(sink).messageRead(messages.capture(), eq(2));
    assertEquals(Bytes.asList(new byte[] {3, 14}), bytes(messages));
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void smallCombinedPayloads() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}), false);
    verify(sink).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(sink).messageRead(messages.capture(), eq(2));
    assertEquals(Bytes.asList(new byte[] {14, 15}), bytes(messages));
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void endOfStreamWithPayloadShouldNotifyEndOfStream() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true);
    verify(sink).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(sink).endOfStream();
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void endOfStreamShouldNotifyEndOfStream() {
    deframer.deframe(buffer(new byte[0]), true);
    verify(sink).endOfStream();
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void payloadSplitBetweenBuffers() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 7, 3, 14, 1, 5, 9}), false);
    verifyNoMoreInteractions(sink);
    deframer.deframe(buffer(new byte[] {2, 6}), false);
    verify(sink).messageRead(messages.capture(), eq(7));
    assertEquals(Bytes.asList(new byte[] {3, 14, 1, 5, 9, 2, 6}), bytes(messages));
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void frameHeaderSplitBetweenBuffers() {
    deframer.deframe(buffer(new byte[] {0, 0}), false);
    verifyNoMoreInteractions(sink);
    deframer.deframe(buffer(new byte[] {0, 0, 1, 3}), false);
    verify(sink).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void emptyPayload() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 0}), false);
    verify(sink).messageRead(messages.capture(), eq(0));
    assertEquals(Bytes.asList(), bytes(messages));
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void largerFrameSize() {
    deframer.deframe(
        Buffers.wrap(Bytes.concat(new byte[] {0, 0, 0, 3, (byte) 232}, new byte[1000])), false);
    verify(sink).messageRead(messages.capture(), eq(1000));
    assertEquals(Bytes.asList(new byte[1000]), bytes(messages));
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void payloadCallbackShouldWaitForFutureCompletion() {
    SettableFuture<Void> messageFuture = SettableFuture.create();
    when(sink.messageRead(any(InputStream.class), eq(1))).thenReturn(messageFuture);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}), false);
    verify(sink).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verifyNoMoreInteractions(sink);

    messageFuture.set(null);
    verify(sink).messageRead(messages.capture(), eq(2));
    assertEquals(Bytes.asList(new byte[] {14, 15}), bytes(messages));
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void endOfStreamCallbackShouldWaitForFutureCompletion() {
    SettableFuture<Void> messageFuture = SettableFuture.create();
    when(sink.messageRead(any(InputStream.class), eq(1))).thenReturn(messageFuture);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true);
    verify(sink).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verifyNoMoreInteractions(sink);

    messageFuture.set(null);
    verify(sink).endOfStream();
    verifyNoMoreInteractions(sink);
  }

  @Test
  public void compressed() {
    deframer = new MessageDeframer2(
        sink, MoreExecutors.directExecutor(), MessageDeframer2.Compression.GZIP);
    byte[] payload = compress(new byte[1000]);
    assertTrue(payload.length < 100);
    byte[] header = new byte[] {1, 0, 0, 0, (byte) payload.length};
    deframer.deframe(buffer(Bytes.concat(header, payload)), false);
    verify(sink).messageRead(messages.capture(), eq(1000));
    assertEquals(Bytes.asList(new byte[1000]), bytes(messages));
    verifyNoMoreInteractions(sink);
  }

  private static List<Byte> bytes(ArgumentCaptor<InputStream> captor) {
    try {
      return Bytes.asList(ByteStreams.toByteArray(captor.getValue()));
    } catch (IOException ex) {
      throw new AssertionError(ex);
    }
  }

  private static Buffer buffer(byte[] bytes) {
    return Buffers.wrap(bytes);
  }

  private static byte[] compress(byte[] bytes) {
    try {
      ByteArrayOutputStream baos = new ByteArrayOutputStream();
      GZIPOutputStream zip = new GZIPOutputStream(baos);
      zip.write(bytes);
      zip.close();
      return baos.toByteArray();
    } catch (IOException ex) {
      throw new RuntimeException(ex);
    }
  }
}
