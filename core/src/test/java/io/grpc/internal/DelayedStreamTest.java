/*
 * Copyright 2015 The gRPC Authors
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
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.Attributes.Key;
import io.grpc.Codec;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.testing.SingleMessageProducer;
import java.io.ByteArrayInputStream;
import java.io.InputStream;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.InOrder;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Tests for {@link DelayedStream}.  Most of the state checking is enforced by
 * {@link ClientCallImpl} so we don't check it here.
 */
@RunWith(JUnit4.class)
public class DelayedStreamTest {
  @Mock private ClientStreamListener listener;
  @Mock private ClientStream realStream;
  @Captor private ArgumentCaptor<ClientStreamListener> listenerCaptor;
  private DelayedStream stream = new DelayedStream();

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
  }

  @Test
  public void setStream_setAuthority() {
    final String authority = "becauseIsaidSo";
    stream.setAuthority(authority);
    stream.start(listener);
    stream.setStream(realStream);
    InOrder inOrder = inOrder(realStream);
    inOrder.verify(realStream).setAuthority(authority);
    inOrder.verify(realStream).start(any(ClientStreamListener.class));
  }

  @Test(expected = IllegalStateException.class)
  public void setAuthority_afterStart() {
    stream.start(listener);
    stream.setAuthority("notgonnawork");
  }

  @Test(expected = IllegalStateException.class)
  public void start_afterStart() {
    stream.start(listener);
    stream.start(mock(ClientStreamListener.class));
  }

  @Test
  public void setStream_sendsAllMessages() {
    stream.start(listener);
    stream.setCompressor(Codec.Identity.NONE);
    stream.setDecompressorRegistry(DecompressorRegistry.getDefaultInstance());

    stream.setMessageCompression(true);
    InputStream message = new ByteArrayInputStream(new byte[]{'a'});
    stream.writeMessage(message);
    stream.setMessageCompression(false);
    stream.writeMessage(message);

    stream.setStream(realStream);

    verify(realStream).setCompressor(Codec.Identity.NONE);
    verify(realStream).setDecompressorRegistry(DecompressorRegistry.getDefaultInstance());

    verify(realStream).setMessageCompression(true);
    verify(realStream).setMessageCompression(false);

    verify(realStream, times(2)).writeMessage(message);
    verify(realStream).start(listenerCaptor.capture());

    stream.writeMessage(message);
    verify(realStream, times(3)).writeMessage(message);

    verifyNoMoreInteractions(listener);
    listenerCaptor.getValue().onReady();
    verify(listener).onReady();
  }

  @Test
  public void setStream_halfClose() {
    stream.start(listener);
    stream.halfClose();
    stream.setStream(realStream);

    verify(realStream).halfClose();
  }

  @Test
  public void setStream_flush() {
    stream.start(listener);
    stream.flush();
    stream.setStream(realStream);
    verify(realStream).flush();

    stream.flush();
    verify(realStream, times(2)).flush();
  }

  @Test
  public void setStream_flowControl() {
    stream.start(listener);
    stream.request(1);
    stream.request(2);
    stream.setStream(realStream);
    verify(realStream).request(1);
    verify(realStream).request(2);

    stream.request(3);
    verify(realStream).request(3);
  }

  @Test
  public void setStream_setMessageCompression() {
    stream.start(listener);
    stream.setMessageCompression(false);
    stream.setStream(realStream);
    verify(realStream).setMessageCompression(false);

    stream.setMessageCompression(true);
    verify(realStream).setMessageCompression(true);
  }

  @Test
  public void setStream_isReady() {
    stream.start(listener);
    assertFalse(stream.isReady());
    stream.setStream(realStream);
    verify(realStream, never()).isReady();

    assertFalse(stream.isReady());
    verify(realStream).isReady();

    when(realStream.isReady()).thenReturn(true);
    assertTrue(stream.isReady());
    verify(realStream, times(2)).isReady();
  }

  @Test
  public void setStream_getAttributes() {
    Attributes attributes =
        Attributes.newBuilder().set(Key.<String>create("fakeKey"), "fakeValue").build();
    when(realStream.getAttributes()).thenReturn(attributes);

    stream.start(listener);

    try {
      stream.getAttributes(); // expect to throw IllegalStateException, otherwise fail()
      fail();
    } catch (IllegalStateException expected) {
      // ignore
    }

    stream.setStream(realStream);
    assertEquals(attributes, stream.getAttributes());
  }

  @Test
  public void startThenCancelled() {
    stream.start(listener);
    stream.cancel(Status.CANCELLED);
    verify(listener).closed(eq(Status.CANCELLED), any(Metadata.class));
  }

  @Test
  public void startThenSetStreamThenCancelled() {
    stream.start(listener);
    stream.setStream(realStream);
    stream.cancel(Status.CANCELLED);
    verify(realStream).start(any(ClientStreamListener.class));
    verify(realStream).cancel(same(Status.CANCELLED));
  }

  @Test
  public void setStreamThenStartThenCancelled() {
    stream.setStream(realStream);
    stream.start(listener);
    stream.cancel(Status.CANCELLED);
    verify(realStream).start(same(listener));
    verify(realStream).cancel(same(Status.CANCELLED));
  }

  @Test
  public void setStreamThenCancelled() {
    stream.setStream(realStream);
    stream.cancel(Status.CANCELLED);
    verify(realStream).cancel(same(Status.CANCELLED));
  }

  @Test
  public void setStreamTwice() {
    stream.start(listener);
    stream.setStream(realStream);
    verify(realStream).start(any(ClientStreamListener.class));
    stream.setStream(mock(ClientStream.class));
    stream.flush();
    verify(realStream).flush();
  }

  @Test
  public void cancelThenSetStream() {
    stream.cancel(Status.CANCELLED);
    stream.setStream(realStream);
    stream.start(listener);
    stream.isReady();
    verifyNoMoreInteractions(realStream);
  }

  @Test
  public void cancel_beforeStart() {
    Status status = Status.CANCELLED.withDescription("that was quick");
    stream.cancel(status);
    stream.start(listener);
    verify(listener).closed(same(status), any(Metadata.class));
  }

  @Test
  public void cancelledThenStart() {
    stream.cancel(Status.CANCELLED);
    stream.start(listener);
    verify(listener).closed(eq(Status.CANCELLED), any(Metadata.class));
  }

  @Test
  public void listener_onReadyDelayedUntilPassthrough() {
    class IsReadyListener extends NoopClientStreamListener {
      boolean onReadyCalled;

      @Override
      public void onReady() {
        // If onReady was not delayed, then passthrough==false and isReady will return false.
        assertTrue(stream.isReady());
        onReadyCalled = true;
      }
    }

    IsReadyListener isReadyListener = new IsReadyListener();
    stream.start(isReadyListener);
    stream.setStream(new NoopClientStream() {
      @Override
      public void start(ClientStreamListener listener) {
        // This call to the listener should end up being delayed.
        listener.onReady();
      }

      @Override
      public boolean isReady() {
        return true;
      }
    });
    assertTrue(isReadyListener.onReadyCalled);
  }

  @Test
  public void listener_allQueued() {
    final Metadata headers = new Metadata();
    final InputStream message1 = mock(InputStream.class);
    final InputStream message2 = mock(InputStream.class);
    final SingleMessageProducer producer1 = new SingleMessageProducer(message1);
    final SingleMessageProducer producer2 = new SingleMessageProducer(message2);
    final Metadata trailers = new Metadata();
    final Status status = Status.UNKNOWN.withDescription("unique status");

    final InOrder inOrder = inOrder(listener);
    stream.start(listener);
    stream.setStream(new NoopClientStream() {
      @Override
      public void start(ClientStreamListener passedListener) {
        passedListener.onReady();
        passedListener.headersRead(headers);
        passedListener.messagesAvailable(producer1);
        passedListener.onReady();
        passedListener.messagesAvailable(producer2);
        passedListener.closed(status, trailers);

        verifyNoMoreInteractions(listener);
      }
    });
    inOrder.verify(listener).onReady();
    inOrder.verify(listener).headersRead(headers);
    inOrder.verify(listener).messagesAvailable(producer1);
    inOrder.verify(listener).onReady();
    inOrder.verify(listener).messagesAvailable(producer2);
    inOrder.verify(listener).closed(status, trailers);
  }

  @Test
  public void listener_noQueued() {
    final Metadata headers = new Metadata();
    final InputStream message = mock(InputStream.class);
    final SingleMessageProducer producer = new SingleMessageProducer(message);
    final Metadata trailers = new Metadata();
    final Status status = Status.UNKNOWN.withDescription("unique status");

    stream.start(listener);
    stream.setStream(realStream);
    verify(realStream).start(listenerCaptor.capture());
    ClientStreamListener delayedListener = listenerCaptor.getValue();
    delayedListener.onReady();
    verify(listener).onReady();
    delayedListener.headersRead(headers);
    verify(listener).headersRead(headers);
    delayedListener.messagesAvailable(producer);
    verify(listener).messagesAvailable(producer);
    delayedListener.closed(status, trailers);
    verify(listener).closed(status, trailers);
  }
}
