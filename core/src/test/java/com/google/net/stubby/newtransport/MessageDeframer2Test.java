package com.google.net.stubby.newtransport;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.notNull;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;
import com.google.common.primitives.Bytes;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

import java.util.Arrays;
import java.util.List;
import java.util.zip.GZIPOutputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;

/**
 * Tests for {@link MessageDeframer2}.
 */
@RunWith(JUnit4.class)
public class MessageDeframer2Test {
  private StreamListener listener = mock(StreamListener.class);
  private MessageDeframer2 deframer
      = MessageDeframer2.createOnClient(listener, MoreExecutors.directExecutor());
  private ArgumentCaptor<InputStream> messages = ArgumentCaptor.forClass(InputStream.class);

  @Test
  public void simplePayload() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 2, 3, 14}), false);
    verify(listener).messageRead(messages.capture(), eq(2));
    assertEquals(Bytes.asList(new byte[] {3, 14}), bytes(messages));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void smallCombinedPayloads() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}), false);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener).messageRead(messages.capture(), eq(2));
    assertEquals(Bytes.asList(new byte[] {14, 15}), bytes(messages));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void clientEndOfStreamShouldNotNotifyClose() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void serverEndOfStreamWithPayloadShouldNotifyClose() {
    deframer = MessageDeframer2.createOnServer(listener, MoreExecutors.directExecutor());
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener).closed(eq(Status.OK), notNull(Metadata.Trailers.class));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void serverEndOfStreamShouldNotifyClose() {
    deframer = MessageDeframer2.createOnServer(listener, MoreExecutors.directExecutor());
    deframer.deframe(buffer(new byte[0]), true);
    verify(listener).closed(eq(Status.OK), notNull(Metadata.Trailers.class));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void payloadSplitBetweenBuffers() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 7, 3, 14, 1, 5, 9}), false);
    verifyNoMoreInteractions(listener);
    deframer.deframe(buffer(new byte[] {2, 6}), false);
    verify(listener).messageRead(messages.capture(), eq(7));
    assertEquals(Bytes.asList(new byte[] {3, 14, 1, 5, 9, 2, 6}), bytes(messages));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void frameHeaderSplitBetweenBuffers() {
    deframer.deframe(buffer(new byte[] {0, 0}), false);
    verifyNoMoreInteractions(listener);
    deframer.deframe(buffer(new byte[] {0, 0, 1, 3}), false);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void emptyPayload() {
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 0}), false);
    verify(listener).messageRead(messages.capture(), eq(0));
    assertEquals(Bytes.asList(), bytes(messages));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void largerFrameSize() {
    deframer.deframe(
        Buffers.wrap(Bytes.concat(new byte[] {0, 0, 0, 3, (byte) 232}, new byte[1000])), false);
    verify(listener).messageRead(messages.capture(), eq(1000));
    assertEquals(Bytes.asList(new byte[1000]), bytes(messages));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void payloadCallbackShouldWaitForFutureCompletion() {
    SettableFuture<Void> messageFuture = SettableFuture.create();
    when(listener.messageRead(any(InputStream.class), eq(1))).thenReturn(messageFuture);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}), false);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verifyNoMoreInteractions(listener);

    messageFuture.set(null);
    verify(listener).messageRead(messages.capture(), eq(2));
    assertEquals(Bytes.asList(new byte[] {14, 15}), bytes(messages));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void serverClosedCallbackShouldWaitForFutureCompletion() {
    deframer = MessageDeframer2.createOnServer(listener, MoreExecutors.directExecutor());
    SettableFuture<Void> messageFuture = SettableFuture.create();
    when(listener.messageRead(any(InputStream.class), eq(1))).thenReturn(messageFuture);
    deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verifyNoMoreInteractions(listener);

    messageFuture.set(null);
    verify(listener).closed(eq(Status.OK), notNull(Metadata.Trailers.class));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void compressed() {
    deframer = MessageDeframer2.createOnClient(
        listener, MoreExecutors.directExecutor(), MessageDeframer2.Compression.GZIP);
    byte[] payload = compress(new byte[1000]);
    assertTrue(payload.length < 100);
    byte[] header = new byte[] {1, 0, 0, 0, (byte) payload.length};
    deframer.deframe(buffer(Bytes.concat(header, payload)), false);
    verify(listener).messageRead(messages.capture(), eq(1000));
    assertEquals(Bytes.asList(new byte[1000]), bytes(messages));
    verifyNoMoreInteractions(listener);
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
