/*
 * Copyright 2017 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import com.google.common.io.ByteStreams;
import com.google.common.primitives.Bytes;
import io.grpc.internal.StreamListener.MessageProducer;
import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.util.LinkedList;
import java.util.Queue;
import javax.annotation.Nullable;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ApplicationThreadDeframer}. */
@RunWith(JUnit4.class)
public class ApplicationThreadDeframerTest {
  private MessageDeframer mockDeframer = mock(MessageDeframer.class);
  private DeframerListener listener = new DeframerListener();
  private TransportExecutor transportExecutor = new TransportExecutor();
  private ApplicationThreadDeframer applicationThreadDeframer =
      new ApplicationThreadDeframer(listener, transportExecutor, mockDeframer);

  @Before
  public void setUp() {
    // ApplicationThreadDeframer constructor injects itself as the wrapped deframer's listener.
    verify(mockDeframer).setListener(applicationThreadDeframer);
  }

  @Test
  public void requestInvokesMessagesAvailableOnListener() {
    applicationThreadDeframer.request(1);
    verifyZeroInteractions(mockDeframer);
    listener.runStoredProducer();
    verify(mockDeframer).request(1);
  }

  @Test
  public void deframeInvokesMessagesAvailableOnListener() {
    ReadableBuffer frame = ReadableBuffers.wrap(new byte[1]);
    applicationThreadDeframer.deframe(frame);
    verifyZeroInteractions(mockDeframer);
    listener.runStoredProducer();
    verify(mockDeframer).deframe(frame);
  }

  @Test
  public void closeWhenCompleteInvokesMessagesAvailableOnListener() {
    applicationThreadDeframer.closeWhenComplete();
    verifyZeroInteractions(mockDeframer);
    listener.runStoredProducer();
    verify(mockDeframer).closeWhenComplete();
  }

  @Test
  public void closeInvokesMessagesAvailableOnListener() {
    applicationThreadDeframer.close();
    verify(mockDeframer).stopDelivery();
    verifyNoMoreInteractions(mockDeframer);
    listener.runStoredProducer();
    verify(mockDeframer).close();
  }

  @Test
  public void bytesReadInvokesTransportExecutor() {
    applicationThreadDeframer.bytesRead(1);
    assertEquals(0, listener.bytesRead);
    transportExecutor.runStoredRunnable();
    assertEquals(1, listener.bytesRead);
  }

  @Test
  public void deframerClosedInvokesTransportExecutor() {
    applicationThreadDeframer.deframerClosed(true);
    assertFalse(listener.deframerClosedWithPartialMessage);
    transportExecutor.runStoredRunnable();
    assertTrue(listener.deframerClosedWithPartialMessage);
  }

  @Test
  public void deframeFailedInvokesTransportExecutor() {
    Throwable cause = new Throwable("error");
    applicationThreadDeframer.deframeFailed(cause);
    assertNull(listener.deframeFailedCause);
    transportExecutor.runStoredRunnable();
    assertEquals(cause, listener.deframeFailedCause);
  }

  @Test
  public void messagesAvailableDrainsToMessageReadQueue_returnedByInitializingMessageProducer()
      throws Exception {
    byte[][] messageBytes = {{1, 2, 3}, {4}, {5, 6}};
    Queue<InputStream> messages = new LinkedList<>();
    for (int i = 0; i < messageBytes.length; i++) {
      messages.add(new ByteArrayInputStream(messageBytes[i]));
    }
    MultiMessageProducer messageProducer = new MultiMessageProducer(messages);
    applicationThreadDeframer.messagesAvailable(messageProducer);
    applicationThreadDeframer.request(1 /* value is ignored */);
    for (int i = 0; i < messageBytes.length; i++) {
      InputStream message = listener.storedProducer.next();
      assertNotNull(message);
      assertEquals(Bytes.asList(messageBytes[i]), Bytes.asList(ByteStreams.toByteArray(message)));
    }
    assertNull(listener.storedProducer.next());
  }

  private static class DeframerListener implements MessageDeframer.Listener {
    private MessageProducer storedProducer;
    private int bytesRead;
    private boolean deframerClosedWithPartialMessage;
    private Throwable deframeFailedCause;

    private void runStoredProducer() {
      assertNotNull(storedProducer);
      storedProducer.next();
    }

    @Override
    public void bytesRead(int numBytes) {
      assertEquals(0, bytesRead);
      bytesRead = numBytes;
    }

    @Override
    public void messagesAvailable(MessageProducer producer) {
      assertNull(storedProducer);
      storedProducer = producer;
    }

    @Override
    public void deframerClosed(boolean hasPartialMessage) {
      assertFalse(deframerClosedWithPartialMessage);
      deframerClosedWithPartialMessage = hasPartialMessage;
    }

    @Override
    public void deframeFailed(Throwable cause) {
      assertNull(deframeFailedCause);
      deframeFailedCause = cause;
    }
  }

  private static class TransportExecutor implements ApplicationThreadDeframer.TransportExecutor {
    private Runnable storedRunnable;

    private void runStoredRunnable() {
      assertNotNull(storedRunnable);
      storedRunnable.run();
    }

    @Override
    public void runOnTransportThread(Runnable r) {
      assertNull(storedRunnable);
      storedRunnable = r;
    }
  }

  private static class MultiMessageProducer implements StreamListener.MessageProducer {
    private final Queue<InputStream> messages;

    private MultiMessageProducer(Queue<InputStream> messages) {
      this.messages = messages;
    }

    @Nullable
    @Override
    public InputStream next() {
      return messages.poll();
    }
  }
}
