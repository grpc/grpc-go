/*
 * Copyright 2015, Google Inc. All rights reserved.
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

package io.grpc.internal;

import static io.grpc.internal.GrpcUtil.ACCEPT_ENCODING_SPLITER;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyZeroInteractions;
import static org.mockito.Mockito.when;

import com.google.common.collect.ImmutableSet;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Codec;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.Decompressor;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;
import io.grpc.internal.ClientCallImpl.StreamCreationTask;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.HashSet;
import java.util.Set;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * Test for {@link ClientCallImpl}.
 */
@RunWith(JUnit4.class)
public class ClientCallImplTest {

  private static final MethodDescriptor<Void, Void> DESCRIPTOR = MethodDescriptor.create(
      MethodType.UNARY,
      "service/method",
      new TestMarshaller<Void>(),
      new TestMarshaller<Void>());

  private final ScheduledExecutorService deadlineCancellationExecutor =
      Executors.newScheduledThreadPool(0);
  private final Set<String> knownMessageEncodings = new HashSet<String>();
  private final CompressorRegistry compressorRegistry = CompressorRegistry.getDefaultInstance();
  private final DecompressorRegistry decompressorRegistry =
      DecompressorRegistry.getDefaultInstance();
  private final MethodDescriptor<Void, Void> method = MethodDescriptor.create(
      MethodType.UNARY,
      "service/method",
      new TestMarshaller<Void>(),
      new TestMarshaller<Void>());

  @Mock private ClientStreamListener streamListener;
  @Mock private ClientTransport clientTransport;
  @Mock private DelayedStream delayedStream;
  @Captor private ArgumentCaptor<Status> statusCaptor;

  @Mock
  private ClientTransport transport;

  private final ClientTransportProvider provider = new ClientTransportProvider() {
    @Override
    public ListenableFuture<ClientTransport> get(CallOptions callOptions) {
      return Futures.immediateFuture(transport);
    }
  };

  @Mock
  private ClientStream stream;

  @Captor
  private ArgumentCaptor<ClientStreamListener> listenerArgumentCaptor;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    decompressorRegistry.register(new Codec.Gzip(), true);
  }

  @After
  public void tearDown() {
    Context.ROOT.attach();
  }

  @Test
  public void advertisedEncodingsAreSent() {
    final ClientTransport transport = mock(ClientTransport.class);
    final ClientStream stream = mock(ClientStream.class);
    ClientTransportProvider provider = new ClientTransportProvider() {
      @Override
      public ListenableFuture<ClientTransport> get(CallOptions callOptions) {
        return Futures.immediateFuture(transport);
      }
    };

    when(transport.newStream(any(MethodDescriptor.class), any(Metadata.class),
        any(ClientStreamListener.class))).thenReturn(stream);

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        method,
        MoreExecutors.directExecutor(),
        CallOptions.DEFAULT,
        provider,
        deadlineCancellationExecutor)
            .setDecompressorRegistry(decompressorRegistry)
            .setCompressorRegistry(compressorRegistry)
            .setKnownMessageEncodingRegistry(knownMessageEncodings);

    call.start(new TestClientCallListener<Void>(), new Metadata());

    ArgumentCaptor<Metadata> metadataCaptor = ArgumentCaptor.forClass(Metadata.class);
    verify(transport).newStream(
        eq(method), metadataCaptor.capture(), isA(ClientStreamListener.class));
    Metadata actual = metadataCaptor.getValue();

    Set<String> acceptedEncodings =
        ImmutableSet.copyOf(actual.getAll(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY));
    assertEquals(decompressorRegistry.getAdvertisedMessageEncodings(), acceptedEncodings);
  }

  @Test
  public void prepareHeaders_authorityAdded() {
    Metadata m = new Metadata();
    CallOptions callOptions = CallOptions.DEFAULT.withAuthority("auth");
    ClientCallImpl.prepareHeaders(m, callOptions, "user agent", knownMessageEncodings,
        decompressorRegistry, compressorRegistry);

    assertEquals(m.get(GrpcUtil.AUTHORITY_KEY), "auth");
  }

  @Test
  public void prepareHeaders_userAgentAdded() {
    Metadata m = new Metadata();
    ClientCallImpl.prepareHeaders(m, CallOptions.DEFAULT, "user agent", knownMessageEncodings,
        decompressorRegistry, compressorRegistry);

    assertEquals(m.get(GrpcUtil.USER_AGENT_KEY), "user agent");
  }

  @Test
  public void prepareHeaders_messageEncodingAdded() {
    Metadata m = new Metadata();
    knownMessageEncodings.add(new Codec.Gzip().getMessageEncoding());
    ClientCallImpl.prepareHeaders(m, CallOptions.DEFAULT, "user agent", knownMessageEncodings,
        decompressorRegistry, compressorRegistry);

    assertEquals(m.get(GrpcUtil.MESSAGE_ENCODING_KEY), new Codec.Gzip().getMessageEncoding());
  }

  @Test
  public void prepareHeaders_ignoreIdentityEncoding() {
    Metadata m = new Metadata();
    knownMessageEncodings.add(Codec.Identity.NONE.getMessageEncoding());
    ClientCallImpl.prepareHeaders(m,  CallOptions.DEFAULT, "user agent", knownMessageEncodings,
        decompressorRegistry, compressorRegistry);

    assertNull(m.get(GrpcUtil.MESSAGE_ENCODING_KEY));
  }

  @Test
  public void prepareHeaders_acceptedEncodingsAdded() {
    Metadata m = new Metadata();
    DecompressorRegistry customRegistry = DecompressorRegistry.newEmptyInstance();
    customRegistry.register(new Decompressor() {
      @Override
      public String getMessageEncoding() {
        return "a";
      }

      @Override
      public InputStream decompress(InputStream is) throws IOException {
        return null;
      }
    }, true);
    customRegistry.register(new Decompressor() {
      @Override
      public String getMessageEncoding() {
        return "b";
      }

      @Override
      public InputStream decompress(InputStream is) throws IOException {
        return null;
      }
    }, true);
    customRegistry.register(new Decompressor() {
      @Override
      public String getMessageEncoding() {
        return "c";
      }

      @Override
      public InputStream decompress(InputStream is) throws IOException {
        return null;
      }
    }, false); // not advertised

    ClientCallImpl.prepareHeaders(m, CallOptions.DEFAULT, "user agent", knownMessageEncodings,
        customRegistry, compressorRegistry);

    Iterable<String> acceptedEncodings =
        ACCEPT_ENCODING_SPLITER.split(m.get(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY));

    // Order may be different, since decoder priorities have not yet been implemented.
    assertEquals(ImmutableSet.of("b", "a"), ImmutableSet.copyOf(acceptedEncodings));
  }

  @Test
  public void prepareHeaders_removeReservedHeaders() {
    Metadata m = new Metadata();
    m.put(GrpcUtil.AUTHORITY_KEY, "auth");
    m.put(GrpcUtil.USER_AGENT_KEY, "user agent");
    m.put(GrpcUtil.MESSAGE_ENCODING_KEY, "gzip");
    m.put(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY, "gzip");

    ClientCallImpl.prepareHeaders(m, CallOptions.DEFAULT, null, knownMessageEncodings,
        DecompressorRegistry.newEmptyInstance(), compressorRegistry);

    assertNull(m.get(GrpcUtil.AUTHORITY_KEY));
    assertNull(m.get(GrpcUtil.USER_AGENT_KEY));
    assertNull(m.get(GrpcUtil.MESSAGE_ENCODING_KEY));
    assertNull(m.get(GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY));
  }

  @Test
  public void callerContextPropagatedToListener() throws Exception {
    when(transport.newStream(any(MethodDescriptor.class), any(Metadata.class),
        listenerArgumentCaptor.capture())).thenReturn(stream);

    // Attach the context which is recorded when the call is created
    final Context.Key<String> testKey = Context.key("testing");
    Context.current().withValue(testKey, "testValue").attach();

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        DESCRIPTOR,
        new SerializingExecutor(Executors.newSingleThreadExecutor()),
        CallOptions.DEFAULT,
        provider,
        deadlineCancellationExecutor)
            .setDecompressorRegistry(decompressorRegistry)
            .setCompressorRegistry(compressorRegistry)
            .setKnownMessageEncodingRegistry(knownMessageEncodings);

    Context.ROOT.attach();

    // Override the value after creating the call, this should not be seen by callbacks
    Context.current().withValue(testKey, "badValue").attach();

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

    ClientStreamListener listener = listenerArgumentCaptor.getValue();
    listener.onReady();
    listener.headersRead(new Metadata());
    listener.messageRead(new ByteArrayInputStream(new byte[0]));
    listener.messageRead(new ByteArrayInputStream(new byte[0]));
    listener.closed(Status.OK, new Metadata());

    assertTrue(latch.await(5, TimeUnit.SECONDS));

    assertTrue(onHeadersCalled.get());
    assertTrue(onMessageCalled.get());
    assertTrue(onReadyCalled.get());
    assertFalse(observedIncorrectContext.get());
  }

  @Test
  public void contextCancellationCancelsStream() throws Exception {
    when(transport.newStream(any(MethodDescriptor.class), any(Metadata.class),
        listenerArgumentCaptor.capture())).thenReturn(stream);

    // Attach the context which is recorded when the call is created
    Context.CancellableContext cancellableContext = Context.current().withCancellation();
    Context previous = cancellableContext.attach();

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        DESCRIPTOR,
        new SerializingExecutor(Executors.newSingleThreadExecutor()),
        CallOptions.DEFAULT,
        provider,
        deadlineCancellationExecutor)
            .setDecompressorRegistry(decompressorRegistry)
            .setCompressorRegistry(compressorRegistry)
            .setKnownMessageEncodingRegistry(knownMessageEncodings);

    previous.attach();

    call.start(new ClientCall.Listener<Void>() {}, new Metadata());

    ClientStreamListener listener = listenerArgumentCaptor.getValue();
    listener.onReady();
    listener.headersRead(new Metadata());
    listener.messageRead(new ByteArrayInputStream(new byte[0]));

    cancellableContext.cancel(new Throwable());

    verify(stream, times(1)).cancel(Status.CANCELLED);

    try {
      call.sendMessage(null);
      fail("Call has been cancelled");
    } catch (IllegalStateException ise) {
      // expected
    }
  }

  @Test
  public void contextAlreadyCancelledNotifiesImmediately() throws Exception {
    // Attach the context which is recorded when the call is created
    Context.CancellableContext cancellableContext = Context.current().withCancellation();
    Throwable cause = new Throwable();
    cancellableContext.cancel(cause);
    Context previous = cancellableContext.attach();

    ClientCallImpl<Void, Void> call = new ClientCallImpl<Void, Void>(
        DESCRIPTOR,
        new SerializingExecutor(Executors.newSingleThreadExecutor()),
        CallOptions.DEFAULT,
        provider,
        deadlineCancellationExecutor)
        .setDecompressorRegistry(decompressorRegistry);

    previous.attach();

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
  public void streamCreationTask_failure() {
    StreamCreationTask task = new StreamCreationTask(
        delayedStream, new Metadata(), method, CallOptions.DEFAULT, streamListener);

    task.onFailure(Status.CANCELLED.asException());

    verify(delayedStream).maybeClosePrematurely(statusCaptor.capture());
    assertEquals(Status.Code.CANCELLED, statusCaptor.getValue().getCode());
  }

  @Test
  public void streamCreationTask_alreadyCancelled() {
    StreamCreationTask task = new StreamCreationTask(
        delayedStream, new Metadata(), method, CallOptions.DEFAULT, streamListener);

    when(delayedStream.cancelledPrematurely()).thenReturn(true);
    task.onSuccess(clientTransport);
    verifyZeroInteractions(clientTransport);
  }

  @Test
  public void streamCreationTask_transportShutdown() {
    StreamCreationTask task = new StreamCreationTask(
        delayedStream, new Metadata(), method, CallOptions.DEFAULT, streamListener);

    // null means no transport available
    task.onSuccess(null);

    verify(delayedStream).maybeClosePrematurely(statusCaptor.capture());
    assertEquals(Status.Code.UNAVAILABLE, statusCaptor.getValue().getCode());
  }

  @Test
  public void streamCreationTask_deadlineExceeded() {
    Metadata headers = new Metadata();
    headers.put(GrpcUtil.TIMEOUT_KEY, 1L);
    CallOptions callOptions = CallOptions.DEFAULT.withDeadlineNanoTime(System.nanoTime() - 1);
    StreamCreationTask task =
        new StreamCreationTask(delayedStream, headers, method, callOptions, streamListener);

    task.onSuccess(clientTransport);

    verify(delayedStream).maybeClosePrematurely(statusCaptor.capture());
    assertEquals(Status.Code.DEADLINE_EXCEEDED, statusCaptor.getValue().getCode());
  }

  @Test
  public void streamCreationTask_success() {
    Metadata headers = new Metadata();
    StreamCreationTask task =
        new StreamCreationTask(delayedStream, headers, method, CallOptions.DEFAULT, streamListener);
    when(clientTransport.newStream(method, headers, streamListener))
        .thenReturn(NoopClientStream.INSTANCE);

    task.onSuccess(clientTransport);

    verify(clientTransport).newStream(method, headers, streamListener);
  }

  private static class TestMarshaller<T> implements Marshaller<T> {
    @Override
    public InputStream stream(T value) {
      return null;
    }

    @Override
    public T parse(InputStream stream) {
      return null;
    }
  }

  private static class TestClientCallListener<T> extends ClientCall.Listener<T> {
  }
}

