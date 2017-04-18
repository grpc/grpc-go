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

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.io.CharStreams;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.DecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCall;
import io.grpc.Status;
import io.grpc.internal.ServerCallImpl.ServerStreamListenerImpl;
import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.io.InputStreamReader;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;

@RunWith(JUnit4.class)
public class ServerCallImplTest {
  @Rule public final ExpectedException thrown = ExpectedException.none();
  @Mock private ServerStream stream;
  @Mock private ServerCall.Listener<Long> callListener;
  @Captor private ArgumentCaptor<Status> statusCaptor;

  private ServerCallImpl<Long, Long> call;
  private Context.CancellableContext context;

  private final MethodDescriptor<Long, Long> method = MethodDescriptor.<Long, Long>newBuilder()
      .setType(MethodType.UNARY)
      .setFullMethodName("/service/method")
      .setRequestMarshaller(new LongMarshaller())
      .setResponseMarshaller(new LongMarshaller())
      .build();

  private final Metadata requestHeaders = new Metadata();

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    context = Context.ROOT.withCancellation();
    call = new ServerCallImpl<Long, Long>(stream, method, requestHeaders, context,
        DecompressorRegistry.getDefaultInstance(), CompressorRegistry.getDefaultInstance());
  }

  @Test
  public void request() {
    call.request(10);

    verify(stream).request(10);
  }

  @Test
  public void sendHeader_firstCall() {
    Metadata headers = new Metadata();

    call.sendHeaders(headers);

    verify(stream).writeHeaders(headers);
  }

  @Test
  public void sendHeader_failsOnSecondCall() {
    call.sendHeaders(new Metadata());
    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("sendHeaders has already been called");

    call.sendHeaders(new Metadata());
  }

  @Test
  public void sendHeader_failsOnClosed() {
    call.close(Status.CANCELLED, new Metadata());

    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("call is closed");

    call.sendHeaders(new Metadata());
  }

  @Test
  public void sendMessage() {
    call.sendHeaders(new Metadata());
    call.sendMessage(1234L);

    verify(stream).writeMessage(isA(InputStream.class));
    verify(stream).flush();
  }

  @Test
  public void sendMessage_failsOnClosed() {
    call.sendHeaders(new Metadata());
    call.close(Status.CANCELLED, new Metadata());

    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("call is closed");

    call.sendMessage(1234L);
  }

  @Test
  public void sendMessage_failsIfheadersUnsent() {
    thrown.expect(IllegalStateException.class);
    thrown.expectMessage("sendHeaders has not been called");

    call.sendMessage(1234L);
  }

  @Test
  public void sendMessage_closesOnFailure() {
    call.sendHeaders(new Metadata());
    doThrow(new RuntimeException("bad")).when(stream).writeMessage(isA(InputStream.class));

    try {
      call.sendMessage(1234L);
      fail();
    } catch (RuntimeException e) {
      // expected
    }

    verify(stream).close(isA(Status.class), isA(Metadata.class));
  }

  @Test
  public void isReady() {
    when(stream.isReady()).thenReturn(true);

    assertTrue(call.isReady());
  }

  @Test
  public void getAuthority() {
    when(stream.getAuthority()).thenReturn("fooapi.googleapis.com");
    assertEquals("fooapi.googleapis.com", call.getAuthority());
    verify(stream).getAuthority();
  }

  @Test
  public void getNullAuthority() {
    when(stream.getAuthority()).thenReturn(null);
    assertNull(call.getAuthority());
    verify(stream).getAuthority();
  }

  @Test
  public void setMessageCompression() {
    call.setMessageCompression(true);

    verify(stream).setMessageCompression(true);
  }

  @Test
  public void streamListener_halfClosed() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);

    streamListener.halfClosed();

    verify(callListener).onHalfClose();
  }

  @Test
  public void streamListener_halfClosed_onlyOnce() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);
    streamListener.halfClosed();
    // canceling the call should short circuit future halfClosed() calls.
    streamListener.closed(Status.CANCELLED);

    streamListener.halfClosed();

    verify(callListener).onHalfClose();
  }

  @Test
  public void streamListener_closedOk() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);

    streamListener.closed(Status.OK);

    verify(callListener).onComplete();
    assertTrue(context.isCancelled());
    assertNull(context.cancellationCause());
  }

  @Test
  public void streamListener_closedCancelled() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);

    streamListener.closed(Status.CANCELLED);

    verify(callListener).onCancel();
    assertTrue(context.isCancelled());
    assertNull(context.cancellationCause());
  }

  @Test
  public void streamListener_onReady() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);

    streamListener.onReady();

    verify(callListener).onReady();
  }

  @Test
  public void streamListener_onReady_onlyOnce() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);
    streamListener.onReady();
    // canceling the call should short circuit future halfClosed() calls.
    streamListener.closed(Status.CANCELLED);

    streamListener.onReady();

    verify(callListener).onReady();
  }

  @Test
  public void streamListener_messageRead() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);
    streamListener.messageRead(method.streamRequest(1234L));

    verify(callListener).onMessage(1234L);
  }

  @Test
  public void streamListener_messageRead_unaryFailsOnMultiple() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);
    streamListener.messageRead(method.streamRequest(1234L));
    streamListener.messageRead(method.streamRequest(1234L));

    // Makes sure this was only called once.
    verify(callListener).onMessage(1234L);

    verify(stream).close(statusCaptor.capture(), Mockito.isA(Metadata.class));
    assertEquals(Status.Code.INTERNAL, statusCaptor.getValue().getCode());
  }

  @Test
  public void streamListener_messageRead_onlyOnce() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);
    streamListener.messageRead(method.streamRequest(1234L));
    // canceling the call should short circuit future halfClosed() calls.
    streamListener.closed(Status.CANCELLED);

    streamListener.messageRead(method.streamRequest(1234L));

    verify(callListener).onMessage(1234L);
  }

  @Test
  public void streamListener_unexpectedRuntimeException() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);
    doThrow(new RuntimeException("unexpected exception"))
        .when(callListener)
        .onMessage(any(Long.class));

    InputStream inputStream = method.streamRequest(1234L);

    thrown.expect(RuntimeException.class);
    thrown.expectMessage("unexpected exception");
    streamListener.messageRead(inputStream);
  }

  private static class LongMarshaller implements Marshaller<Long> {
    @Override
    public InputStream stream(Long value) {
      return new ByteArrayInputStream(value.toString().getBytes(UTF_8));
    }

    @Override
    public Long parse(InputStream stream) {
      try {
        return Long.parseLong(CharStreams.toString(new InputStreamReader(stream, UTF_8)));
      } catch (Exception e) {
        throw new RuntimeException(e);
      }
    }
  }
}
