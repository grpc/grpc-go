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

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.internal.GrpcUtil.ACCEPT_ENCODING_SPLITTER;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyZeroInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableSet;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.Attributes;
import io.grpc.Attributes.Key;
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ClientStreamTracer;
import io.grpc.Codec;
import io.grpc.Context;
import io.grpc.Deadline;
import io.grpc.Decompressor;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;
import io.grpc.internal.testing.SingleMessageProducer;
import io.grpc.testing.TestMethodDescriptors;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.Set;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executor;
import java.util.concurrent.Executors;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Matchers;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Test for {@link ClientCallImpl}.
 */
@RunWith(JUnit4.class)
public class ClientCallImplTest {

  private final FakeClock fakeClock = new FakeClock();
  private final ScheduledExecutorService deadlineCancellationExecutor =
      fakeClock.getScheduledExecutorService();
  private final CallTracer channelCallTracer = CallTracer.getDefaultFactory().create();
  private final DecompressorRegistry decompressorRegistry =
      DecompressorRegistry.getDefaultInstance().with(new Codec.Gzip(), true);
  private final MethodDescriptor<Void, Void> method = MethodDescriptor.<Void, Void>newBuilder()
      .setType(MethodType.UNARY)
      .setFullMethodName("service/method")
      .setRequestMarshaller(TestMethodDescriptors.voidMarshaller())
      .setResponseMarshaller(TestMethodDescriptors.voidMarshaller())
      .build();

  @Mock private ClientStreamListener streamListener;
  @Mock private ClientTransport clientTransport;
  @Captor private ArgumentCaptor<Status> statusCaptor;

  @Mock
  private ClientStreamTracer.Factory streamTracerFactory;

  @Mock
  private ClientTransport transport;

  @Mock
  private ClientTransportProvider provider;

  @Mock
  private ClientStream stream;

  @Mock
  private ClientCall.Listener<Void> callListener;

  @Captor
  private ArgumentCaptor<ClientStreamListener> listenerArgumentCaptor;

  @Captor
  private ArgumentCaptor<Status> statusArgumentCaptor;

  private CallOptions baseCallOptions;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(provider.get(any(PickSubchannelArgsImpl.class))).thenReturn(transport);
    when(transport.newStream(
            any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class)))
        .thenReturn(stream);
    baseCallOptions = CallOptions.DEFAULT.withStreamTracerFactory(streamTracerFactory);
  }

  @After
  public void tearDown() {
    verifyZeroInteractions(streamTracerFactory);
  }

  @Test
  public void statusPropagatedFromStreamToCallListener() {
    DelayedExecutor executor = new DelayedExecutor();
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        executor,
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());
    verify(stream).start(listenerArgumentCaptor.capture());
    final ClientStreamListener streamListener = listenerArgumentCaptor.getValue();
    streamListener.headersRead(new Metadata());
    Status status = Status.RESOURCE_EXHAUSTED.withDescription("simulated");
    streamListener.closed(status , new Metadata());
    executor.release();

    verify(callListener).onClose(same(status), Matchers.isA(Metadata.class));
  }

  @Test
  public void exceptionInOnMessageTakesPrecedenceOverServer() {
    DelayedExecutor executor = new DelayedExecutor();
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        executor,
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());
    verify(stream).start(listenerArgumentCaptor.capture());
    final ClientStreamListener streamListener = listenerArgumentCaptor.getValue();
    streamListener.headersRead(new Metadata());

    RuntimeException failure = new RuntimeException("bad");
    doThrow(failure).when(callListener).onMessage(Matchers.<Void>any());

    /*
     * In unary calls, the server closes the call right after responding, so the onClose call is
     * queued to run.  When messageRead is called, an exception will occur and attempt to cancel the
     * stream.  However, since the server closed it "first" the second exception is lost leading to
     * the call being counted as successful.
     */
    streamListener
        .messagesAvailable(new SingleMessageProducer(new ByteArrayInputStream(new byte[]{})));
    streamListener.closed(Status.OK, new Metadata());
    executor.release();

    verify(callListener).onClose(statusArgumentCaptor.capture(), Matchers.isA(Metadata.class));
    Status callListenerStatus = statusArgumentCaptor.getValue();
    assertThat(callListenerStatus.getCode()).isEqualTo(Status.Code.CANCELLED);
    assertThat(callListenerStatus.getCause()).isSameAs(failure);
    verify(stream).cancel(same(callListenerStatus));
  }

  @Test
  public void exceptionInOnHeadersTakesPrecedenceOverServer() {
    DelayedExecutor executor = new DelayedExecutor();
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        executor,
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());
    verify(stream).start(listenerArgumentCaptor.capture());
    final ClientStreamListener streamListener = listenerArgumentCaptor.getValue();

    RuntimeException failure = new RuntimeException("bad");
    doThrow(failure).when(callListener).onHeaders(any(Metadata.class));

    /*
     * In unary calls, the server closes the call right after responding, so the onClose call is
     * queued to run.  When headersRead is called, an exception will occur and attempt to cancel the
     * stream.  However, since the server closed it "first" the second exception is lost leading to
     * the call being counted as successful.
     */
    streamListener.headersRead(new Metadata());
    streamListener.closed(Status.OK, new Metadata());
    executor.release();

    verify(callListener).onClose(statusArgumentCaptor.capture(), Matchers.isA(Metadata.class));
    Status callListenerStatus = statusArgumentCaptor.getValue();
    assertThat(callListenerStatus.getCode()).isEqualTo(Status.Code.CANCELLED);
    assertThat(callListenerStatus.getCause()).isSameAs(failure);
    verify(stream).cancel(same(callListenerStatus));
  }

  @Test
  public void exceptionInOnReadyTakesPrecedenceOverServer() {
    DelayedExecutor executor = new DelayedExecutor();
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        executor,
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());
    verify(stream).start(listenerArgumentCaptor.capture());
    final ClientStreamListener streamListener = listenerArgumentCaptor.getValue();

    RuntimeException failure = new RuntimeException("bad");
    doThrow(failure).when(callListener).onReady();

    /*
     * In unary calls, the server closes the call right after responding, so the onClose call is
     * queued to run.  When onReady is called, an exception will occur and attempt to cancel the
     * stream.  However, since the server closed it "first" the second exception is lost leading to
     * the call being counted as successful.
     */
    streamListener.onReady();
    streamListener.closed(Status.OK, new Metadata());
    executor.release();

    verify(callListener).onClose(statusArgumentCaptor.capture(), Matchers.isA(Metadata.class));
    Status callListenerStatus = statusArgumentCaptor.getValue();
    assertThat(callListenerStatus.getCode()).isEqualTo(Status.Code.CANCELLED);
    assertThat(callListenerStatus.getCause()).isSameAs(failure);
    verify(stream).cancel(same(callListenerStatus));
  }

  @Test
  public void advertisedEncodingsAreSent() {
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */)
            .setDecompressorRegistry(decompressorRegistry);

    call.start(callListener, new Metadata());

    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);
    verify(transport).newStream(eq(method), metadataCaptor.capture(), same(baseCallOptions));
    Metadata actual = metadataCaptor.getValue();

    // there should only be one.
    Set<String> acceptedEncodings = ImmutableSet.of(
        new String(actual.get(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY), GrpcUtil.US_ASCII));
    assertEquals(decompressorRegistry.getAdvertisedMessageEncodings(), acceptedEncodings);
  }

  @Test
  public void authorityPropagatedToStream() {
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        baseCallOptions.withAuthority("overridden-authority"),
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */)
            .setDecompressorRegistry(decompressorRegistry);

    call.start(callListener, new Metadata());
    verify(stream).setAuthority("overridden-authority");
  }

  @Test
  public void callOptionsPropagatedToTransport() {
    final CallOptions callOptions = baseCallOptions.withAuthority("dummy_value");
    final ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        callOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */)
        .setDecompressorRegistry(decompressorRegistry);
    final Metadata metadata = new Metadata();

    call.start(callListener, metadata);

    verify(transport).newStream(same(method), same(metadata), same(callOptions));
  }

  @Test
  public void authorityNotPropagatedToStream() {
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        // Don't provide an authority
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */)
            .setDecompressorRegistry(decompressorRegistry);

    call.start(callListener, new Metadata());
    verify(stream, never()).setAuthority(any(String.class));
  }

  @Test
  public void prepareHeaders_userAgentIgnored() {
    Metadata m = new Metadata();
    m.put(GrpcUtil.USER_AGENT_KEY, "batmobile");
    ClientCallImpl.prepareHeaders(m, decompressorRegistry, Codec.Identity.NONE, false);

    // User Agent is removed and set by the transport
    assertThat(m.get(GrpcUtil.USER_AGENT_KEY)).isNotNull();
  }

  @Test
  public void prepareHeaders_ignoreIdentityEncoding() {
    Metadata m = new Metadata();
    ClientCallImpl.prepareHeaders(m, decompressorRegistry, Codec.Identity.NONE, false);

    assertNull(m.get(GrpcUtil.MESSAGE_ENCODING_KEY));
  }

  @Test
  public void prepareHeaders_acceptedMessageEncodingsAdded() {
    Metadata m = new Metadata();
    DecompressorRegistry customRegistry = DecompressorRegistry.emptyInstance()
        .with(new Decompressor() {
          @Override
          public String getMessageEncoding() {
            return "a";
          }

          @Override
          public InputStream decompress(InputStream is) throws IOException {
            return null;
          }
        }, true)
        .with(new Decompressor() {
          @Override
          public String getMessageEncoding() {
            return "b";
          }

          @Override
          public InputStream decompress(InputStream is) throws IOException {
            return null;
          }
        }, true)
        .with(new Decompressor() {
          @Override
          public String getMessageEncoding() {
            return "c";
          }

          @Override
          public InputStream decompress(InputStream is) throws IOException {
            return null;
          }
        }, false); // not advertised

    ClientCallImpl.prepareHeaders(m, customRegistry, Codec.Identity.NONE, false);

    Iterable<String> acceptedEncodings = ACCEPT_ENCODING_SPLITTER.split(
        new String(m.get(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY), GrpcUtil.US_ASCII));

    // Order may be different, since decoder priorities have not yet been implemented.
    assertEquals(ImmutableSet.of("b", "a"), ImmutableSet.copyOf(acceptedEncodings));
  }

  @Test
  public void prepareHeaders_noAcceptedContentEncodingsWithoutFullStreamDecompressionEnabled() {
    Metadata m = new Metadata();
    ClientCallImpl.prepareHeaders(m, decompressorRegistry, Codec.Identity.NONE, false);

    assertNull(m.get(GrpcUtil.CONTENT_ACCEPT_ENCODING_KEY));
  }

  @Test
  public void prepareHeaders_acceptedMessageAndContentEncodingsAdded() {
    Metadata m = new Metadata();
    DecompressorRegistry customRegistry =
        DecompressorRegistry.emptyInstance()
            .with(
                new Decompressor() {
                  @Override
                  public String getMessageEncoding() {
                    return "a";
                  }

                  @Override
                  public InputStream decompress(InputStream is) throws IOException {
                    return null;
                  }
                },
                true)
            .with(
                new Decompressor() {
                  @Override
                  public String getMessageEncoding() {
                    return "b";
                  }

                  @Override
                  public InputStream decompress(InputStream is) throws IOException {
                    return null;
                  }
                },
                true)
            .with(
                new Decompressor() {
                  @Override
                  public String getMessageEncoding() {
                    return "c";
                  }

                  @Override
                  public InputStream decompress(InputStream is) throws IOException {
                    return null;
                  }
                },
                false); // not advertised

    ClientCallImpl.prepareHeaders(m, customRegistry, Codec.Identity.NONE, true);

    Iterable<String> acceptedMessageEncodings =
        ACCEPT_ENCODING_SPLITTER.split(
            new String(m.get(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY), GrpcUtil.US_ASCII));
    // Order may be different, since decoder priorities have not yet been implemented.
    assertEquals(ImmutableSet.of("b", "a"), ImmutableSet.copyOf(acceptedMessageEncodings));

    Iterable<String> acceptedContentEncodings =
        ACCEPT_ENCODING_SPLITTER.split(
            new String(m.get(GrpcUtil.CONTENT_ACCEPT_ENCODING_KEY), GrpcUtil.US_ASCII));
    assertEquals(
        ImmutableSet.of("gzip"), ImmutableSet.copyOf(acceptedContentEncodings));
  }

  @Test
  public void prepareHeaders_removeReservedHeaders() {
    Metadata m = new Metadata();
    m.put(GrpcUtil.MESSAGE_ENCODING_KEY, "gzip");
    m.put(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY, "gzip".getBytes(GrpcUtil.US_ASCII));
    m.put(GrpcUtil.CONTENT_ENCODING_KEY, "gzip");
    m.put(GrpcUtil.CONTENT_ACCEPT_ENCODING_KEY, "gzip".getBytes(GrpcUtil.US_ASCII));

    ClientCallImpl.prepareHeaders(
        m, DecompressorRegistry.emptyInstance(), Codec.Identity.NONE, false);

    assertNull(m.get(GrpcUtil.MESSAGE_ENCODING_KEY));
    assertNull(m.get(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY));
    assertNull(m.get(GrpcUtil.CONTENT_ENCODING_KEY));
    assertNull(m.get(GrpcUtil.CONTENT_ACCEPT_ENCODING_KEY));
  }

  @Test
  public void callerContextPropagatedToListener() throws Exception {
    // Attach the context which is recorded when the call is created
    final Context.Key<String> testKey = Context.key("testing");
    Context context = Context.current().withValue(testKey, "testValue");
    Context previous = context.attach();

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        new SerializingExecutor(Executors.newSingleThreadExecutor()),
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */)
            .setDecompressorRegistry(decompressorRegistry);

    context.detach(previous);

    // Override the value after creating the call, this should not be seen by callbacks
    context = Context.current().withValue(testKey, "badValue");
    previous = context.attach();

    final AtomicBoolean onHeadersCalled = new AtomicBoolean();
    final AtomicBoolean onMessageCalled = new AtomicBoolean();
    final AtomicBoolean onReadyCalled = new AtomicBoolean();
    final AtomicBoolean observedIncorrectContext = new AtomicBoolean();
    final CountDownLatch latch = new CountDownLatch(1);

    call.start(new ClientCall.Listener<Void>() {
      @Override
      public void onHeaders(Metadata headers) {
        onHeadersCalled.set(true);
        checkContext();
      }

      @Override
      public void onMessage(Void message) {
        onMessageCalled.set(true);
        checkContext();
      }

      @Override
      public void onClose(Status status, Metadata trailers) {
        checkContext();
        latch.countDown();
      }

      @Override
      public void onReady() {
        onReadyCalled.set(true);
        checkContext();
      }

      private void checkContext() {
        if (!"testValue".equals(testKey.get())) {
          observedIncorrectContext.set(true);
        }
      }
    }, new Metadata());

    context.detach(previous);

    verify(stream).start(listenerArgumentCaptor.capture());
    ClientStreamListener listener = listenerArgumentCaptor.getValue();
    listener.onReady();
    listener.headersRead(new Metadata());
    listener.messagesAvailable(new SingleMessageProducer(new ByteArrayInputStream(new byte[0])));
    listener.messagesAvailable(new SingleMessageProducer(new ByteArrayInputStream(new byte[0])));
    listener.closed(Status.OK, new Metadata());

    assertTrue(latch.await(5, TimeUnit.SECONDS));

    assertTrue(onHeadersCalled.get());
    assertTrue(onMessageCalled.get());
    assertTrue(onReadyCalled.get());
    assertFalse(observedIncorrectContext.get());
  }

  @Test
  public void contextCancellationCancelsStream() throws Exception {
    // Attach the context which is recorded when the call is created
    Context.CancellableContext cancellableContext = Context.current().withCancellation();
    Context previous = cancellableContext.attach();

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        new SerializingExecutor(Executors.newSingleThreadExecutor()),
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */)
            .setDecompressorRegistry(decompressorRegistry);

    cancellableContext.detach(previous);

    call.start(callListener, new Metadata());

    Throwable t = new Throwable();
    cancellableContext.cancel(t);

    verify(stream, times(1)).cancel(statusArgumentCaptor.capture());
    Status streamStatus = statusArgumentCaptor.getValue();
    assertEquals(Status.Code.CANCELLED, streamStatus.getCode());
  }

  @Test
  public void contextAlreadyCancelledNotifiesImmediately() throws Exception {
    // Attach the context which is recorded when the call is created
    Context.CancellableContext cancellableContext = Context.current().withCancellation();
    Throwable cause = new Throwable();
    cancellableContext.cancel(cause);
    Context previous = cancellableContext.attach();

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        new SerializingExecutor(Executors.newSingleThreadExecutor()),
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */)
        .setDecompressorRegistry(decompressorRegistry);

    cancellableContext.detach(previous);

    final SettableFuture<Status> statusFuture = SettableFuture.create();
    call.start(new ClientCall.Listener<Void>() {
      @Override
      public void onClose(Status status, Metadata trailers) {
        statusFuture.set(status);
      }
    }, new Metadata());

    // Caller should receive onClose callback.
    Status status = statusFuture.get(5, TimeUnit.SECONDS);
    assertEquals(Status.Code.CANCELLED, status.getCode());
    assertSame(cause, status.getCause());

    // Following operations should be no-op.
    call.request(1);
    call.sendMessage(null);
    call.halfClose();

    // Stream should never be created.
    verifyZeroInteractions(transport);

    try {
      call.sendMessage(null);
      fail("Call has been cancelled");
    } catch (IllegalStateException ise) {
      // expected
    }
  }

  @Test
  public void deadlineExceededBeforeCallStarted() {
    CallOptions callOptions = baseCallOptions.withDeadlineAfter(0, TimeUnit.SECONDS);
    fakeClock.forwardTime(System.nanoTime(), TimeUnit.NANOSECONDS);
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        new SerializingExecutor(Executors.newSingleThreadExecutor()),
        callOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */)
            .setDecompressorRegistry(decompressorRegistry);
    call.start(callListener, new Metadata());
    verify(transport, times(0))
        .newStream(any(MethodDescriptor.class), any(Metadata.class), any(CallOptions.class));
    verify(callListener, timeout(1000)).onClose(statusCaptor.capture(), any(Metadata.class));
    assertEquals(Status.Code.DEADLINE_EXCEEDED, statusCaptor.getValue().getCode());
    verifyZeroInteractions(provider);
  }

  @Test
  public void contextDeadlineShouldBePropagatedToStream() {
    Context context = Context.current()
        .withDeadlineAfter(1000, TimeUnit.MILLISECONDS, deadlineCancellationExecutor);
    Context origContext = context.attach();

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());

    context.detach(origContext);

    ArgumentCaptor<Deadline> deadlineCaptor = ArgumentCaptor.forClass(Deadline.class);
    verify(stream).setDeadline(deadlineCaptor.capture());

    assertTimeoutBetween(deadlineCaptor.getValue().timeRemaining(TimeUnit.MILLISECONDS), 600, 1000);
  }

  @Test
  public void contextDeadlineShouldOverrideLargerCallOptionsDeadline() {
    Context context = Context.current()
        .withDeadlineAfter(1000, TimeUnit.MILLISECONDS, deadlineCancellationExecutor);
    Context origContext = context.attach();

    CallOptions callOpts = baseCallOptions.withDeadlineAfter(2000, TimeUnit.MILLISECONDS);
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        callOpts,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());

    context.detach(origContext);

    ArgumentCaptor<Deadline> deadlineCaptor = ArgumentCaptor.forClass(Deadline.class);
    verify(stream).setDeadline(deadlineCaptor.capture());

    assertTimeoutBetween(deadlineCaptor.getValue().timeRemaining(TimeUnit.MILLISECONDS), 600, 1000);
  }

  @Test
  public void contextDeadlineShouldNotOverrideSmallerCallOptionsDeadline() {
    Context context = Context.current()
        .withDeadlineAfter(2000, TimeUnit.MILLISECONDS, deadlineCancellationExecutor);
    Context origContext = context.attach();

    CallOptions callOpts = baseCallOptions.withDeadlineAfter(1000, TimeUnit.MILLISECONDS);
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        callOpts,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());

    context.detach(origContext);

    ArgumentCaptor<Deadline> deadlineCaptor = ArgumentCaptor.forClass(Deadline.class);
    verify(stream).setDeadline(deadlineCaptor.capture());

    assertTimeoutBetween(deadlineCaptor.getValue().timeRemaining(TimeUnit.MILLISECONDS), 600, 1000);
  }

  @Test
  public void callOptionsDeadlineShouldBePropagatedToStream() {
    CallOptions callOpts = baseCallOptions.withDeadlineAfter(1000, TimeUnit.MILLISECONDS);
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        callOpts,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());

    ArgumentCaptor<Deadline> deadlineCaptor = ArgumentCaptor.forClass(Deadline.class);
    verify(stream).setDeadline(deadlineCaptor.capture());

    assertTimeoutBetween(deadlineCaptor.getValue().timeRemaining(TimeUnit.MILLISECONDS), 600, 1000);
  }

  @Test
  public void noDeadlineShouldBePropagatedToStream() {
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());

    verify(stream, never()).setDeadline(any(Deadline.class));
  }

  @Test
  public void expiredDeadlineCancelsStream_CallOptions() {
    fakeClock.forwardTime(System.nanoTime(), TimeUnit.NANOSECONDS);
    // The deadline needs to be a number large enough to get encompass the call to start, otherwise
    // the scheduled cancellation won't be created, and the call will fail early.
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        baseCallOptions.withDeadline(Deadline.after(1, TimeUnit.SECONDS)),
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);

    call.start(callListener, new Metadata());

    fakeClock.forwardNanos(TimeUnit.SECONDS.toNanos(1) + 1);

    verify(stream, times(1)).cancel(statusCaptor.capture());
    assertEquals(Status.Code.DEADLINE_EXCEEDED, statusCaptor.getValue().getCode());
  }

  @Test
  public void expiredDeadlineCancelsStream_Context() {
    fakeClock.forwardTime(System.nanoTime(), TimeUnit.NANOSECONDS);

    Context context = Context.current()
        .withDeadlineAfter(1, TimeUnit.SECONDS, deadlineCancellationExecutor);
    Context origContext = context.attach();

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);

    context.detach(origContext);

    call.start(callListener, new Metadata());

    fakeClock.forwardNanos(TimeUnit.SECONDS.toNanos(1) + 1);

    verify(stream, times(1)).cancel(statusCaptor.capture());
    assertEquals(Status.Code.DEADLINE_EXCEEDED, statusCaptor.getValue().getCode());
  }

  @Test
  public void streamCancelAbortsDeadlineTimer() {
    fakeClock.forwardTime(System.nanoTime(), TimeUnit.NANOSECONDS);

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        baseCallOptions.withDeadline(Deadline.after(1, TimeUnit.SECONDS)),
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    call.start(callListener, new Metadata());
    call.cancel("canceled", null);

    // Run the deadline timer, which should have been cancelled by the previous call to cancel()
    fakeClock.forwardNanos(TimeUnit.SECONDS.toNanos(1) + 1);

    verify(stream, times(1)).cancel(statusCaptor.capture());

    assertEquals(Status.CANCELLED.getCode(), statusCaptor.getValue().getCode());
  }

  /**
   * Without a context or call options deadline,
   * a timeout should not be set in metadata.
   */
  @Test
  public void timeoutShouldNotBeSet() {
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);

    Metadata headers = new Metadata();

    call.start(callListener, headers);

    assertFalse(headers.containsKey(GrpcUtil.TIMEOUT_KEY));
  }

  @Test
  public void cancelInOnMessageShouldInvokeStreamCancel() throws Exception {
    final ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        baseCallOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */);
    final Exception cause = new Exception();
    ClientCall.Listener<Void> callListener =
        new ClientCall.Listener<Void>() {
          @Override
          public void onMessage(Void message) {
            call.cancel("foo", cause);
          }
        };

    call.start(callListener, new Metadata());
    call.halfClose();
    call.request(1);

    verify(stream).start(listenerArgumentCaptor.capture());
    ClientStreamListener streamListener = listenerArgumentCaptor.getValue();
    streamListener.onReady();
    streamListener.headersRead(new Metadata());
    streamListener
        .messagesAvailable(new SingleMessageProducer(new ByteArrayInputStream(new byte[0])));
    verify(stream).cancel(statusCaptor.capture());
    Status status = statusCaptor.getValue();
    assertEquals(Status.CANCELLED.getCode(), status.getCode());
    assertEquals("foo", status.getDescription());
    assertSame(cause, status.getCause());
  }

  @Test
  public void startAddsMaxSize() {
    CallOptions callOptions =
        baseCallOptions.withMaxInboundMessageSize(1).withMaxOutboundMessageSize(2);
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        new SerializingExecutor(Executors.newSingleThreadExecutor()),
        callOptions,
        provider,
        deadlineCancellationExecutor,
        channelCallTracer,
        false /* retryEnabled */)
            .setDecompressorRegistry(decompressorRegistry);

    call.start(callListener, new Metadata());

    verify(stream).setMaxInboundMessageSize(1);
    verify(stream).setMaxOutboundMessageSize(2);
  }

  @Test
  public void getAttributes() {
    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method, MoreExecutors.directExecutor(), baseCallOptions, provider,
        deadlineCancellationExecutor, channelCallTracer, false /* retryEnabled */);
    Attributes attrs =
        Attributes.newBuilder().set(Key.<String>create("fake key"), "fake value").build();
    when(stream.getAttributes()).thenReturn(attrs);

    assertNotEquals(attrs, call.getAttributes());

    call.start(callListener, new Metadata());

    assertEquals(attrs, call.getAttributes());
  }

  private static void assertTimeoutBetween(long timeout, long from, long to) {
    assertTrue("timeout: " + timeout + " ns", timeout <= to);
    assertTrue("timeout: " + timeout + " ns", timeout >= from);
  }

  private static final class DelayedExecutor implements Executor {
    private final BlockingQueue<Runnable> commands = new LinkedBlockingQueue<Runnable>();

    @Override
    public void execute(Runnable command) {
      commands.add(command);
    }

    void release() {
      while (!commands.isEmpty()) {
        commands.poll().run();
      }
    }
  }
}
