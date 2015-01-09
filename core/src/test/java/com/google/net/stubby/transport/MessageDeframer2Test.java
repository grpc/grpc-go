/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package com.google.net.stubby.transport;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;
import com.google.common.primitives.Bytes;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.transport.MessageDeframer2.Listener;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.List;
import java.util.concurrent.ExecutionException;
import java.util.zip.GZIPOutputStream;

/**
 * Tests for {@link MessageDeframer2}.
 */
@RunWith(JUnit4.class)
public class MessageDeframer2Test {
  private Listener listener = mock(Listener.class);
  private MessageDeframer2 deframer =
      new MessageDeframer2(listener, MoreExecutors.directExecutor());
  private ArgumentCaptor<InputStream> messages = ArgumentCaptor.forClass(InputStream.class);

  @Test
  public void simplePayload() {
    assertNull(deframer.deframe(buffer(new byte[]{0, 0, 0, 0, 2, 3, 14}), false));
    verify(listener).messageRead(messages.capture(), eq(2));
    assertEquals(Bytes.asList(new byte[]{3, 14}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void smallCombinedPayloads() {
    assertNull(
        deframer.deframe(buffer(new byte[]{0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}), false));
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener).messageRead(messages.capture(), eq(2));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    assertEquals(Bytes.asList(new byte[] {14, 15}), bytes(messages));
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void endOfStreamWithPayloadShouldNotifyEndOfStream() {
    assertNull(deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true));
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener).endOfStream();
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void endOfStreamShouldNotifyEndOfStream() {
    assertNull(deframer.deframe(buffer(new byte[0]), true));
    verify(listener).endOfStream();
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void payloadSplitBetweenBuffers() {
    assertNull(deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 7, 3, 14, 1, 5, 9}), false));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    assertNull(deframer.deframe(buffer(new byte[] {2, 6}), false));
    verify(listener).messageRead(messages.capture(), eq(7));
    assertEquals(Bytes.asList(new byte[] {3, 14, 1, 5, 9, 2, 6}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void frameHeaderSplitBetweenBuffers() {
    assertNull(deframer.deframe(buffer(new byte[] {0, 0}), false));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
    assertNull(deframer.deframe(buffer(new byte[] {0, 0, 1, 3}), false));
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void emptyPayload() {
    assertNull(deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 0}), false));
    verify(listener).messageRead(messages.capture(), eq(0));
    assertEquals(Bytes.asList(), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void largerFrameSize() {
    assertNull(deframer.deframe(
        Buffers.wrap(Bytes.concat(new byte[] {0, 0, 0, 3, (byte) 232}, new byte[1000])), false));
    verify(listener).messageRead(messages.capture(), eq(1000));
    assertEquals(Bytes.asList(new byte[1000]), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void payloadCallbackShouldWaitForFutureCompletion() {
    SettableFuture<Void> messageFuture = SettableFuture.create();
    when(listener.messageRead(any(InputStream.class), eq(1))).thenReturn(messageFuture);
    // Deframe a block with 2 messages.
    ListenableFuture<?> deframeFuture
        = deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}), false);
    assertNotNull(deframeFuture);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[]{3}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);

    SettableFuture<Void> messageFuture2 = SettableFuture.create();
    when(listener.messageRead(any(InputStream.class), eq(2))).thenReturn(messageFuture2);
    messageFuture.set(null);
    assertFalse(deframeFuture.isDone());
    verify(listener).messageRead(messages.capture(), eq(2));
    assertEquals(Bytes.asList(new byte[] {14, 15}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);

    messageFuture2.set(null);
    assertTrue(deframeFuture.isDone());

    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verify(listener).deliveryStalled();
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void endOfStreamCallbackShouldWaitForFutureCompletion() {
    SettableFuture<Void> messageFuture = SettableFuture.create();
    when(listener.messageRead(any(InputStream.class), eq(1))).thenReturn(messageFuture);
    ListenableFuture<?> deframeFuture
        = deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3}), true);
    assertNotNull(deframeFuture);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);

    messageFuture.set(null);
    assertTrue(deframeFuture.isDone());
    verify(listener).endOfStream();
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void futureShouldPropagateThrownException() throws InterruptedException {
    SettableFuture<Void> messageFuture = SettableFuture.create();
    when(listener.messageRead(any(InputStream.class), eq(1))).thenReturn(messageFuture);
    ListenableFuture<?> deframeFuture
        = deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}), false);
    assertNotNull(deframeFuture);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[]{3}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);

    RuntimeException thrownEx = new RuntimeException();
    when(listener.messageRead(any(InputStream.class), eq(2))).thenThrow(thrownEx);
    messageFuture.set(null);
    verify(listener).messageRead(messages.capture(), eq(2));
    assertTrue(deframeFuture.isDone());
    try {
      deframeFuture.get();
      fail("Should have throws ExecutionException");
    } catch (ExecutionException ex) {
      assertEquals(thrownEx, ex.getCause());
    }
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void futureFailureShouldStopAndPropagateFailure() throws InterruptedException {
    SettableFuture<Void> messageFuture = SettableFuture.create();
    when(listener.messageRead(any(InputStream.class), eq(1))).thenReturn(messageFuture);
    ListenableFuture<?> deframeFuture
        = deframer.deframe(buffer(new byte[] {0, 0, 0, 0, 1, 3, 0, 0, 0, 0, 2, 14, 15}), false);
    assertNotNull(deframeFuture);
    verify(listener).messageRead(messages.capture(), eq(1));
    assertEquals(Bytes.asList(new byte[] {3}), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);

    RuntimeException thrownEx = new RuntimeException();
    messageFuture.setException(thrownEx);
    assertTrue(deframeFuture.isDone());
    try {
      deframeFuture.get();
      fail("Should have throws ExecutionException");
    } catch (ExecutionException ex) {
      assertEquals(thrownEx, ex.getCause());
    }
    verify(listener, atLeastOnce()).bytesRead(anyInt());
    verifyNoMoreInteractions(listener);
  }

  @Test
  public void compressed() {
    deframer = new MessageDeframer2(
        listener, MoreExecutors.directExecutor(), MessageDeframer2.Compression.GZIP);
    byte[] payload = compress(new byte[1000]);
    assertTrue(payload.length < 100);
    byte[] header = new byte[] {1, 0, 0, 0, (byte) payload.length};
    deframer.deframe(buffer(Bytes.concat(header, payload)), false);
    verify(listener).messageRead(messages.capture(), eq(1000));
    assertEquals(Bytes.asList(new byte[1000]), bytes(messages));
    verify(listener, atLeastOnce()).bytesRead(anyInt());
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
