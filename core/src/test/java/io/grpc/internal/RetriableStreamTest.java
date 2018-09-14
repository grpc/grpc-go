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

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.internal.ClientStreamListener.RpcProgress.DROPPED;
import static io.grpc.internal.ClientStreamListener.RpcProgress.PROCESSED;
import static io.grpc.internal.ClientStreamListener.RpcProgress.REFUSED;
import static io.grpc.internal.RetriableStream.GRPC_PREVIOUS_RPC_ATTEMPTS;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableSet;
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
import io.grpc.Status.Code;
import io.grpc.StringMarshaller;
import io.grpc.internal.RetriableStream.ChannelBufferMeter;
import io.grpc.internal.RetriableStream.Throttle;
import io.grpc.internal.StreamListener.MessageProducer;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.List;
import java.util.Random;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import javax.annotation.Nullable;
import org.junit.After;
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
  private static final int MAX_OUTBOUND_MESSAGE_SIZE = 5678;
  private static final long PER_RPC_BUFFER_LIMIT = 1000;
  private static final long CHANNEL_BUFFER_LIMIT = 2000;
  private static final int MAX_ATTEMPTS = 6;
  private static final long INITIAL_BACKOFF_IN_SECONDS = 100;
  private static final long MAX_BACKOFF_IN_SECONDS = 700;
  private static final double BACKOFF_MULTIPLIER = 2D;
  private static final double FAKE_RANDOM = .5D;

  static {
    RetriableStream.setRandom(
        // not random
        new Random() {
          @Override
          public double nextDouble() {
            return FAKE_RANDOM;
          }
        });
  }

  private static final Code RETRIABLE_STATUS_CODE_1 = Code.UNAVAILABLE;
  private static final Code RETRIABLE_STATUS_CODE_2 = Code.DATA_LOSS;
  private static final Code NON_RETRIABLE_STATUS_CODE = Code.INTERNAL;
  private static final RetryPolicy RETRY_POLICY =
      new RetryPolicy(
          MAX_ATTEMPTS,
          TimeUnit.SECONDS.toNanos(INITIAL_BACKOFF_IN_SECONDS),
          TimeUnit.SECONDS.toNanos(MAX_BACKOFF_IN_SECONDS),
          BACKOFF_MULTIPLIER,
          ImmutableSet.of(RETRIABLE_STATUS_CODE_1, RETRIABLE_STATUS_CODE_2));

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

  private final class RecordedRetriableStream extends RetriableStream<String> {
    RecordedRetriableStream(MethodDescriptor<String, ?> method, Metadata headers,
        ChannelBufferMeter channelBufferUsed, long perRpcBufferLimit, long channelBufferLimit,
        Executor callExecutor,
        ScheduledExecutorService scheduledExecutorService,
        final RetryPolicy retryPolicy,
        final HedgingPolicy hedgingPolicy,
        @Nullable Throttle throttle) {
      super(
          method, headers, channelBufferUsed, perRpcBufferLimit, channelBufferLimit, callExecutor,
          scheduledExecutorService,
          new RetryPolicy.Provider() {
            @Override
            public RetryPolicy get() {
              return retryPolicy;
            }
          },
          new HedgingPolicy.Provider() {
            @Override
            public HedgingPolicy get() {
              return hedgingPolicy;
            }
          },
          throttle);
    }

    @Override
    void postCommit() {
      retriableStreamRecorder.postCommit();
    }

    @Override
    ClientStream newSubstream(ClientStreamTracer.Factory tracerFactory, Metadata metadata) {
      bufferSizeTracer =
          tracerFactory.newClientStreamTracer(CallOptions.DEFAULT, new Metadata());
      int actualPreviousRpcAttemptsInHeader = metadata.get(GRPC_PREVIOUS_RPC_ATTEMPTS) == null
          ? 0 : Integer.valueOf(metadata.get(GRPC_PREVIOUS_RPC_ATTEMPTS));
      return retriableStreamRecorder.newSubstream(actualPreviousRpcAttemptsInHeader);
    }

    @Override
    Status prestart() {
      return retriableStreamRecorder.prestart();
    }
  }

  private final RetriableStream<String> retriableStream =
      newThrottledRetriableStream(null /* throttle */);

  private ClientStreamTracer bufferSizeTracer;

  private RetriableStream<String> newThrottledRetriableStream(Throttle throttle) {
    return new RecordedRetriableStream(
        method, new Metadata(), channelBufferUsed, PER_RPC_BUFFER_LIMIT, CHANNEL_BUFFER_LIMIT,
        MoreExecutors.directExecutor(), fakeClock.getScheduledExecutorService(), RETRY_POLICY,
        HedgingPolicy.DEFAULT, throttle);
  }

  @After
  public void tearDown() {
    assertEquals(0, fakeClock.numPendingTasks());
  }

  @Test
  public void retry_everythingDrained() {
    ClientStream mockStream1 = mock(ClientStream.class);
    ClientStream mockStream2 = mock(ClientStream.class);
    ClientStream mockStream3 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    InOrder inOrder =
        inOrder(retriableStreamRecorder, masterListener, mockStream1, mockStream2, mockStream3);

    // stream settings before start
    retriableStream.setAuthority(AUTHORITY);
    retriableStream.setCompressor(COMPRESSOR);
    retriableStream.setDecompressorRegistry(DECOMPRESSOR_REGISTRY);
    retriableStream.setFullStreamDecompression(false);
    retriableStream.setFullStreamDecompression(true);
    retriableStream.setMaxInboundMessageSize(MAX_INBOUND_MESSAGE_SIZE);
    retriableStream.setMessageCompression(true);
    retriableStream.setMaxOutboundMessageSize(MAX_OUTBOUND_MESSAGE_SIZE);
    retriableStream.setMessageCompression(false);

    inOrder.verifyNoMoreInteractions();

    // start
    retriableStream.start(masterListener);

    inOrder.verify(retriableStreamRecorder).prestart();
    inOrder.verify(retriableStreamRecorder).newSubstream(0);

    inOrder.verify(mockStream1).setAuthority(AUTHORITY);
    inOrder.verify(mockStream1).setCompressor(COMPRESSOR);
    inOrder.verify(mockStream1).setDecompressorRegistry(DECOMPRESSOR_REGISTRY);
    inOrder.verify(mockStream1).setFullStreamDecompression(false);
    inOrder.verify(mockStream1).setFullStreamDecompression(true);
    inOrder.verify(mockStream1).setMaxInboundMessageSize(MAX_INBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream1).setMessageCompression(true);
    inOrder.verify(mockStream1).setMaxOutboundMessageSize(MAX_OUTBOUND_MESSAGE_SIZE);
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
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_2), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());

    // send more messages during backoff
    retriableStream.sendMessage("msg1 during backoff1");
    retriableStream.sendMessage("msg2 during backoff1");

    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM) - 1L, TimeUnit.SECONDS);
    inOrder.verifyNoMoreInteractions();
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    inOrder.verify(retriableStreamRecorder).newSubstream(1);
    inOrder.verify(mockStream2).setAuthority(AUTHORITY);
    inOrder.verify(mockStream2).setCompressor(COMPRESSOR);
    inOrder.verify(mockStream2).setDecompressorRegistry(DECOMPRESSOR_REGISTRY);
    inOrder.verify(mockStream2).setFullStreamDecompression(false);
    inOrder.verify(mockStream2).setFullStreamDecompression(true);
    inOrder.verify(mockStream2).setMaxInboundMessageSize(MAX_INBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream2).setMessageCompression(true);
    inOrder.verify(mockStream2).setMaxOutboundMessageSize(MAX_OUTBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream2).setMessageCompression(false);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verify(mockStream2, times(2)).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream2).request(345);
    inOrder.verify(mockStream2, times(2)).flush();
    inOrder.verify(mockStream2).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream2).request(456);
    inOrder.verify(mockStream2, times(2)).writeMessage(any(InputStream.class));
    inOrder.verifyNoMoreInteractions();

    // send more messages
    retriableStream.sendMessage("msg1 after retry1");
    retriableStream.sendMessage("msg2 after retry1");

    // mockStream1 is closed so it is not in the drainedSubstreams
    verifyNoMoreInteractions(mockStream1);
    inOrder.verify(mockStream2, times(2)).writeMessage(any(InputStream.class));

    // retry2
    doReturn(mockStream3).when(retriableStreamRecorder).newSubstream(2);
    sublistenerCaptor2.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());

    // send more messages during backoff
    retriableStream.sendMessage("msg1 during backoff2");
    retriableStream.sendMessage("msg2 during backoff2");
    retriableStream.sendMessage("msg3 during backoff2");

    fakeClock.forwardTime(
        (long) (INITIAL_BACKOFF_IN_SECONDS * BACKOFF_MULTIPLIER * FAKE_RANDOM) - 1L,
        TimeUnit.SECONDS);
    inOrder.verifyNoMoreInteractions();
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    inOrder.verify(retriableStreamRecorder).newSubstream(2);
    inOrder.verify(mockStream3).setAuthority(AUTHORITY);
    inOrder.verify(mockStream3).setCompressor(COMPRESSOR);
    inOrder.verify(mockStream3).setDecompressorRegistry(DECOMPRESSOR_REGISTRY);
    inOrder.verify(mockStream3).setFullStreamDecompression(false);
    inOrder.verify(mockStream3).setFullStreamDecompression(true);
    inOrder.verify(mockStream3).setMaxInboundMessageSize(MAX_INBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream3).setMessageCompression(true);
    inOrder.verify(mockStream3).setMaxOutboundMessageSize(MAX_OUTBOUND_MESSAGE_SIZE);
    inOrder.verify(mockStream3).setMessageCompression(false);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor3 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream3).start(sublistenerCaptor3.capture());
    inOrder.verify(mockStream3, times(2)).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream3).request(345);
    inOrder.verify(mockStream3, times(2)).flush();
    inOrder.verify(mockStream3).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream3).request(456);
    inOrder.verify(mockStream3, times(7)).writeMessage(any(InputStream.class));
    inOrder.verifyNoMoreInteractions();

    // no more retry
    sublistenerCaptor3.getValue().closed(
        Status.fromCode(NON_RETRIABLE_STATUS_CODE), new Metadata());

    inOrder.verify(retriableStreamRecorder).postCommit();
    inOrder.verify(masterListener).closed(any(Status.class), any(Metadata.class));
    inOrder.verifyNoMoreInteractions();
  }

  @Test
  public void headersRead_cancel() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
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
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // retry
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);

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
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    sublistenerCaptor1.getValue().headersRead(new Metadata());

    inOrder.verify(retriableStreamRecorder).postCommit();

    Status status = Status.fromCode(RETRIABLE_STATUS_CODE_1);
    Metadata metadata = new Metadata();
    sublistenerCaptor1.getValue().closed(status, metadata);

    verify(masterListener).closed(status, metadata);
    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void retry_headersRead_closed() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // retry
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verify(retriableStreamRecorder, never()).postCommit();

    // headersRead
    sublistenerCaptor2.getValue().headersRead(new Metadata());

    inOrder.verify(retriableStreamRecorder).postCommit();

    // closed even with retriable status
    Status status = Status.fromCode(RETRIABLE_STATUS_CODE_1);
    Metadata metadata = new Metadata();
    sublistenerCaptor2.getValue().closed(status, metadata);

    verify(masterListener).closed(status, metadata);
    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void cancel_closed() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
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

    // closed even with retriable status
    Status status = Status.fromCode(RETRIABLE_STATUS_CODE_1);
    Metadata metadata = new Metadata();
    sublistenerCaptor1.getValue().closed(status, metadata);
    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void retry_cancel_closed() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // retry
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);

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
    Status status = Status.fromCode(NON_RETRIABLE_STATUS_CODE);
    Metadata metadata = new Metadata();
    sublistenerCaptor2.getValue().closed(status, metadata);
    inOrder.verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void unretriableClosed_cancel() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // closed
    Status status = Status.fromCode(NON_RETRIABLE_STATUS_CODE);
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
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    InOrder inOrder = inOrder(retriableStreamRecorder);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // retry
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verify(retriableStreamRecorder, never()).postCommit();

    // closed
    Status status = Status.fromCode(NON_RETRIABLE_STATUS_CODE);
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
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // retry
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());

    // cancel while backoff
    assertEquals(1, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder, never()).postCommit();
    retriableStream.cancel(Status.CANCELLED);
    verify(retriableStreamRecorder).postCommit();

    verifyNoMoreInteractions(mockStream1);
    verifyNoMoreInteractions(mockStream2);
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

    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
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
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());

    // send more requests during backoff
    retriableStream.request(789);

    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);

    inOrder.verify(mockStream2).start(sublistenerCaptor2.get());
    inOrder.verify(mockStream2).request(3);
    inOrder.verify(retriableStreamRecorder).postCommit();
    inOrder.verify(mockStream2).writeMessage(any(InputStream.class)); // msg "substream1 request 3"
    inOrder.verify(mockStream2).request(2);
    inOrder.verify(mockStream2).writeMessage(any(InputStream.class)); // msg "substream1 request 2"
    inOrder.verify(mockStream2).request(1);

    // msg "substream1 request 1"
    inOrder.verify(mockStream2).writeMessage(any(InputStream.class));
    inOrder.verify(mockStream2).request(789);
    // msg "substream2 request 3"
    // msg "substream2 request 2"
    inOrder.verify(mockStream2, times(2)).writeMessage(any(InputStream.class));
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

    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
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

    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    retriableStream.start(masterListener);

    assertFalse(retriableStream.isReady());

    doReturn(true).when(mockStream1).isReady();

    assertTrue(retriableStream.isReady());
  }

  @Test
  public void isReady_whileDraining() {
    final AtomicReference<ClientStreamListener> sublistenerCaptor1 =
        new AtomicReference<ClientStreamListener>();
    final List<Boolean> readiness = new ArrayList<>();
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

    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    retriableStream.start(masterListener);

    verify(mockStream1).start(sublistenerCaptor1.get());
    readiness.add(retriableStream.isReady()); // expected true

    // retry
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    doReturn(false).when(mockStream1).isReady(); // mockStream1 closed, so isReady false
    sublistenerCaptor1.get().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());

    // send more requests during backoff
    retriableStream.request(789);
    readiness.add(retriableStream.isReady()); // expected false b/c in backoff

    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);

    verify(mockStream2).start(any(ClientStreamListener.class));
    readiness.add(retriableStream.isReady()); // expected true

    assertThat(readiness).containsExactly(false, true, false, false, true).inOrder();
  }

  @Test
  public void messageAvailable() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);

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
                    listener.closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
                  }
                }));
    final ClientStream mockStream3 = mock(ClientStream.class);

    when(retriableStreamRecorder.newSubstream(anyInt()))
        .thenReturn(mockStream1, mockStream2, mockStream3);

    retriableStream.start(masterListener);
    retriableStream.sendMessage("msg1");
    retriableStream.sendMessage("msg2");

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    ClientStreamListener listener1 = sublistenerCaptor1.getValue();

    // retry
    listener1.closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);
    assertEquals(1, fakeClock.numPendingTasks());

    // send requests during backoff
    retriableStream.request(3);
    fakeClock.forwardTime(
        (long) (INITIAL_BACKOFF_IN_SECONDS * BACKOFF_MULTIPLIER * FAKE_RANDOM), TimeUnit.SECONDS);

    retriableStream.request(1);
    verify(mockStream1, never()).request(anyInt());
    verify(mockStream2, never()).request(anyInt());
    verify(mockStream3).request(3);
    verify(mockStream3).request(1);
  }


  @Test
  public void perRpcBufferLimitExceeded() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);

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
  public void perRpcBufferLimitExceededDuringBackoff() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);

    retriableStream.start(masterListener);

    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    bufferSizeTracer.outboundWireSize(PER_RPC_BUFFER_LIMIT - 1);

    // retry
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());

    // bufferSizeTracer.outboundWireSize() quits immediately while backoff b/c substream1 is closed
    assertEquals(1, fakeClock.numPendingTasks());
    bufferSizeTracer.outboundWireSize(2);
    verify(retriableStreamRecorder, never()).postCommit();

    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);
    verify(mockStream2).start(any(ClientStreamListener.class));

    // bufferLimitExceeded
    bufferSizeTracer.outboundWireSize(PER_RPC_BUFFER_LIMIT - 1);
    verify(retriableStreamRecorder, never()).postCommit();
    bufferSizeTracer.outboundWireSize(2);
    verify(retriableStreamRecorder).postCommit();

    verifyNoMoreInteractions(mockStream1);
    verifyNoMoreInteractions(mockStream2);
  }

  @Test
  public void channelBufferLimitExceeded() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);

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

  @Test
  public void updateHeaders() {
    Metadata originalHeaders = new Metadata();
    Metadata headers = retriableStream.updateHeaders(originalHeaders, 0);
    assertNotSame(originalHeaders, headers);
    assertNull(headers.get(GRPC_PREVIOUS_RPC_ATTEMPTS));

    headers = retriableStream.updateHeaders(originalHeaders, 345);
    assertEquals("345", headers.get(GRPC_PREVIOUS_RPC_ATTEMPTS));
    assertNull(originalHeaders.get(GRPC_PREVIOUS_RPC_ATTEMPTS));
  }

  @Test
  public void expBackoff_maxBackoff_maxRetryAttempts() {
    ClientStream mockStream1 = mock(ClientStream.class);
    ClientStream mockStream2 = mock(ClientStream.class);
    ClientStream mockStream3 = mock(ClientStream.class);
    ClientStream mockStream4 = mock(ClientStream.class);
    ClientStream mockStream5 = mock(ClientStream.class);
    ClientStream mockStream6 = mock(ClientStream.class);
    ClientStream mockStream7 = mock(ClientStream.class);
    InOrder inOrder = inOrder(
        mockStream1, mockStream2, mockStream3, mockStream4, mockStream5, mockStream6, mockStream7);
    when(retriableStreamRecorder.newSubstream(anyInt())).thenReturn(
        mockStream1, mockStream2, mockStream3, mockStream4, mockStream5, mockStream6, mockStream7);

    retriableStream.start(masterListener);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(0);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream1).start(sublistenerCaptor1.capture());
    inOrder.verifyNoMoreInteractions();


    // retry1
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM) - 1L, TimeUnit.SECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(1);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verifyNoMoreInteractions();

    // retry2
    sublistenerCaptor2.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_2), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(
        (long) (INITIAL_BACKOFF_IN_SECONDS * BACKOFF_MULTIPLIER * FAKE_RANDOM) - 1L,
        TimeUnit.SECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(2);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor3 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream3).start(sublistenerCaptor3.capture());
    inOrder.verifyNoMoreInteractions();

    // retry3
    sublistenerCaptor3.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(
        (long) (INITIAL_BACKOFF_IN_SECONDS * BACKOFF_MULTIPLIER * BACKOFF_MULTIPLIER * FAKE_RANDOM)
            - 1L,
        TimeUnit.SECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(3);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor4 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream4).start(sublistenerCaptor4.capture());
    inOrder.verifyNoMoreInteractions();

    // retry4
    sublistenerCaptor4.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_2), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime((long) (MAX_BACKOFF_IN_SECONDS * FAKE_RANDOM) - 1L, TimeUnit.SECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(4);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor5 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream5).start(sublistenerCaptor5.capture());
    inOrder.verifyNoMoreInteractions();

    // retry5
    sublistenerCaptor5.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_2), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime((long) (MAX_BACKOFF_IN_SECONDS * FAKE_RANDOM) - 1L, TimeUnit.SECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(5);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor6 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream6).start(sublistenerCaptor6.capture());
    inOrder.verifyNoMoreInteractions();

    // can not retry any more
    verify(retriableStreamRecorder, never()).postCommit();
    sublistenerCaptor6.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    verify(retriableStreamRecorder).postCommit();
    inOrder.verifyNoMoreInteractions();
  }

  @Test
  public void pushback() {
    ClientStream mockStream1 = mock(ClientStream.class);
    ClientStream mockStream2 = mock(ClientStream.class);
    ClientStream mockStream3 = mock(ClientStream.class);
    ClientStream mockStream4 = mock(ClientStream.class);
    ClientStream mockStream5 = mock(ClientStream.class);
    ClientStream mockStream6 = mock(ClientStream.class);
    ClientStream mockStream7 = mock(ClientStream.class);
    InOrder inOrder = inOrder(
        mockStream1, mockStream2, mockStream3, mockStream4, mockStream5, mockStream6, mockStream7);
    when(retriableStreamRecorder.newSubstream(anyInt())).thenReturn(
        mockStream1, mockStream2, mockStream3, mockStream4, mockStream5, mockStream6, mockStream7);

    retriableStream.start(masterListener);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(0);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream1).start(sublistenerCaptor1.capture());
    inOrder.verifyNoMoreInteractions();


    // retry1
    int pushbackInMillis = 123;
    Metadata headers = new Metadata();
    headers.put(RetriableStream.GRPC_RETRY_PUSHBACK_MS, "" + pushbackInMillis);
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), headers);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(pushbackInMillis - 1, TimeUnit.MILLISECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.MILLISECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(1);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verifyNoMoreInteractions();

    // retry2
    pushbackInMillis = 4567 * 1000;
    headers = new Metadata();
    headers.put(RetriableStream.GRPC_RETRY_PUSHBACK_MS, "" + pushbackInMillis);
    sublistenerCaptor2.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_2), headers);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(pushbackInMillis - 1, TimeUnit.MILLISECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.MILLISECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(2);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor3 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream3).start(sublistenerCaptor3.capture());
    inOrder.verifyNoMoreInteractions();

    // retry3
    sublistenerCaptor3.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM) - 1L, TimeUnit.SECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(3);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor4 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream4).start(sublistenerCaptor4.capture());
    inOrder.verifyNoMoreInteractions();

    // retry4
    sublistenerCaptor4.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_2), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(
        (long) (INITIAL_BACKOFF_IN_SECONDS * BACKOFF_MULTIPLIER * FAKE_RANDOM) - 1L,
        TimeUnit.SECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(4);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor5 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream5).start(sublistenerCaptor5.capture());
    inOrder.verifyNoMoreInteractions();

    // retry5
    sublistenerCaptor5.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_2), new Metadata());
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(
        (long) (INITIAL_BACKOFF_IN_SECONDS * BACKOFF_MULTIPLIER * BACKOFF_MULTIPLIER * FAKE_RANDOM)
            - 1L,
        TimeUnit.SECONDS);
    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(1L, TimeUnit.SECONDS);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(5);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor6 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream6).start(sublistenerCaptor6.capture());
    inOrder.verifyNoMoreInteractions();

    // can not retry any more even pushback is positive
    pushbackInMillis = 4567 * 1000;
    headers = new Metadata();
    headers.put(RetriableStream.GRPC_RETRY_PUSHBACK_MS, "" + pushbackInMillis);
    verify(retriableStreamRecorder, never()).postCommit();
    sublistenerCaptor6.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), headers);
    verify(retriableStreamRecorder).postCommit();
    inOrder.verifyNoMoreInteractions();
  }

  @Test
  public void pushback_noRetry() {
    ClientStream mockStream1 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(anyInt());

    retriableStream.start(masterListener);
    assertEquals(0, fakeClock.numPendingTasks());
    verify(retriableStreamRecorder).newSubstream(0);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());
    verify(retriableStreamRecorder, never()).postCommit();

    // pushback no retry
    Metadata headers = new Metadata();
    headers.put(RetriableStream.GRPC_RETRY_PUSHBACK_MS, "");
    sublistenerCaptor1.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), headers);

    verify(retriableStreamRecorder, never()).newSubstream(1);
    verify(retriableStreamRecorder).postCommit();
  }

  @Test
  public void throttle() {
    Throttle throttle = new Throttle(4f, 0.8f);
    assertTrue(throttle.isAboveThreshold());
    assertTrue(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // token = 3
    assertTrue(throttle.isAboveThreshold());
    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // token = 2
    assertFalse(throttle.isAboveThreshold());
    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // token = 1
    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // token = 0
    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // token = 0
    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // token = 0
    assertFalse(throttle.isAboveThreshold());

    throttle.onSuccess(); // token = 0.8
    assertFalse(throttle.isAboveThreshold());
    throttle.onSuccess(); // token = 1.6
    assertFalse(throttle.isAboveThreshold());
    throttle.onSuccess(); // token = 3.2
    assertTrue(throttle.isAboveThreshold());
    throttle.onSuccess(); // token = 4
    assertTrue(throttle.isAboveThreshold());
    throttle.onSuccess(); // token = 4
    assertTrue(throttle.isAboveThreshold());
    throttle.onSuccess(); // token = 4
    assertTrue(throttle.isAboveThreshold());

    assertTrue(throttle.isAboveThreshold());
    assertTrue(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // token = 3
    assertTrue(throttle.isAboveThreshold());
    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // token = 2
    assertFalse(throttle.isAboveThreshold());
  }

  @Test
  public void throttledStream_FailWithRetriableStatusCode_WithoutPushback() {
    Throttle throttle = new Throttle(4f, 0.8f);
    RetriableStream<String> retriableStream = newThrottledRetriableStream(throttle);

    ClientStream mockStream = mock(ClientStream.class);
    doReturn(mockStream).when(retriableStreamRecorder).newSubstream(anyInt());
    retriableStream.start(masterListener);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream).start(sublistenerCaptor.capture());

    // mimic some other call in the channel triggers a throttle countdown
    assertTrue(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 3

    sublistenerCaptor.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), new Metadata());
    verify(retriableStreamRecorder).postCommit();
    assertFalse(throttle.isAboveThreshold()); // count = 2
  }

  @Test
  public void throttledStream_FailWithNonRetriableStatusCode_WithoutPushback() {
    Throttle throttle = new Throttle(4f, 0.8f);
    RetriableStream<String> retriableStream = newThrottledRetriableStream(throttle);

    ClientStream mockStream = mock(ClientStream.class);
    doReturn(mockStream).when(retriableStreamRecorder).newSubstream(anyInt());
    retriableStream.start(masterListener);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream).start(sublistenerCaptor.capture());

    // mimic some other call in the channel triggers a throttle countdown
    assertTrue(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 3

    sublistenerCaptor.getValue().closed(Status.fromCode(NON_RETRIABLE_STATUS_CODE), new Metadata());
    verify(retriableStreamRecorder).postCommit();
    assertTrue(throttle.isAboveThreshold()); // count = 3

    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 2
  }

  @Test
  public void throttledStream_FailWithRetriableStatusCode_WithRetriablePushback() {
    Throttle throttle = new Throttle(4f, 0.8f);
    RetriableStream<String> retriableStream = newThrottledRetriableStream(throttle);

    ClientStream mockStream = mock(ClientStream.class);
    doReturn(mockStream).when(retriableStreamRecorder).newSubstream(anyInt());
    retriableStream.start(masterListener);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream).start(sublistenerCaptor.capture());

    // mimic some other call in the channel triggers a throttle countdown
    assertTrue(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 3

    int pushbackInMillis = 123;
    Metadata headers = new Metadata();
    headers.put(RetriableStream.GRPC_RETRY_PUSHBACK_MS, "" + pushbackInMillis);
    sublistenerCaptor.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), headers);
    verify(retriableStreamRecorder).postCommit();
    assertFalse(throttle.isAboveThreshold()); // count = 2
  }

  @Test
  public void throttledStream_FailWithNonRetriableStatusCode_WithRetriablePushback() {
    Throttle throttle = new Throttle(4f, 0.8f);
    RetriableStream<String> retriableStream = newThrottledRetriableStream(throttle);

    ClientStream mockStream = mock(ClientStream.class);
    doReturn(mockStream).when(retriableStreamRecorder).newSubstream(anyInt());
    retriableStream.start(masterListener);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream).start(sublistenerCaptor.capture());

    // mimic some other call in the channel triggers a throttle countdown
    assertTrue(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 3

    int pushbackInMillis = 123;
    Metadata headers = new Metadata();
    headers.put(RetriableStream.GRPC_RETRY_PUSHBACK_MS, "" + pushbackInMillis);
    sublistenerCaptor.getValue().closed(Status.fromCode(NON_RETRIABLE_STATUS_CODE), headers);
    verify(retriableStreamRecorder, never()).postCommit();
    assertTrue(throttle.isAboveThreshold()); // count = 3

    // drain pending retry
    fakeClock.forwardTime(pushbackInMillis, TimeUnit.MILLISECONDS);

    assertTrue(throttle.isAboveThreshold()); // count = 3
    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 2
  }

  @Test
  public void throttledStream_FailWithRetriableStatusCode_WithNonRetriablePushback() {
    Throttle throttle = new Throttle(4f, 0.8f);
    RetriableStream<String> retriableStream = newThrottledRetriableStream(throttle);

    ClientStream mockStream = mock(ClientStream.class);
    doReturn(mockStream).when(retriableStreamRecorder).newSubstream(anyInt());
    retriableStream.start(masterListener);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream).start(sublistenerCaptor.capture());

    // mimic some other call in the channel triggers a throttle countdown
    assertTrue(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 3

    Metadata headers = new Metadata();
    headers.put(RetriableStream.GRPC_RETRY_PUSHBACK_MS, "");
    sublistenerCaptor.getValue().closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), headers);
    verify(retriableStreamRecorder).postCommit();
    assertFalse(throttle.isAboveThreshold()); // count = 2
  }

  @Test
  public void throttledStream_FailWithNonRetriableStatusCode_WithNonRetriablePushback() {
    Throttle throttle = new Throttle(4f, 0.8f);
    RetriableStream<String> retriableStream = newThrottledRetriableStream(throttle);

    ClientStream mockStream = mock(ClientStream.class);
    doReturn(mockStream).when(retriableStreamRecorder).newSubstream(anyInt());
    retriableStream.start(masterListener);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream).start(sublistenerCaptor.capture());

    // mimic some other call in the channel triggers a throttle countdown
    assertTrue(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 3

    Metadata headers = new Metadata();
    headers.put(RetriableStream.GRPC_RETRY_PUSHBACK_MS, "");
    sublistenerCaptor.getValue().closed(Status.fromCode(NON_RETRIABLE_STATUS_CODE), headers);
    verify(retriableStreamRecorder).postCommit();
    assertFalse(throttle.isAboveThreshold()); // count = 2
  }

  @Test
  public void throttleStream_Succeed() {
    Throttle throttle = new Throttle(4f, 0.8f);
    RetriableStream<String> retriableStream = newThrottledRetriableStream(throttle);

    ClientStream mockStream = mock(ClientStream.class);
    doReturn(mockStream).when(retriableStreamRecorder).newSubstream(anyInt());
    retriableStream.start(masterListener);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream).start(sublistenerCaptor.capture());

    // mimic some other calls in the channel trigger throttle countdowns
    assertTrue(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 3
    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 2
    assertFalse(throttle.onQualifiedFailureThenCheckIsAboveThreshold()); // count = 1

    sublistenerCaptor.getValue().headersRead(new Metadata());
    verify(retriableStreamRecorder).postCommit();
    assertFalse(throttle.isAboveThreshold());  // count = 1.8

    // mimic some other call in the channel triggers a success
    throttle.onSuccess();
    assertTrue(throttle.isAboveThreshold()); // count = 2.6
  }

  @Test
  public void transparentRetry() {
    ClientStream mockStream1 = mock(ClientStream.class);
    ClientStream mockStream2 = mock(ClientStream.class);
    ClientStream mockStream3 = mock(ClientStream.class);
    InOrder inOrder = inOrder(
        retriableStreamRecorder,
        mockStream1, mockStream2, mockStream3);

    // start
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    retriableStream.start(masterListener);

    inOrder.verify(retriableStreamRecorder).newSubstream(0);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream1).start(sublistenerCaptor1.capture());
    inOrder.verifyNoMoreInteractions();

    // transparent retry
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(0);
    sublistenerCaptor1.getValue()
        .closed(Status.fromCode(NON_RETRIABLE_STATUS_CODE), REFUSED, new Metadata());

    inOrder.verify(retriableStreamRecorder).newSubstream(0);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verifyNoMoreInteractions();
    verify(retriableStreamRecorder, never()).postCommit();
    assertEquals(0, fakeClock.numPendingTasks());

    // no more transparent retry
    doReturn(mockStream3).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor2.getValue()
        .closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), REFUSED, new Metadata());

    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);
    inOrder.verify(retriableStreamRecorder).newSubstream(1);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor3 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream3).start(sublistenerCaptor3.capture());
    inOrder.verifyNoMoreInteractions();
    verify(retriableStreamRecorder, never()).postCommit();
    assertEquals(0, fakeClock.numPendingTasks());
  }

  @Test
  public void normalRetry_thenNoTransparentRetry_butNormalRetry() {
    ClientStream mockStream1 = mock(ClientStream.class);
    ClientStream mockStream2 = mock(ClientStream.class);
    ClientStream mockStream3 = mock(ClientStream.class);
    InOrder inOrder = inOrder(
        retriableStreamRecorder,
        mockStream1, mockStream2, mockStream3);

    // start
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    retriableStream.start(masterListener);

    inOrder.verify(retriableStreamRecorder).newSubstream(0);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream1).start(sublistenerCaptor1.capture());
    inOrder.verifyNoMoreInteractions();

    // normal retry
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue()
        .closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), PROCESSED, new Metadata());

    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);
    inOrder.verify(retriableStreamRecorder).newSubstream(1);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verifyNoMoreInteractions();
    verify(retriableStreamRecorder, never()).postCommit();
    assertEquals(0, fakeClock.numPendingTasks());

    // no more transparent retry
    doReturn(mockStream3).when(retriableStreamRecorder).newSubstream(2);
    sublistenerCaptor2.getValue()
        .closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), REFUSED, new Metadata());

    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime(
        (long) (INITIAL_BACKOFF_IN_SECONDS * BACKOFF_MULTIPLIER * FAKE_RANDOM), TimeUnit.SECONDS);
    inOrder.verify(retriableStreamRecorder).newSubstream(2);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor3 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream3).start(sublistenerCaptor3.capture());
    inOrder.verifyNoMoreInteractions();
    verify(retriableStreamRecorder, never()).postCommit();
  }

  @Test
  public void normalRetry_thenNoTransparentRetry_andNoMoreRetry() {
    ClientStream mockStream1 = mock(ClientStream.class);
    ClientStream mockStream2 = mock(ClientStream.class);
    ClientStream mockStream3 = mock(ClientStream.class);
    InOrder inOrder = inOrder(
        retriableStreamRecorder,
        mockStream1, mockStream2, mockStream3);

    // start
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    retriableStream.start(masterListener);

    inOrder.verify(retriableStreamRecorder).newSubstream(0);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream1).start(sublistenerCaptor1.capture());
    inOrder.verifyNoMoreInteractions();

    // normal retry
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);
    sublistenerCaptor1.getValue()
        .closed(Status.fromCode(RETRIABLE_STATUS_CODE_1), PROCESSED, new Metadata());

    assertEquals(1, fakeClock.numPendingTasks());
    fakeClock.forwardTime((long) (INITIAL_BACKOFF_IN_SECONDS * FAKE_RANDOM), TimeUnit.SECONDS);
    inOrder.verify(retriableStreamRecorder).newSubstream(1);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor2 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    inOrder.verify(mockStream2).start(sublistenerCaptor2.capture());
    inOrder.verifyNoMoreInteractions();
    verify(retriableStreamRecorder, never()).postCommit();
    assertEquals(0, fakeClock.numPendingTasks());

    // no more transparent retry
    doReturn(mockStream3).when(retriableStreamRecorder).newSubstream(2);
    sublistenerCaptor2.getValue()
        .closed(Status.fromCode(NON_RETRIABLE_STATUS_CODE), REFUSED, new Metadata());

    verify(retriableStreamRecorder).postCommit();
  }

  @Test
  public void droppedShouldNeverRetry() {
    ClientStream mockStream1 = mock(ClientStream.class);
    ClientStream mockStream2 = mock(ClientStream.class);
    doReturn(mockStream1).when(retriableStreamRecorder).newSubstream(0);
    doReturn(mockStream2).when(retriableStreamRecorder).newSubstream(1);

    // start
    retriableStream.start(masterListener);

    verify(retriableStreamRecorder).newSubstream(0);
    ArgumentCaptor<ClientStreamListener> sublistenerCaptor1 =
        ArgumentCaptor.forClass(ClientStreamListener.class);
    verify(mockStream1).start(sublistenerCaptor1.capture());

    // drop and verify no retry
    Status status = Status.fromCode(RETRIABLE_STATUS_CODE_1);
    sublistenerCaptor1.getValue().closed(status, DROPPED, new Metadata());

    verifyNoMoreInteractions(mockStream1, mockStream2);
    verify(retriableStreamRecorder).postCommit();
    verify(masterListener).closed(same(status), any(Metadata.class));
  }

  /**
   * Used to stub a retriable stream as well as to record methods of the retriable stream being
   * called.
   */
  private interface RetriableStreamRecorder {
    void postCommit();

    ClientStream newSubstream(int previousAttempts);

    Status prestart();
  }
}
