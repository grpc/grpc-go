/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc.testing.integration;

import static io.grpc.testing.integration.Messages.PayloadType.COMPRESSABLE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.auth.oauth2.AccessToken;
import com.google.auth.oauth2.ComputeEngineCredentials;
import com.google.auth.oauth2.GoogleCredentials;
import com.google.auth.oauth2.OAuth2Credentials;
import com.google.auth.oauth2.ServiceAccountCredentials;
import com.google.auth.oauth2.ServiceAccountJwtAccessCredentials;
import com.google.protobuf.ByteString;
import com.google.protobuf.EmptyProtos.Empty;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.Server;
import io.grpc.ServerInterceptors;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.auth.ClientAuthInterceptor;
import io.grpc.internal.AbstractServerImplBuilder;
import io.grpc.internal.GrpcUtil;
import io.grpc.protobuf.ProtoUtils;
import io.grpc.stub.MetadataUtils;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.StreamRecorder;
import io.grpc.testing.TestUtils;
import io.grpc.testing.integration.Messages.Payload;
import io.grpc.testing.integration.Messages.PayloadType;
import io.grpc.testing.integration.Messages.ResponseParameters;
import io.grpc.testing.integration.Messages.SimpleRequest;
import io.grpc.testing.integration.Messages.SimpleResponse;
import io.grpc.testing.integration.Messages.StreamingInputCallRequest;
import io.grpc.testing.integration.Messages.StreamingInputCallResponse;
import io.grpc.testing.integration.Messages.StreamingOutputCallRequest;
import io.grpc.testing.integration.Messages.StreamingOutputCallResponse;

import org.junit.After;
import org.junit.Assert;
import org.junit.Before;
import org.junit.Test;
import org.mockito.ArgumentCaptor;

import java.io.IOException;
import java.io.InputStream;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Abstract base class for all GRPC transport tests.
 */
public abstract class AbstractTransportTest {

  public static final Metadata.Key<Messages.SimpleContext> METADATA_KEY =
      ProtoUtils.keyForProto(Messages.SimpleContext.getDefaultInstance());
  private static final AtomicReference<Metadata> requestHeadersCapture =
      new AtomicReference<Metadata>();
  private static ScheduledExecutorService testServiceExecutor;
  private static Server server;
  private static int OPERATION_TIMEOUT = 5000;

  protected static void startStaticServer(AbstractServerImplBuilder<?> builder) {
    testServiceExecutor = Executors.newScheduledThreadPool(2);

    builder.addService(ServerInterceptors.intercept(
        TestServiceGrpc.bindService(new TestServiceImpl(testServiceExecutor)),
        TestUtils.recordRequestHeadersInterceptor(requestHeadersCapture),
        TestUtils.echoRequestHeadersInterceptor(Util.METADATA_KEY)));
    try {
      server = builder.build().start();
    } catch (IOException ex) {
      throw new RuntimeException(ex);
    }
  }

  protected static void stopStaticServer() {
    server.shutdownNow();
    testServiceExecutor.shutdown();
  }

  protected ManagedChannel channel;
  protected TestServiceGrpc.TestServiceBlockingStub blockingStub;
  protected TestServiceGrpc.TestService asyncStub;

  /**
   * Must be called by the subclass setup method if overridden.
   */
  @Before
  public void setUp() {
    channel = createChannel();
    blockingStub = TestServiceGrpc.newBlockingStub(channel);
    asyncStub = TestServiceGrpc.newStub(channel);
    requestHeadersCapture.set(null);
  }

  /** Clean up. */
  @After
  public void tearDown() throws Exception {
    if (channel != null) {
      channel.shutdown();
    }
  }

  protected abstract ManagedChannel createChannel();

  @Test(timeout = 10000)
  public void emptyUnary() throws Exception {
    assertEquals(Empty.getDefaultInstance(), blockingStub.emptyCall(Empty.getDefaultInstance()));
  }

  @Test(timeout = 10000)
  public void largeUnary() throws Exception {
    final SimpleRequest request = SimpleRequest.newBuilder()
        .setResponseSize(314159)
        .setResponseType(PayloadType.COMPRESSABLE)
        .setPayload(Payload.newBuilder()
            .setBody(ByteString.copyFrom(new byte[271828])))
        .build();
    final SimpleResponse goldenResponse = SimpleResponse.newBuilder()
        .setPayload(Payload.newBuilder()
            .setType(PayloadType.COMPRESSABLE)
            .setBody(ByteString.copyFrom(new byte[314159])))
        .build();

    assertEquals(goldenResponse, blockingStub.unaryCall(request));
  }

  @Test(timeout = 10000)
  public void serverStreaming() throws Exception {
    final StreamingOutputCallRequest request = StreamingOutputCallRequest.newBuilder()
        .setResponseType(PayloadType.COMPRESSABLE)
        .addResponseParameters(ResponseParameters.newBuilder()
            .setSize(31415))
        .addResponseParameters(ResponseParameters.newBuilder()
            .setSize(9))
        .addResponseParameters(ResponseParameters.newBuilder()
            .setSize(2653))
        .addResponseParameters(ResponseParameters.newBuilder()
            .setSize(58979))
        .build();
    final List<StreamingOutputCallResponse> goldenResponses = Arrays.asList(
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[31415])))
            .build(),
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[9])))
            .build(),
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[2653])))
            .build(),
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[58979])))
            .build());

    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    asyncStub.streamingOutputCall(request, recorder);
    recorder.awaitCompletion();
    assertSuccess(recorder);
    assertEquals(goldenResponses, recorder.getValues());
  }

  @Test(timeout = 10000)
  public void clientStreaming() throws Exception {
    final List<StreamingInputCallRequest> requests = Arrays.asList(
        StreamingInputCallRequest.newBuilder()
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[27182])))
            .build(),
        StreamingInputCallRequest.newBuilder()
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[8])))
            .build(),
        StreamingInputCallRequest.newBuilder()
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[1828])))
            .build(),
        StreamingInputCallRequest.newBuilder()
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[45904])))
            .build());
    final StreamingInputCallResponse goldenResponse = StreamingInputCallResponse.newBuilder()
        .setAggregatedPayloadSize(74922)
        .build();

    StreamRecorder<StreamingInputCallResponse> responseObserver = StreamRecorder.create();
    StreamObserver<StreamingInputCallRequest> requestObserver =
        asyncStub.streamingInputCall(responseObserver);
    for (StreamingInputCallRequest request : requests) {
      requestObserver.onNext(request);
    }
    requestObserver.onCompleted();
    assertEquals(goldenResponse, responseObserver.firstValue().get());
  }

  @Test(timeout = 10000)
  public void pingPong() throws Exception {
    final List<StreamingOutputCallRequest> requests = Arrays.asList(
        StreamingOutputCallRequest.newBuilder()
            .addResponseParameters(ResponseParameters.newBuilder()
                .setSize(31415))
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[27182])))
            .build(),
        StreamingOutputCallRequest.newBuilder()
            .addResponseParameters(ResponseParameters.newBuilder()
                .setSize(9))
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[8])))
            .build(),
        StreamingOutputCallRequest.newBuilder()
            .addResponseParameters(ResponseParameters.newBuilder()
                .setSize(2653))
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[1828])))
            .build(),
        StreamingOutputCallRequest.newBuilder()
            .addResponseParameters(ResponseParameters.newBuilder()
                .setSize(58979))
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[45904])))
            .build());
    final List<StreamingOutputCallResponse> goldenResponses = Arrays.asList(
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[31415])))
            .build(),
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[9])))
            .build(),
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[2653])))
            .build(),
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[58979])))
            .build());

    @SuppressWarnings("unchecked")
    StreamObserver<StreamingOutputCallResponse> responseObserver = mock(StreamObserver.class);
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    for (int i = 0; i < requests.size(); i++) {
      requestObserver.onNext(requests.get(i));
      verify(responseObserver, timeout(OPERATION_TIMEOUT)).onNext(goldenResponses.get(i));
      verifyNoMoreInteractions(responseObserver);
    }
    requestObserver.onCompleted();
    verify(responseObserver, timeout(OPERATION_TIMEOUT)).onCompleted();
    verifyNoMoreInteractions(responseObserver);
  }

  @Test(timeout = 10000)
  public void emptyStream() throws Exception {
    @SuppressWarnings("unchecked")
    StreamObserver<StreamingOutputCallResponse> responseObserver = mock(StreamObserver.class);
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    requestObserver.onCompleted();
    verify(responseObserver, timeout(OPERATION_TIMEOUT)).onCompleted();
    verifyNoMoreInteractions(responseObserver);
  }

  @Test(timeout = 10000)
  public void cancelAfterBegin() throws Exception {
    StreamRecorder<StreamingInputCallResponse> responseObserver = StreamRecorder.create();
    StreamObserver<StreamingInputCallRequest> requestObserver =
        asyncStub.streamingInputCall(responseObserver);
    requestObserver.onError(new RuntimeException());
    responseObserver.awaitCompletion();
    assertEquals(Arrays.<StreamingInputCallResponse>asList(), responseObserver.getValues());
    assertEquals(Status.CANCELLED, Status.fromThrowable(responseObserver.getError()));
  }

  @Test(timeout = 10000)
  public void cancelAfterFirstResponse() throws Exception {
    final StreamingOutputCallRequest request = StreamingOutputCallRequest.newBuilder()
        .addResponseParameters(ResponseParameters.newBuilder()
            .setSize(31415))
        .setPayload(Payload.newBuilder()
            .setBody(ByteString.copyFrom(new byte[27182])))
        .build();
    final StreamingOutputCallResponse goldenResponse = StreamingOutputCallResponse.newBuilder()
        .setPayload(Payload.newBuilder()
            .setType(PayloadType.COMPRESSABLE)
            .setBody(ByteString.copyFrom(new byte[31415])))
        .build();

    @SuppressWarnings("unchecked")
    StreamObserver<StreamingOutputCallResponse> responseObserver = mock(StreamObserver.class);
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    requestObserver.onNext(request);
    verify(responseObserver, timeout(OPERATION_TIMEOUT)).onNext(goldenResponse);
    verifyNoMoreInteractions(responseObserver);

    requestObserver.onError(new RuntimeException());
    ArgumentCaptor<Throwable> captor = ArgumentCaptor.forClass(Throwable.class);
    verify(responseObserver, timeout(OPERATION_TIMEOUT)).onError(captor.capture());
    assertEquals(Status.CANCELLED, Status.fromThrowable(captor.getValue()));
    verifyNoMoreInteractions(responseObserver);
  }

  @Test(timeout = 10000)
  public void fullDuplexCallShouldSucceed() throws Exception {
    // Build the request.
    List<Integer> responseSizes = Arrays.asList(50, 100, 150, 200);
    StreamingOutputCallRequest.Builder streamingOutputBuilder =
        StreamingOutputCallRequest.newBuilder();
    streamingOutputBuilder.setResponseType(COMPRESSABLE);
    for (Integer size : responseSizes) {
      streamingOutputBuilder.addResponseParametersBuilder().setSize(size).setIntervalUs(0);
    }
    final StreamingOutputCallRequest request = streamingOutputBuilder.build();

    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<StreamingOutputCallRequest> requestStream =
        asyncStub.fullDuplexCall(recorder);

    final int numRequests = 10;
    for (int ix = numRequests; ix > 0; --ix) {
      requestStream.onNext(request);
    }
    requestStream.onCompleted();
    recorder.awaitCompletion();
    assertSuccess(recorder);
    assertEquals(responseSizes.size() * numRequests, recorder.getValues().size());
    for (int ix = 0; ix < recorder.getValues().size(); ++ix) {
      StreamingOutputCallResponse response = recorder.getValues().get(ix);
      assertEquals(COMPRESSABLE, response.getPayload().getType());
      int length = response.getPayload().getBody().size();
      int expectedSize = responseSizes.get(ix % responseSizes.size());
      assertEquals("comparison failed at index " + ix, expectedSize, length);
    }
  }

  @Test(timeout = 10000)
  public void halfDuplexCallShouldSucceed() throws Exception {
    // Build the request.
    List<Integer> responseSizes = Arrays.asList(50, 100, 150, 200);
    StreamingOutputCallRequest.Builder streamingOutputBuilder =
        StreamingOutputCallRequest.newBuilder();
    streamingOutputBuilder.setResponseType(COMPRESSABLE);
    for (Integer size : responseSizes) {
      streamingOutputBuilder.addResponseParametersBuilder().setSize(size).setIntervalUs(0);
    }
    final StreamingOutputCallRequest request = streamingOutputBuilder.build();

    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<StreamingOutputCallRequest> requestStream = asyncStub.halfDuplexCall(recorder);

    final int numRequests = 10;
    for (int ix = numRequests; ix > 0; --ix) {
      requestStream.onNext(request);
    }
    requestStream.onCompleted();
    recorder.awaitCompletion();
    assertSuccess(recorder);
    assertEquals(responseSizes.size() * numRequests, recorder.getValues().size());
    for (int ix = 0; ix < recorder.getValues().size(); ++ix) {
      StreamingOutputCallResponse response = recorder.getValues().get(ix);
      assertEquals(COMPRESSABLE, response.getPayload().getType());
      int length = response.getPayload().getBody().size();
      int expectedSize = responseSizes.get(ix % responseSizes.size());
      assertEquals("comparison failed at index " + ix, expectedSize, length);
    }
  }

  @Test(timeout = 10000)
  public void serverStreamingShouldBeFlowControlled() throws Exception {
    final StreamingOutputCallRequest request = StreamingOutputCallRequest.newBuilder()
        .setResponseType(COMPRESSABLE)
        .addResponseParameters(ResponseParameters.newBuilder().setSize(100000))
        .addResponseParameters(ResponseParameters.newBuilder().setSize(100001))
        .build();
    final List<StreamingOutputCallResponse> goldenResponses = Arrays.asList(
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[100000]))).build(),
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[100001]))).build());

    long start = System.nanoTime();

    final ArrayBlockingQueue<Object> queue = new ArrayBlockingQueue<Object>(10);
    ClientCall<StreamingOutputCallRequest, StreamingOutputCallResponse> call =
        channel.newCall(TestServiceGrpc.METHOD_STREAMING_OUTPUT_CALL, CallOptions.DEFAULT);
    call.start(new ClientCall.Listener<StreamingOutputCallResponse>() {
      @Override
      public void onHeaders(Metadata headers) {}

      @Override
      public void onMessage(final StreamingOutputCallResponse message) {
        queue.add(message);
      }

      @Override
      public void onClose(Status status, Metadata trailers) {
        queue.add(status);
      }
    }, new Metadata());
    call.sendMessage(request);
    call.halfClose();

    // Time how long it takes to get the first response.
    call.request(1);
    assertEquals(goldenResponses.get(0), queue.poll(OPERATION_TIMEOUT, TimeUnit.MILLISECONDS));
    long firstCallDuration = System.nanoTime() - start;

    // Without giving additional flow control, make sure that we don't get another response. We wait
    // until we are comfortable the next message isn't coming. We may have very low nanoTime
    // resolution (like on Windows) or be using a testing, in-process transport where message
    // handling is instantaneous. In both cases, firstCallDuration may be 0, so round up sleep time
    // to at least 1ms.
    assertNull(queue.poll(Math.max(firstCallDuration * 4, 1 * 1000 * 1000), TimeUnit.NANOSECONDS));

    // Make sure that everything still completes.
    call.request(1);
    assertEquals(goldenResponses.get(1), queue.poll(OPERATION_TIMEOUT, TimeUnit.MILLISECONDS));
    assertEquals(Status.OK, queue.poll(OPERATION_TIMEOUT, TimeUnit.MILLISECONDS));
  }

  @Test(timeout = 30000)
  public void veryLargeRequest() throws Exception {
    final SimpleRequest request = SimpleRequest.newBuilder()
        .setPayload(Payload.newBuilder()
            .setType(PayloadType.COMPRESSABLE)
            .setBody(ByteString.copyFrom(new byte[unaryPayloadLength()])))
        .setResponseSize(10)
        .setResponseType(PayloadType.COMPRESSABLE)
        .build();
    final SimpleResponse goldenResponse = SimpleResponse.newBuilder()
        .setPayload(Payload.newBuilder()
            .setType(PayloadType.COMPRESSABLE)
            .setBody(ByteString.copyFrom(new byte[10])))
        .build();

    assertEquals(goldenResponse, blockingStub.unaryCall(request));
  }

  @Test(timeout = 30000)
  public void veryLargeResponse() throws Exception {
    final SimpleRequest request = SimpleRequest.newBuilder()
        .setResponseSize(unaryPayloadLength())
        .setResponseType(PayloadType.COMPRESSABLE)
        .build();
    final SimpleResponse goldenResponse = SimpleResponse.newBuilder()
        .setPayload(Payload.newBuilder()
            .setType(PayloadType.COMPRESSABLE)
            .setBody(ByteString.copyFrom(new byte[unaryPayloadLength()])))
        .build();

    assertEquals(goldenResponse, blockingStub.unaryCall(request));
  }

  @Test(timeout = 10000)
  public void exchangeContextUnaryCall() throws Exception {
    TestServiceGrpc.TestServiceBlockingStub stub =
        TestServiceGrpc.newBlockingStub(channel);

    // Capture the context exchange
    Metadata fixedHeaders = new Metadata();
    // Send a context proto (as it's in the default extension registry)
    Messages.SimpleContext contextValue =
        Messages.SimpleContext.newBuilder().setValue("dog").build();
    fixedHeaders.put(METADATA_KEY, contextValue);
    stub = MetadataUtils.attachHeaders(stub, fixedHeaders);
    // .. and expect it to be echoed back in trailers
    AtomicReference<Metadata> trailersCapture = new AtomicReference<Metadata>();
    AtomicReference<Metadata> headersCapture = new AtomicReference<Metadata>();
    stub = MetadataUtils.captureMetadata(stub, headersCapture, trailersCapture);

    Assert.assertNotNull(stub.emptyCall(Empty.getDefaultInstance()));

    // Assert that our side channel object is echoed back in both headers and trailers
    Assert.assertEquals(contextValue, headersCapture.get().get(METADATA_KEY));
    Assert.assertEquals(contextValue, trailersCapture.get().get(METADATA_KEY));
  }

  @Test(timeout = 10000)
  public void exchangeContextStreamingCall() throws Exception {
    TestServiceGrpc.TestServiceStub stub = TestServiceGrpc.newStub(channel);

    // Capture the context exchange
    Metadata fixedHeaders = new Metadata();
    // Send a context proto (as it's in the default extension registry)
    Messages.SimpleContext contextValue =
        Messages.SimpleContext.newBuilder().setValue("dog").build();
    fixedHeaders.put(METADATA_KEY, contextValue);
    stub = MetadataUtils.attachHeaders(stub, fixedHeaders);
    // .. and expect it to be echoed back in trailers
    AtomicReference<Metadata> trailersCapture = new AtomicReference<Metadata>();
    AtomicReference<Metadata> headersCapture = new AtomicReference<Metadata>();
    stub = MetadataUtils.captureMetadata(stub, headersCapture, trailersCapture);

    List<Integer> responseSizes = Arrays.asList(50, 100, 150, 200);
    Messages.StreamingOutputCallRequest.Builder streamingOutputBuilder =
        Messages.StreamingOutputCallRequest.newBuilder();
    streamingOutputBuilder.setResponseType(COMPRESSABLE);
    for (Integer size : responseSizes) {
      streamingOutputBuilder.addResponseParametersBuilder().setSize(size).setIntervalUs(0);
    }
    final Messages.StreamingOutputCallRequest request = streamingOutputBuilder.build();

    StreamRecorder<Messages.StreamingOutputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<Messages.StreamingOutputCallRequest> requestStream =
        stub.fullDuplexCall(recorder);

    final int numRequests = 10;
    for (int ix = numRequests; ix > 0; --ix) {
      requestStream.onNext(request);
    }
    requestStream.onCompleted();
    recorder.awaitCompletion();
    assertSuccess(recorder);
    org.junit.Assert.assertEquals(responseSizes.size() * numRequests, recorder.getValues().size());

    // Assert that our side channel object is echoed back in both headers and trailers
    Assert.assertEquals(contextValue, headersCapture.get().get(METADATA_KEY));
    Assert.assertEquals(contextValue, trailersCapture.get().get(METADATA_KEY));
  }

  @Test(timeout = 10000)
  public void sendsTimeoutHeader() {
    long configuredTimeoutMinutes = 100;
    TestServiceGrpc.TestServiceBlockingStub stub = TestServiceGrpc.newBlockingStub(channel)
        .withDeadlineAfter(configuredTimeoutMinutes, TimeUnit.MINUTES);
    stub.emptyCall(Empty.getDefaultInstance());
    long transferredTimeoutMinutes = TimeUnit.MICROSECONDS.toMinutes(
        requestHeadersCapture.get().get(GrpcUtil.TIMEOUT_KEY));
    Assert.assertTrue(
        "configuredTimeoutMinutes=" + configuredTimeoutMinutes
            + ", transferredTimeoutMinutes=" + transferredTimeoutMinutes,
        configuredTimeoutMinutes - transferredTimeoutMinutes >= 0
            && configuredTimeoutMinutes - transferredTimeoutMinutes <= 1);
  }

  @Test
  public void deadlineNotExceeded() {
    // warm up the channel and JVM
    blockingStub.emptyCall(Empty.getDefaultInstance());
    TestServiceGrpc.newBlockingStub(channel)
        .withDeadlineAfter(10, TimeUnit.SECONDS)
        .streamingOutputCall(StreamingOutputCallRequest.newBuilder()
            .addResponseParameters(ResponseParameters.newBuilder()
                .setIntervalUs(0))
                .build()).next();
  }

  @Test(timeout = 10000)
  public void deadlineExceeded() {
    // warm up the channel and JVM
    blockingStub.emptyCall(Empty.getDefaultInstance());
    TestServiceGrpc.TestServiceBlockingStub stub = TestServiceGrpc.newBlockingStub(channel)
        .withDeadlineAfter(10, TimeUnit.MILLISECONDS);
    try {
      stub.streamingOutputCall(StreamingOutputCallRequest.newBuilder()
           .addResponseParameters(ResponseParameters.newBuilder()
               .setIntervalUs(20000))
               .build()).next();
      fail("Expected deadline to be exceeded");
    } catch (Throwable t) {
      assertEquals(Status.DEADLINE_EXCEEDED, Status.fromThrowable(t));
    }
  }

  @Test(timeout = 10000)
  public void deadlineExceededServerStreaming() throws Exception {
    // warm up the channel and JVM
    blockingStub.emptyCall(Empty.getDefaultInstance());
    ResponseParameters.Builder responseParameters = ResponseParameters.newBuilder()
        .setSize(1)
        .setIntervalUs(10000);
    StreamingOutputCallRequest request = StreamingOutputCallRequest.newBuilder()
        .setResponseType(PayloadType.COMPRESSABLE)
        .addResponseParameters(responseParameters)
        .addResponseParameters(responseParameters)
        .addResponseParameters(responseParameters)
        .addResponseParameters(responseParameters)
        .build();
    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    TestServiceGrpc.newStub(channel)
        .withDeadlineAfter(30, TimeUnit.MILLISECONDS)
        .streamingOutputCall(request, recorder);
    recorder.awaitCompletion();
    assertEquals(Status.DEADLINE_EXCEEDED, Status.fromThrowable(recorder.getError()));
  }

  protected int unaryPayloadLength() {
    // 10MiB.
    return 10485760;
  }

  @Test(timeout = 10000)
  public void gracefulShutdown() throws Exception {
    final List<StreamingOutputCallRequest> requests = Arrays.asList(
        StreamingOutputCallRequest.newBuilder()
            .addResponseParameters(ResponseParameters.newBuilder()
                .setSize(3))
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[2])))
            .build(),
        StreamingOutputCallRequest.newBuilder()
            .addResponseParameters(ResponseParameters.newBuilder()
                .setSize(1))
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[7])))
            .build(),
        StreamingOutputCallRequest.newBuilder()
            .addResponseParameters(ResponseParameters.newBuilder()
                .setSize(4))
            .setPayload(Payload.newBuilder()
                .setBody(ByteString.copyFrom(new byte[1])))
            .build());
    final List<StreamingOutputCallResponse> goldenResponses = Arrays.asList(
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[3])))
            .build(),
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[1])))
            .build(),
        StreamingOutputCallResponse.newBuilder()
            .setPayload(Payload.newBuilder()
                .setType(PayloadType.COMPRESSABLE)
                .setBody(ByteString.copyFrom(new byte[4])))
            .build());

    @SuppressWarnings("unchecked")
    StreamObserver<StreamingOutputCallResponse> responseObserver = mock(StreamObserver.class);
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    requestObserver.onNext(requests.get(0));
    verify(responseObserver, timeout(OPERATION_TIMEOUT)).onNext(goldenResponses.get(0));
    // Initiate graceful shutdown.
    channel.shutdown();
    requestObserver.onNext(requests.get(1));
    verify(responseObserver, timeout(OPERATION_TIMEOUT)).onNext(goldenResponses.get(1));
    // The previous ping-pong could have raced with the shutdown, but this one certainly shouldn't.
    requestObserver.onNext(requests.get(2));
    verify(responseObserver, timeout(OPERATION_TIMEOUT)).onNext(goldenResponses.get(2));
    requestObserver.onCompleted();
    verify(responseObserver, timeout(OPERATION_TIMEOUT)).onCompleted();
    verifyNoMoreInteractions(responseObserver);
  }

  /** Sends an rpc to an unimplemented method on the server. */
  @Test(timeout = 10000)
  public void unimplementedMethod() {
    UnimplementedServiceGrpc.UnimplementedServiceBlockingStub stub =
        UnimplementedServiceGrpc.newBlockingStub(channel);
    try {
      stub.unimplementedCall(Empty.getDefaultInstance());
      fail();
    } catch (StatusRuntimeException e) {
      assertEquals(Status.UNIMPLEMENTED.getCode(), e.getStatus().getCode());
    }
  }

  /** Start a fullDuplexCall which the server will not respond, and verify the deadline expires. */
  @Test(timeout = 10000)
  public void timeoutOnSleepingServer() {
    TestServiceGrpc.TestService stub = TestServiceGrpc.newStub(channel)
        .withDeadlineAfter(1, TimeUnit.MILLISECONDS);
    @SuppressWarnings("unchecked")
    StreamObserver<StreamingOutputCallResponse> responseObserver = mock(StreamObserver.class);
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = stub.fullDuplexCall(responseObserver);

    try {
      requestObserver.onNext(StreamingOutputCallRequest.newBuilder()
          .setPayload(Payload.newBuilder()
              .setBody(ByteString.copyFrom(new byte[27182])))
          .build());
    } catch (IllegalStateException expected) {
      // This can happen if the stream has already been terminated due to deadline exceeded.
    }

    ArgumentCaptor<Throwable> captor = ArgumentCaptor.forClass(Throwable.class);
    verify(responseObserver, timeout(OPERATION_TIMEOUT)).onError(captor.capture());
    assertEquals(Status.DEADLINE_EXCEEDED.getCode(),
        Status.fromThrowable(captor.getValue()).getCode());
    verifyNoMoreInteractions(responseObserver);
  }

  /** Sends a large unary rpc with service account credentials. */
  public void serviceAccountCreds(String jsonKey, InputStream credentialsStream, String authScope)
      throws Exception {
    // cast to ServiceAccountCredentials to double-check the right type of object was created.
    GoogleCredentials credentials =
        ServiceAccountCredentials.class.cast(GoogleCredentials.fromStream(credentialsStream));
    credentials = credentials.createScoped(Arrays.<String>asList(authScope));
    TestServiceGrpc.TestServiceBlockingStub stub = blockingStub
        .withInterceptors(new ClientAuthInterceptor(credentials,
            Executors.newSingleThreadExecutor()));
    final SimpleRequest request = SimpleRequest.newBuilder()
        .setFillUsername(true)
        .setFillOauthScope(true)
        .setResponseSize(314159)
        .setResponseType(PayloadType.COMPRESSABLE)
        .setPayload(Payload.newBuilder()
            .setBody(ByteString.copyFrom(new byte[271828])))
        .build();

    final SimpleResponse response = stub.unaryCall(request);
    assertFalse(response.getUsername().isEmpty());
    assertTrue("Received username: " + response.getUsername(),
        jsonKey.contains(response.getUsername()));
    assertFalse(response.getOauthScope().isEmpty());
    assertTrue("Received oauth scope: " + response.getOauthScope(),
        authScope.contains(response.getOauthScope()));

    final SimpleResponse goldenResponse = SimpleResponse.newBuilder()
        .setOauthScope(response.getOauthScope())
        .setUsername(response.getUsername())
        .setPayload(Payload.newBuilder()
            .setType(PayloadType.COMPRESSABLE)
            .setBody(ByteString.copyFrom(new byte[314159])))
        .build();
    assertEquals(goldenResponse, response);
  }

  /** Sends a large unary rpc with compute engine credentials. */
  public void computeEngineCreds(String serviceAccount, String oauthScope) throws Exception {
    ComputeEngineCredentials credentials = new ComputeEngineCredentials();
    TestServiceGrpc.TestServiceBlockingStub stub = blockingStub
        .withInterceptors(new ClientAuthInterceptor(credentials,
            Executors.newSingleThreadExecutor()));
    final SimpleRequest request = SimpleRequest.newBuilder()
        .setFillUsername(true)
        .setFillOauthScope(true)
        .setResponseSize(314159)
        .setResponseType(PayloadType.COMPRESSABLE)
        .setPayload(Payload.newBuilder()
            .setBody(ByteString.copyFrom(new byte[271828])))
        .build();

    final SimpleResponse response = stub.unaryCall(request);
    assertEquals(serviceAccount, response.getUsername());
    assertFalse(response.getOauthScope().isEmpty());
    assertTrue("Received oauth scope: " + response.getOauthScope(),
        oauthScope.contains(response.getOauthScope()));

    final SimpleResponse goldenResponse = SimpleResponse.newBuilder()
        .setOauthScope(response.getOauthScope())
        .setUsername(response.getUsername())
        .setPayload(Payload.newBuilder()
            .setType(PayloadType.COMPRESSABLE)
            .setBody(ByteString.copyFrom(new byte[314159])))
        .build();
    assertEquals(goldenResponse, response);
  }

  /** Test JWT-based auth. */
  public void jwtTokenCreds(InputStream serviceAccountJson) throws Exception {
    final SimpleRequest request = SimpleRequest.newBuilder()
        .setResponseType(PayloadType.COMPRESSABLE)
        .setResponseSize(314159)
        .setPayload(Payload.newBuilder()
            .setBody(ByteString.copyFrom(new byte[271828])))
        .setFillUsername(true)
        .build();

    ServiceAccountCredentials origCreds = (ServiceAccountCredentials)
        GoogleCredentials.fromStream(serviceAccountJson);
    ServiceAccountJwtAccessCredentials credentials = new ServiceAccountJwtAccessCredentials(
        origCreds.getClientId(), origCreds.getClientEmail(), origCreds.getPrivateKey(),
        origCreds.getPrivateKeyId());
    TestServiceGrpc.TestServiceBlockingStub stub = blockingStub
        .withInterceptors(new ClientAuthInterceptor(credentials,
            Executors.newSingleThreadExecutor()));
    SimpleResponse response = stub.unaryCall(request);
    assertEquals(origCreds.getClientEmail(), response.getUsername());
    assertEquals(314159, response.getPayload().getBody().size());
  }

  /** Sends a unary rpc with raw oauth2 access token credentials. */
  public void oauth2AuthToken(String jsonKey, InputStream credentialsStream, String authScope)
      throws Exception {
    GoogleCredentials utilCredentials =
        GoogleCredentials.fromStream(credentialsStream);
    utilCredentials = utilCredentials.createScoped(Arrays.<String>asList(authScope));
    AccessToken accessToken = utilCredentials.refreshAccessToken();

    // TODO(madongfly): The Auth library may have something like AccessTokenCredentials in the
    // future, change to the official implementation then.
    OAuth2Credentials credentials = new OAuth2Credentials(accessToken) {
      @Override
      public AccessToken refreshAccessToken() throws IOException {
        throw new IOException("This credential is based on a certain AccessToken, "
            + "so you can not refresh AccessToken");
      }
    };

    TestServiceGrpc.TestServiceBlockingStub stub = blockingStub
        .withInterceptors(new ClientAuthInterceptor(credentials,
            Executors.newSingleThreadExecutor()));
    final SimpleRequest request = SimpleRequest.newBuilder()
        .setFillUsername(true)
        .setFillOauthScope(true)
        .build();

    final SimpleResponse response = stub.unaryCall(request);
    assertFalse(response.getUsername().isEmpty());
    assertTrue("Received username: " + response.getUsername(),
        jsonKey.contains(response.getUsername()));
    assertFalse(response.getOauthScope().isEmpty());
    assertTrue("Received oauth scope: " + response.getOauthScope(),
        authScope.contains(response.getOauthScope()));
  }

  /** Sends a unary rpc with "per rpc" raw oauth2 access token credentials. */
  public void perRpcCreds(String jsonKey, InputStream credentialsStream, String oauthScope)
      throws Exception {
    // In gRpc Java, we don't have per Rpc credentials, user can use an intercepted stub only once
    // for that purpose.
    // So, this test is identical to oauth2_auth_token test.
    oauth2AuthToken(jsonKey, credentialsStream, oauthScope);
  }

  protected static void assertSuccess(StreamRecorder<?> recorder) {
    if (recorder.getError() != null) {
      throw new AssertionError(recorder.getError());
    }
  }
}
