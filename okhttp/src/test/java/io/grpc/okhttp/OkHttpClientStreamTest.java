/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.okhttp;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.internal.ClientStreamListener.RpcProgress.PROCESSED;
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
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.NoopClientStreamListener;
import io.grpc.internal.StatsTraceContext;
import io.grpc.internal.TransportTracer;
import io.grpc.okhttp.internal.framed.ErrorCode;
import io.grpc.okhttp.internal.framed.Header;
import java.io.ByteArrayInputStream;
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
  private final TransportTracer transportTracer = new TransportTracer();

  private MethodDescriptor<?, ?> methodDescriptor;
  private OkHttpClientStream stream;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    methodDescriptor = MethodDescriptor.<Void, Void>newBuilder()
        .setType(MethodDescriptor.MethodType.UNARY)
        .setFullMethodName("testService/test")
        .setRequestMarshaller(marshaller)
        .setResponseMarshaller(marshaller)
        .build();

    stream = new OkHttpClientStream(
        methodDescriptor,
        new Metadata(),
        frameWriter,
        transport,
        flowController,
        lock,
        MAX_MESSAGE_SIZE,
        "localhost",
        "userAgent",
        StatsTraceContext.NOOP,
        transportTracer);
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
      public void closed(
          Status status, RpcProgress rpcProgress, Metadata trailers) {
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
    }).when(transport).finishStream(
        1234, Status.CANCELLED, PROCESSED, true, ErrorCode.CANCEL, null);

    stream.cancel(Status.CANCELLED);

    verify(transport).finishStream(1234, Status.CANCELLED, PROCESSED,true, ErrorCode.CANCEL, null);
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
        StatsTraceContext.NOOP, transportTracer);
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
        StatsTraceContext.NOOP, transportTracer);
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
        .setFullMethodName("service/method")
        .setIdempotent(true)
        .setSafe(true)
        .setRequestMarshaller(marshaller)
        .setResponseMarshaller(marshaller)
        .build();
    stream = new OkHttpClientStream(getMethod, new Metadata(), frameWriter, transport,
        flowController, lock, MAX_MESSAGE_SIZE, "localhost", "good-application",
        StatsTraceContext.NOOP, transportTracer);
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

    verify(frameWriter)
        .synStream(eq(true), eq(false), eq(3), eq(0), headersCaptor.capture());
    assertThat(headersCaptor.getValue()).contains(Headers.METHOD_GET_HEADER);
    assertThat(headersCaptor.getValue()).contains(
        new Header(Header.TARGET_PATH, "/" + getMethod.getFullMethodName() + "?"
            + BaseEncoding.base64().encode(msg)));
  }

  // TODO(carl-mastrangelo): extract this out into a testing/ directory and remove other definitions
  // of it.
  private static class BaseClientStreamListener extends NoopClientStreamListener {

    @Override
    public void messagesAvailable(MessageProducer producer) {
      while (producer.next() != null) {}
    }
  }
}

