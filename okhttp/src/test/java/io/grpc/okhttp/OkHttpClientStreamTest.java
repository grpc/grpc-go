/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.okhttp;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isA;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.io.BaseEncoding;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Status;
import io.grpc.internal.ClientStreamListener;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.StatsTraceContext;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.Header;
import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.nio.charset.Charset;
import java.util.List;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

@RunWith(JUnit4.class)
public class OkHttpClientStreamTest {
  private static final int MAX_MESSAGE_SIZE = 100;

  @Mock private MethodDescriptor.Marshaller<Void> marshaller;
  @Mock private AsyncFrameWriter frameWriter;
  @Mock private OkHttpClientTransport transport;
  @Mock private OutboundFlowController flowController;
  @Captor private ArgumentCaptor<List<Header>> headersCaptor;

  private final Object lock = new Object();

  private MethodDescriptor<?, ?> methodDescriptor;
  private OkHttpClientStream stream;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    methodDescriptor = MethodDescriptor.<Void, Void>newBuilder()
        .setType(MethodDescriptor.MethodType.UNARY)
        .setFullMethodName("/testService/test")
        .setRequestMarshaller(marshaller)
        .setResponseMarshaller(marshaller)
        .build();

    stream = new OkHttpClientStream(methodDescriptor, new Metadata(), frameWriter, transport,
        flowController, lock, MAX_MESSAGE_SIZE, "localhost", "userAgent", StatsTraceContext.NOOP);
  }

  @Test
  public void getType() {
    assertEquals(MethodType.UNARY, stream.getType());
  }

  @Test
  public void cancel_notStarted() {
    final AtomicReference<Status> statusRef = new AtomicReference<Status>();
    stream.start(new BaseClientStreamListener() {
      @Override
      public void closed(Status status, Metadata trailers) {
        statusRef.set(status);
        assertTrue(Thread.holdsLock(lock));
      }
    });

    stream.cancel(Status.CANCELLED);

    assertEquals(Status.Code.CANCELLED, statusRef.get().getCode());
  }

  @Test
  public void cancel_started() {
    stream.start(new BaseClientStreamListener());
    stream.transportState().start(1234);
    Mockito.doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Throwable {
        assertTrue(Thread.holdsLock(lock));
        return null;
      }
    }).when(transport).finishStream(1234, Status.CANCELLED, ErrorCode.CANCEL, null);

    stream.cancel(Status.CANCELLED);

    verify(transport).finishStream(1234, Status.CANCELLED, ErrorCode.CANCEL, null);
  }

  @Test
  public void start_alreadyCancelled() {
    stream.start(new BaseClientStreamListener());
    stream.cancel(Status.CANCELLED);

    stream.transportState().start(1234);

    verifyNoMoreInteractions(frameWriter);
  }

  @Test
  public void start_userAgentRemoved() {
    Metadata metaData = new Metadata();
    metaData.put(GrpcUtil.USER_AGENT_KEY, "misbehaving-application");
    stream = new OkHttpClientStream(methodDescriptor, metaData, frameWriter, transport,
        flowController, lock, MAX_MESSAGE_SIZE, "localhost", "good-application",
        StatsTraceContext.NOOP);
    stream.start(new BaseClientStreamListener());
    stream.transportState().start(3);

    verify(frameWriter).synStream(eq(false), eq(false), eq(3), eq(0), headersCaptor.capture());
    assertThat(headersCaptor.getValue())
        .contains(new Header(GrpcUtil.USER_AGENT_KEY.name(), "good-application"));
  }

  @Test
  public void start_headerFieldOrder() {
    Metadata metaData = new Metadata();
    metaData.put(GrpcUtil.USER_AGENT_KEY, "misbehaving-application");
    stream = new OkHttpClientStream(methodDescriptor, metaData, frameWriter, transport,
        flowController, lock, MAX_MESSAGE_SIZE, "localhost", "good-application",
        StatsTraceContext.NOOP);
    stream.start(new BaseClientStreamListener());
    stream.transportState().start(3);

    verify(frameWriter).synStream(eq(false), eq(false), eq(3), eq(0), headersCaptor.capture());
    assertThat(headersCaptor.getValue()).containsExactly(
        Headers.SCHEME_HEADER,
        Headers.METHOD_HEADER,
        new Header(Header.TARGET_AUTHORITY, "localhost"),
        new Header(Header.TARGET_PATH, "/" + methodDescriptor.getFullMethodName()),
        new Header(GrpcUtil.USER_AGENT_KEY.name(), "good-application"),
        Headers.CONTENT_TYPE_HEADER,
        Headers.TE_HEADER)
            .inOrder();
  }

  @Test
  public void getUnaryRequest() {
    MethodDescriptor<?, ?> getMethod = MethodDescriptor.<Void, Void>newBuilder()
        .setType(MethodDescriptor.MethodType.UNARY)
        .setFullMethodName("/service/method")
        .setIdempotent(true)
        .setSafe(true)
        .setRequestMarshaller(marshaller)
        .setResponseMarshaller(marshaller)
        .build();
    stream = new OkHttpClientStream(getMethod, new Metadata(), frameWriter, transport,
        flowController, lock, MAX_MESSAGE_SIZE, "localhost", "good-application",
        StatsTraceContext.NOOP);
    stream.start(new BaseClientStreamListener());

    // GET streams send headers after halfClose is called.
    verify(frameWriter, times(0)).synStream(
        eq(false), eq(false), eq(3), eq(0), headersCaptor.capture());
    verify(transport, times(0)).streamReadyToStart(isA(OkHttpClientStream.class));

    byte[] msg = "request".getBytes(Charset.forName("UTF-8"));
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.halfClose();
    verify(transport).streamReadyToStart(eq(stream));
    stream.transportState().start(3);

    verify(frameWriter).synStream(eq(false), eq(false), eq(3), eq(0), headersCaptor.capture());
    assertThat(headersCaptor.getValue()).contains(
        new Header(Header.TARGET_PATH, "/" + getMethod.getFullMethodName() + "?"
            + BaseEncoding.base64().encode(msg)));
  }

  // TODO(carl-mastrangelo): extract this out into a testing/ directory and remove other definitions
  // of it.
  private static class BaseClientStreamListener implements ClientStreamListener {
    @Override
    public void onReady() {}

    @Override
    public void messageRead(InputStream message) {}

    @Override
    public void headersRead(Metadata headers) {}

    @Override
    public void closed(Status status, Metadata trailers) {}
  }
}

