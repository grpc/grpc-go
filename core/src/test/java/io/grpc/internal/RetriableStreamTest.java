/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.CallOptions;
import io.grpc.ClientStreamTracer;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import io.grpc.internal.RetriableStream.ChannelBufferMeter;
import io.grpc.internal.StreamListener.MessageProducer;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.InOrder;

/** Unit tests for {@link RetriableStream}. */
@RunWith(JUnit4.class)
public class RetriableStreamTest {
  private static final String CANCELLED_BECAUSE_COMMITTED =
      "Stream thrown away because RetriableStream committed";
  private static final String AUTHORITY = "fakeAuthority";
  private static final Compressor COMPRESSOR = Codec.Identity.NONE;
  private static final DecompressorRegistry DECOMPRESSOR_REGISTRY =
      DecompressorRegistry.getDefaultInstance();
  private static final int MAX_INBOUND_MESSAGE_SIZE = 1234;
  private static final int MAX_OUTNBOUND_MESSAGE_SIZE = 5678;
  private static final long PER_RPC_BUFFER_LIMIT = 1000;
  private static final long CHANNEL_BUFFER_LIMIT = 2000;
  private final RetriableStreamRecorder retriableStreamRecorder =
      mock(RetriableStreamRecorder.class);
  private final ClientStreamListener masterListener = mock(ClientStreamListener.class);
  private final MethodDescriptor<String, String> method =
      MethodDescriptor.<String, String>newBuilder()
          .setType(MethodType.BIDI_STREAMING)
          .setFullMethodName(MethodDescriptor.generateFullMethodName("service_foo", "method_bar"))
          .setRequestMarshaller(new StringMarshaller())
          .setResponseMarshaller(new StringMarshaller())
          .build();
  private final ChannelBufferMeter channelBufferUsed = new ChannelBufferMeter();
  private final FakeClock fakeClock = new FakeClock();
  private final RetriableStream<String> retriableStream =
      new RetriableStream<String>(
          method, channelBufferUsed, PER_RPC_BUFFER_LIMIT, CHANNEL_BUFFER_LIMIT,
          MoreExecutors.directExecutor(), fakeClock.getScheduledExecutorService()) {
        @Override
        void postCommit() {
          retriableStreamRecorder.postCommit();
        }

        @Override
        ClientStream newStream(ClientStreamTracer.Factory tracerFactory) {
          bufferSizeTracer =
              tracerFactory.newClientStreamTracer(CallOptions.DEFAULT, new Metadata());
          return retriableStreamRecorder.newSubstream();
        }

        @Override
        Status prestart() {
          return retriableStreamRecorder.prestart();
        }

        @Override
        boolean shouldRetry() {
          return retriableStreamRecorder.shouldRetry();
        }
      };

  private ClientStreamTracer bufferSizeTracer;

  @Test
  public void retry_everythingDrained() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    InOrder inOrder = inOrder(retriableStreamRecorder, masterListener, mockStream1);

    // stream settings before start
    retriableStream.setAuthority(AUTHORITY);
    retriableStream.setCompressor(COMPRESSOR);
    retriableStream.setDecompressorRegistry(DECOMPRESSOR_REGISTRY);
    retriableStream.setFullStreamDecompression(false);
    retriableStream.setFullStreamDecompression(true);
    retriableStream.setMaxInboundMessageSize(MAX_INBOUND_MESSAGE_SIZE);
    retriableStream.setMessageCompression(true);
    retriableStream.setMaxOutboundMessageSize(MAX_OUTNBOUND_MESSAGE_SIZE);
    retriableStream.setMessageCompression(false);

    inOrder.verifyNoMoreInteractions();

    // start
    retriableStream.start(masterListener);

    inOrder.verify(retriableStreamRecorder).prestart();
    inOrder.verify(retriableStreamRecorder).newSubstream();

    inOrder.verify(mockStream1).setAuthority(AUTHORITY);
    inOrder.verify(mockStream1).setCompressor(COMPRESSOR);
    inOrder.verify(mockStream1).setDecompressorRegistry(DECOMPRESSOR_REGISTRY);
    inOrder.verify(mockStream1).setFullStreamDecompression(false);
    inOrder.verify(mockStream1).setFullStreamDecompression(true);
    inOrder.verify(mockStream1).setMaxInboundMessageSize(MAX_INBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream1).setMessageCompression(true);
    inOrder.verify(mockStream1).setMaxOutboundMessageSize(MAX_OUTNBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream1).setMessageCompression(false);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream1).start(sublistenerCaptor1.capture());
    inOrder.verifyNoMoreInteractions();

    retriableStream.sendMessage("msg1");
    retriableStream.sendMessage("msg2");
    retriableStream.request(345);
    retriableStream.flush();
    retriableStream.flush();
    retriableStream.sendMessage("msg3");
    retriableStream.request(456);

    inOrder.verify(mockStream1, times(2)).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream1).request(345);
    inOrder.verify(mockStream1, times(2)).flush();
    inOrder.verify(mockStream1).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream1).request(456);
    inOrder.verifyNoMoreInteractions();

    // retry1
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream();
    inOrder = inOrder(retriableStreamRecorder, masterListener, mockStream1, mockStream2);
    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    sublistenerCaptor1.getValue().closed(Status.UNAVAILABLE, new Metadata());

    inOrder.verify(retriableStreamRecorder).shouldRetry();
    // TODO(zdapeng): send more messages during backoff, then forward backoff ticker w/ right amount
    fakeClock.forwardNanos(0L);
    inOrder.verify(retriableStreamRecorder).newSubstream();
    inOrder.verify(mockStream2).setAuthority(AUTHORITY);
    inOrder.verify(mockStream2).setCompressor(COMPRESSOR);
    inOrder.verify(mockStream2).setDecompressorRegistry(DECOMPRESSOR_REGISTRY);
    inOrder.verify(mockStream2).setFullStreamDecompression(false);
    inOrder.verify(mockStream2).setFullStreamDecompression(true);
    inOrder.verify(mockStream2).setMaxInboundMessageSize(MAX_INBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream2).setMessageCompression(true);
    inOrder.verify(mockStream2).setMaxOutboundMessageSize(MAX_OUTNBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream2).setMessageCompression(false);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verify(mockStream2, times(2)).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream2).request(345);
    inOrder.verify(mockStream2, times(2)).flush();
    inOrder.verify(mockStream2).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream2).request(456);
    inOrder.verifyNoMoreInteractions();

    // send more messages
    retriableStream.sendMessage("msg1 after retry1");
    retriableStream.sendMessage("msg2 after retry1");

    // mockStream1 is closed so it is not in the drainedSubstreams
    verifyNoMoreInteractions(mockStream1);
    inOrder.verify(mockStream2, times(2)).writeMessage(any(InputStream.class));

    // retry2
    ClientStream mockStream3 = mock(ClientStream.class);
    doReturn(mockStream3).when(retriableStreamRecorder).newSubstream();
    inOrder =
        inOrder(retriableStreamRecorder, masterListener, mockStream1, mockStream2, mockStream3);
    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    sublistenerCaptor2.getValue().closed(Status.UNAVAILABLE, new Metadata());

    inOrder.verify(retriableStreamRecorder).shouldRetry();
    // TODO(zdapeng): send more messages during backoff, then forward backoff ticker w/ right amount
    fakeClock.forwardNanos(0L);
    inOrder.verify(retriableStreamRecorder).newSubstream();
    inOrder.verify(mockStream3).setAuthority(AUTHORITY);
    inOrder.verify(mockStream3).setCompressor(COMPRESSOR);
    inOrder.verify(mockStream3).setDecompressorRegistry(DECOMPRESSOR_REGISTRY);
    inOrder.verify(mockStream3).setFullStreamDecompression(false);
    inOrder.verify(mockStream3).setFullStreamDecompression(true);
    inOrder.verify(mockStream3).setMaxInboundMessageSize(MAX_INBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream3).setMessageCompression(true);
    inOrder.verify(mockStream3).setMaxOutboundMessageSize(MAX_OUTNBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream3).setMessageCompression(false);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor3 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream3).start(sublistenerCaptor3.capture());
    inOrder.verify(mockStream3, times(2)).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream3).request(345);
    inOrder.verify(mockStream3, times(2)).flush();
    inOrder.verify(mockStream3).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream3).request(456);
    inOrder.verify(mockStream3, times(2)).writeMessage(any(InputStream.class));
    inOrder.verifyNoMoreInteractions();

    // no more retry
    doReturn(false).when(retriableStreamRecorder).shouldRetry();
    sublistenerCaptor3.getValue().closed(Status.UNAVAILABLE, new Metadata());

    inOrder.verify(retriableStreamRecorder).shouldRetry();
    inOrder.verify(retriableStreamRecorder).postCommit();
    inOrder.verify(masterListener).closed(any(Status.class), any(Metadata.class));
    inOrder.verifyNoMoreInteractions();
  }

  @Test
  public void headersRead_cancel() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    sublistenerCaptor1.getValue().headersRead(new Metadata());

    inOrder.verify(retriableStreamRecorder).postCommit();

    retriableStream.cancel(Status.CANCELLED);

    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void retry_headersRead_cancel() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // retry
    // TODO(zdapeng): forward backoff ticker w/ right amount
    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream();
    sublistenerCaptor1.getValue().closed(Status.UNAVAILABLE, new Metadata());
    fakeClock.forwardNanos(0L);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verify(retriableStreamRecorder, never()).postCommit();

    // headersRead
    sublistenerCaptor2.getValue().headersRead(new Metadata());

    inOrder.verify(retriableStreamRecorder).postCommit();

    // cancel
    retriableStream.cancel(Status.CANCELLED);

    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void headersRead_closed() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    sublistenerCaptor1.getValue().headersRead(new Metadata());

    inOrder.verify(retriableStreamRecorder).postCommit();

    Status status = Status.UNAVAILABLE;
    Metadata metadata = new Metadata();
    sublistenerCaptor1.getValue().closed(status, metadata);

    inOrder.verify(retriableStreamRecorder, never()).postCommit();
    verify(masterListener).closed(status, metadata);
  }

  @Test
  public void retry_headersRead_closed() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // retry
    // TODO(zdapeng): forward backoff ticker w/ right amount
    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream();
    sublistenerCaptor1.getValue().closed(Status.UNAVAILABLE, new Metadata());
    fakeClock.forwardNanos(0L);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verify(retriableStreamRecorder, never()).postCommit();

    // headersRead
    sublistenerCaptor2.getValue().headersRead(new Metadata());

    inOrder.verify(retriableStreamRecorder).postCommit();

    // closed
    doReturn(false).when(retriableStreamRecorder).shouldRetry();
    Status status = Status.UNAVAILABLE;
    Metadata metadata = new Metadata();
    sublistenerCaptor2.getValue().closed(status, metadata);

    inOrder.verify(retriableStreamRecorder, never()).postCommit();
    verify(masterListener).closed(status, metadata);
  }

  @Test
  public void cancel_closed() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // cancel
    retriableStream.cancel(Status.CANCELLED);

    inOrder.verify(retriableStreamRecorder).postCommit();
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(mockStream1).cancel(statusCaptor.capture());
    assertEquals(Status.CANCELLED.getCode(), statusCaptor.getValue().getCode());
    assertEquals(CANCELLED_BECAUSE_COMMITTED, statusCaptor.getValue().getDescription());

    // closed
    // even shouldRetry() returns true
    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    Status status = Status.UNAVAILABLE;
    Metadata metadata = new Metadata();
    sublistenerCaptor1.getValue().closed(status, metadata);
    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void retry_cancel_closed() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // retry
    // TODO(zdapeng): forward backoff ticker w/ right amount
    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream();
    sublistenerCaptor1.getValue().closed(Status.UNAVAILABLE, new Metadata());
    fakeClock.forwardNanos(0L);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verify(retriableStreamRecorder, never()).postCommit();

    // cancel
    retriableStream.cancel(Status.CANCELLED);

    inOrder.verify(retriableStreamRecorder).postCommit();
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(mockStream2).cancel(statusCaptor.capture());
    assertEquals(Status.CANCELLED.getCode(), statusCaptor.getValue().getCode());
    assertEquals(CANCELLED_BECAUSE_COMMITTED, statusCaptor.getValue().getDescription());

    // closed
    doReturn(false).when(retriableStreamRecorder).shouldRetry();
    Status status = Status.UNAVAILABLE;
    Metadata metadata = new Metadata();
    sublistenerCaptor2.getValue().closed(status, metadata);
    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void unretriableClosed_cancel() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // closed
    Status status = Status.UNAVAILABLE;
    Metadata metadata = new Metadata();
    sublistenerCaptor1.getValue().closed(status, metadata);

    inOrder.verify(retriableStreamRecorder).postCommit();
    verify(masterListener).closed(status, metadata);

    // cancel
    retriableStream.cancel(Status.CANCELLED);
    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void retry_unretriableClosed_cancel() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // retry
    // TODO(zdapeng): forward backoff ticker w/ right amount
    fakeClock.forwardNanos(0L);
    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream();
    sublistenerCaptor1.getValue().closed(Status.UNAVAILABLE, new Metadata());
    fakeClock.forwardNanos(0L);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verify(retriableStreamRecorder, never()).postCommit();

    // closed
    doReturn(false).when(retriableStreamRecorder).shouldRetry();
    Status status = Status.UNAVAILABLE;
    Metadata metadata = new Metadata();
    sublistenerCaptor2.getValue().closed(status, metadata);

    inOrder.verify(retriableStreamRecorder).postCommit();
    verify(masterListener).closed(status, metadata);

    // cancel
    retriableStream.cancel(Status.CANCELLED);
    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void retry_cancelWhileBackoff() {
    // TODO(zdapeng)
  }

  @Test
  public void operationsWhileDraining() {
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    final AtomicReference<ClientStreamListener> sublistenerCaptor2 =
        new AtomicReference<ClientStreamListener>();
    final Status cancelStatus = Status.CANCELLED.withDescription("c");
    ClientStream mockStream1 =
        mock(
            ClientStream.class,
            delegatesTo(
                new NoopClientStream() {
                  @Override
                  public void request(int numMessages) {
                    retriableStream.sendMessage("substream1 request " + numMessages);
                    if (numMessages > 1) {
                      retriableStream.request(--numMessages);
                    }
                  }
                }));

    final ClientStream mockStream2 =
        mock(
            ClientStream.class,
            delegatesTo(
                new NoopClientStream() {
                  @Override
                  public void start(ClientStreamListener listener) {
                    sublistenerCaptor2.set(listener);
                  }

                  @Override
                  public void request(int numMessages) {
                    retriableStream.sendMessage("substream2 request " + numMessages);

                    if (numMessages == 3) {
                      sublistenerCaptor2.get().headersRead(new Metadata());
                    }
                    if (numMessages == 2) {
                      retriableStream.request(100);
                    }
                    if (numMessages == 100) {
                      retriableStream.cancel(cancelStatus);
                    }
                  }
                }));

    InOrder inOrder = inOrder(retriableStreamRecorder, mockStream1, mockStream2);

    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    retriableStream.start(masterListener);

    inOrder.verify(mockStream1).start(sublistenerCaptor1.capture());

    retriableStream.request(3);

    inOrder.verify(mockStream1).request(3);
    inOrder.verify(mockStream1).writeMessage(any(InputStream.class)); // msg "substream1 request 3"
    inOrder.verify(mockStream1).request(2);
    inOrder.verify(mockStream1).writeMessage(any(InputStream.class)); // msg "substream1 request 2"
    inOrder.verify(mockStream1).request(1);
    inOrder.verify(mockStream1).writeMessage(any(InputStream.class)); // msg "substream1 request 1"

    // retry
    // TODO(zdapeng): send more messages during backoff, then forward backoff ticker w/ right amount
    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream();
    sublistenerCaptor1.getValue().closed(Status.UNAVAILABLE, new Metadata());
    fakeClock.forwardNanos(0L);

    inOrder.verify(mockStream2).start(sublistenerCaptor2.get());

    inOrder.verify(mockStream2).request(3);
    inOrder.verify(retriableStreamRecorder).postCommit();
    inOrder.verify(mockStream2).writeMessage(any(InputStream.class)); // msg "substream1 request 3"
    inOrder.verify(mockStream2).request(2);
    inOrder.verify(mockStream2).writeMessage(any(InputStream.class)); // msg "substream1 request 2"
    inOrder.verify(mockStream2).request(1);

    // msg "substream1 request 1"
    // msg "substream2 request 3"
    // msg "substream2 request 2"
    inOrder.verify(mockStream2, times(3)).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream2).request(100);

    verify(mockStream2).cancel(cancelStatus);

    // "substream2 request 1" will never be sent
    inOrder.verify(mockStream2, never()).writeMessage(any(InputStream.class));
  }

  @Test
  public void operationsAfterImmediateCommit() {
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    ClientStream mockStream1 = mock(ClientStream.class);

    InOrder inOrder = inOrder(retriableStreamRecorder, mockStream1);

    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    retriableStream.start(masterListener);

    // drained
    inOrder.verify(mockStream1).start(sublistenerCaptor1.capture());

    // commit
    sublistenerCaptor1.getValue().headersRead(new Metadata());

    retriableStream.request(3);
    inOrder.verify(mockStream1).request(3);
    retriableStream.sendMessage("msg 1");
    inOrder.verify(mockStream1).writeMessage(any(InputStream.class));
  }

  @Test
  public void isReady_whenDrained() {
    ClientStream mockStream1 = mock(ClientStream.class);

    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    retriableStream.start(masterListener);

    assertFalse(retriableStream.isReady());

    doReturn(true).when(mockStream1).isReady();

    assertTrue(retriableStream.isReady());
  }

  @Test
  public void isReady_whileDraining() {
    final AtomicReference<ClientStreamListener> sublistenerCaptor1 =
        new AtomicReference<ClientStreamListener>();
    final List<Boolean> readiness = new ArrayList<Boolean>();
    ClientStream mockStream1 =
        mock(
            ClientStream.class,
            delegatesTo(
                new NoopClientStream() {
                  @Override
                  public void start(ClientStreamListener listener) {
                    sublistenerCaptor1.set(listener);
                    readiness.add(retriableStream.isReady()); // expected false b/c in draining
                  }

                  @Override
                  public boolean isReady() {
                    return true;
                  }
                }));

    final ClientStream mockStream2 =
        mock(
            ClientStream.class,
            delegatesTo(
                new NoopClientStream() {
                  @Override
                  public void start(ClientStreamListener listener) {
                    readiness.add(retriableStream.isReady()); // expected false b/c in draining
                  }

                  @Override
                  public boolean isReady() {
                    return true;
                  }
                }));

    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();
    retriableStream.start(masterListener);

    verify(mockStream1).start(sublistenerCaptor1.get());
    readiness.add(retriableStream.isReady()); // expected true

    // retry
    // TODO(zdapeng): send more messages during backoff, then forward backoff ticker w/ right amount
    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream();
    doReturn(false).when(mockStream1).isReady(); // mockStream1 closed, so isReady false
    sublistenerCaptor1.get().closed(Status.UNAVAILABLE, new Metadata());
    fakeClock.forwardNanos(0L);

    verify(mockStream2).start(any(ClientStreamListener.class));
    readiness.add(retriableStream.isReady()); // expected true

    assertThat(readiness).containsExactly(false, true, false, true).inOrder();
  }

  @Test
  public void messageAvailable() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    ClientStreamListener listener = sublistenerCaptor1.getValue();
    listener.headersRead(new Metadata());
    MessageProducer messageProducer = mock(MessageProducer.class);
    listener.messagesAvailable(messageProducer);
    verify(masterListener).messagesAvailable(messageProducer);
  }

  @Test
  public void closedWhileDraining() {
    ClientStream mockStream1 = mock(ClientStream.class);
    final ClientStream mockStream2 =
        mock(
            ClientStream.class,
            delegatesTo(
                new NoopClientStream() {
                  @Override
                  public void start(ClientStreamListener listener) {
                    // closed while draning
                    listener.closed(Status.UNAVAILABLE, new Metadata());
                  }
                }));
    final ClientStream mockStream3 = mock(ClientStream.class);

    doReturn(true).when(retriableStreamRecorder).shouldRetry();
    when(retriableStreamRecorder.newSubstream()).thenReturn(mockStream1, mockStream2, mockStream3);

    retriableStream.start(masterListener);
    retriableStream.sendMessage("msg1");
    retriableStream.sendMessage("msg2");

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    ClientStreamListener listener1 = sublistenerCaptor1.getValue();

    // retry
    // TODO(zdapeng): send more messages during backoff, then forward backoff ticker w/ right amount
    listener1.closed(Status.UNAVAILABLE, new Metadata());
    fakeClock.forwardNanos(0L);

    retriableStream.request(1);
    verify(mockStream1, never()).request(1);
    verify(mockStream2, never()).request(1);
    verify(mockStream3).request(1);
  }

  // TODO(zdapeng): test buffer limit exceeded during backoff
  @Test
  public void perRpcBufferLimitExceeded() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();

    retriableStream.start(masterListener);

    bufferSizeTracer.outboundWireSize(PER_RPC_BUFFER_LIMIT);

    assertEquals(PER_RPC_BUFFER_LIMIT, channelBufferUsed.addAndGet(0));

    verify(retriableStreamRecorder, never()).postCommit();
    bufferSizeTracer.outboundWireSize(2);
    verify(retriableStreamRecorder).postCommit();

    // verify channel buffer is adjusted
    assertEquals(0, channelBufferUsed.addAndGet(0));
  }

  @Test
  public void channelBufferLimitExceeded() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream();

    retriableStream.start(masterListener);

    bufferSizeTracer.outboundWireSize(100);

    assertEquals(100, channelBufferUsed.addAndGet(0));

    channelBufferUsed.addAndGet(CHANNEL_BUFFER_LIMIT - 200);
    verify(retriableStreamRecorder, never()).postCommit();
    bufferSizeTracer.outboundWireSize(100 + 1);
    verify(retriableStreamRecorder).postCommit();

    // verify channel buffer is adjusted
    assertEquals(CHANNEL_BUFFER_LIMIT - 200, channelBufferUsed.addAndGet(0));
  }

  /**
   * Used to stub a retriable stream as well as to record methods of the retriable stream being
   * called.
   */
  private interface RetriableStreamRecorder {
    void postCommit();

    ClientStream newSubstream();

    Status prestart();

    boolean shouldRetry();
  }
}
