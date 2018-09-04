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

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.io.CharStreams;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.DecompressorRegistry;
import io.grpc.InternalChannelz.ServerStats;
import io.grpc.InternalChannelz.ServerStats.Builder;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCall;
import io.grpc.Status;
import io.grpc.internal.ServerCallImpl.ServerStreamListenerImpl;
import io.grpc.internal.testing.SingleMessageProducer;
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
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

@RunWith(JUnit4.class)
public class ServerCallImplTest {
  @Rule public final ExpectedException thrown = ExpectedException.none();
  @Mock private ServerStream stream;
  @Mock private ServerCall.Listener<Long> callListener;

  private final CallTracer serverCallTracer = CallTracer.getDefaultFactory().create();
  private ServerCallImpl<Long, Long> call;
  private Context.CancellableContext context;

  private static final MethodDescriptor<Long, Long> UNARY_METHOD =
      MethodDescriptor.<Long, Long>newBuilder()
          .setType(MethodType.UNARY)
          .setFullMethodName("service/method")
          .setRequestMarshaller(new LongMarshaller())
          .setResponseMarshaller(new LongMarshaller())
          .build();

  private static final MethodDescriptor<Long, Long> CLIENT_STREAMING_METHOD =
      MethodDescriptor.<Long, Long>newBuilder()
          .setType(MethodType.UNARY)
          .setFullMethodName("service/method")
          .setRequestMarshaller(new LongMarshaller())
          .setResponseMarshaller(new LongMarshaller())
          .build();

  private final Metadata requestHeaders = new Metadata();

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    context = Context.ROOT.withCancellation();
    call = new ServerCallImpl<Long, Long>(stream, UNARY_METHOD, requestHeaders, context,
        DecompressorRegistry.getDefaultInstance(), CompressorRegistry.getDefaultInstance(),
        serverCallTracer);
  }

  @Test
  public void callTracer_success() {
    callTracer0(Status.OK);
  }

  @Test
  public void callTracer_failure() {
    callTracer0(Status.UNKNOWN);
  }

  private void callTracer0(Status status) {
    CallTracer tracer = CallTracer.getDefaultFactory().create();
    Builder beforeBuilder = new Builder();
    tracer.updateBuilder(beforeBuilder);
    ServerStats before = beforeBuilder.build();
    assertEquals(0, before.callsStarted);
    assertEquals(0, before.lastCallStartedNanos);

    call = new ServerCallImpl<Long, Long>(stream, UNARY_METHOD, requestHeaders, context,
        DecompressorRegistry.getDefaultInstance(), CompressorRegistry.getDefaultInstance(),
        tracer);

    // required boilerplate
    call.sendHeaders(new Metadata());
    call.sendMessage(123L);
    // end: required boilerplate

    call.close(status, new Metadata());
    Builder afterBuilder = new Builder();
    tracer.updateBuilder(afterBuilder);
    ServerStats after = afterBuilder.build();
    assertEquals(1, after.callsStarted);
    if (status.isOk()) {
      assertEquals(1, after.callsSucceeded);
    } else {
      assertEquals(1, after.callsFailed);
    }
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

    call.sendMessage(1234L);

    verify(stream).close(isA(Status.class), isA(Metadata.class));
  }

  @Test
  public void sendMessage_serverSendsOne_closeOnSecondCall_unary() {
    sendMessage_serverSendsOne_closeOnSecondCall(UNARY_METHOD);
  }

  @Test
  public void sendMessage_serverSendsOne_closeOnSecondCall_clientStreaming() {
    sendMessage_serverSendsOne_closeOnSecondCall(CLIENT_STREAMING_METHOD);
  }

  private void sendMessage_serverSendsOne_closeOnSecondCall(
      MethodDescriptor<Long, Long> method) {
    ServerCallImpl<Long, Long> serverCall = new ServerCallImpl<Long, Long>(
        stream,
        method,
        requestHeaders,
        context,
        DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(),
        serverCallTracer);
    serverCall.sendHeaders(new Metadata());
    serverCall.sendMessage(1L);
    verify(stream, times(1)).writeMessage(any(InputStream.class));
    verify(stream, never()).close(any(Status.class), any(Metadata.class));

    // trying to send a second message causes gRPC to close the underlying stream
    serverCall.sendMessage(1L);
    verify(stream, times(1)).writeMessage(any(InputStream.class));
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(stream, times(1)).cancel(statusCaptor.capture());
    assertEquals(Status.Code.INTERNAL, statusCaptor.getValue().getCode());
    assertEquals(ServerCallImpl.TOO_MANY_RESPONSES, statusCaptor.getValue().getDescription());
  }

  @Test
  public void sendMessage_serverSendsOne_closeOnSecondCall_appRunToCompletion_unary() {
    sendMessage_serverSendsOne_closeOnSecondCall_appRunToCompletion(UNARY_METHOD);
  }

  @Test
  public void sendMessage_serverSendsOne_closeOnSecondCall_appRunToCompletion_clientStreaming() {
    sendMessage_serverSendsOne_closeOnSecondCall_appRunToCompletion(CLIENT_STREAMING_METHOD);
  }

  private void sendMessage_serverSendsOne_closeOnSecondCall_appRunToCompletion(
      MethodDescriptor<Long, Long> method) {
    ServerCallImpl<Long, Long> serverCall = new ServerCallImpl<Long, Long>(
        stream,
        method,
        requestHeaders,
        context,
        DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(),
        serverCallTracer);
    serverCall.sendHeaders(new Metadata());
    serverCall.sendMessage(1L);
    serverCall.sendMessage(1L);
    verify(stream, times(1)).writeMessage(any(InputStream.class));
    verify(stream, times(1)).cancel(any(Status.class));

    // App runs to completion but everything is ignored
    serverCall.sendMessage(1L);
    serverCall.close(Status.OK, new Metadata());
    try {
      serverCall.close(Status.OK, new Metadata());
      fail("calling a second time should still cause an error");
    } catch (IllegalStateException expected) {
      // noop
    }
  }

  @Test
  public void serverSendsOne_okFailsOnMissingResponse_unary() {
    serverSendsOne_okFailsOnMissingResponse(UNARY_METHOD);
  }

  @Test
  public void serverSendsOne_okFailsOnMissingResponse_clientStreaming() {
    serverSendsOne_okFailsOnMissingResponse(CLIENT_STREAMING_METHOD);
  }

  private void serverSendsOne_okFailsOnMissingResponse(
      MethodDescriptor<Long, Long> method) {
    ServerCallImpl<Long, Long> serverCall = new ServerCallImpl<Long, Long>(
        stream,
        method,
        requestHeaders,
        context,
        DecompressorRegistry.getDefaultInstance(),
        CompressorRegistry.getDefaultInstance(),
        serverCallTracer);
    serverCall.close(Status.OK, new Metadata());
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    verify(stream, times(1)).cancel(statusCaptor.capture());
    assertEquals(Status.Code.INTERNAL, statusCaptor.getValue().getCode());
    assertEquals(ServerCallImpl.MISSING_RESPONSE, statusCaptor.getValue().getDescription());
  }

  @Test
  public void serverSendsOne_canErrorWithoutResponse() {
    final String description = "test description";
    final Status status = Status.RESOURCE_EXHAUSTED.withDescription(description);
    final Metadata metadata = new Metadata();
    call.close(status, metadata);
    verify(stream, times(1)).close(same(status), same(metadata));
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
    streamListener.messagesAvailable(new SingleMessageProducer(UNARY_METHOD.streamRequest(1234L)));

    verify(callListener).onMessage(1234L);
  }

  @Test
  public void streamListener_messageRead_onlyOnce() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);
    streamListener.messagesAvailable(new SingleMessageProducer(UNARY_METHOD.streamRequest(1234L)));
    // canceling the call should short circuit future halfClosed() calls.
    streamListener.closed(Status.CANCELLED);

    streamListener.messagesAvailable(new SingleMessageProducer(UNARY_METHOD.streamRequest(1234L)));

    verify(callListener).onMessage(1234L);
  }

  @Test
  public void streamListener_unexpectedRuntimeException() {
    ServerStreamListenerImpl<Long> streamListener =
        new ServerCallImpl.ServerStreamListenerImpl<Long>(call, callListener, context);
    doThrow(new RuntimeException("unexpected exception"))
        .when(callListener)
        .onMessage(any(Long.class));

    InputStream inputStream = UNARY_METHOD.streamRequest(1234L);

    thrown.expect(RuntimeException.class);
    thrown.expectMessage("unexpected exception");
    streamListener.messagesAvailable(new SingleMessageProducer(inputStream));
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
