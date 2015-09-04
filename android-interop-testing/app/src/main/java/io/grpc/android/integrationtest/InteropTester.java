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

package io.grpc.android.integrationtest;

import com.google.protobuf.nano.EmptyProtos;
import com.google.protobuf.nano.MessageNano;

import android.annotation.TargetApi;
import android.net.SSLCertificateSocketFactory;
import android.os.AsyncTask;
import android.os.Build;
import android.support.annotation.Nullable;
import android.util.Log;

import static junit.framework.Assert.assertEquals;
import static junit.framework.Assert.assertNull;
import static junit.framework.Assert.assertTrue;
import static junit.framework.Assert.fail;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.StatusRuntimeException;
import io.grpc.android.integrationtest.nano.Messages;
import io.grpc.android.integrationtest.nano.Messages.Payload;
import io.grpc.android.integrationtest.nano.Messages.ResponseParameters;
import io.grpc.android.integrationtest.nano.Messages.SimpleRequest;
import io.grpc.android.integrationtest.nano.Messages.SimpleResponse;
import io.grpc.android.integrationtest.nano.Messages.StreamingInputCallRequest;
import io.grpc.android.integrationtest.nano.Messages.StreamingInputCallResponse;
import io.grpc.android.integrationtest.nano.Messages.StreamingOutputCallRequest;
import io.grpc.android.integrationtest.nano.Messages.StreamingOutputCallResponse;
import io.grpc.okhttp.OkHttpChannelBuilder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.StreamRecorder;

import java.io.InputStream;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.lang.RuntimeException;
import java.lang.reflect.Method;
import java.security.KeyStore;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.TrustManager;
import javax.net.ssl.TrustManagerFactory;
import javax.security.auth.x500.X500Principal;

/**
 * Implementation of the integration tests, as an AsyncTask.
 */
public final class InteropTester extends AsyncTask<Void, Void, String> {
  final static String SUCCESS_MESSAGE = "Succeed!!!";

  private ManagedChannel channel;
  private TestServiceGrpc.TestServiceBlockingStub blockingStub;
  private TestServiceGrpc.TestService asyncStub;
  private String testCase;
  private TestListener listener;
  private static int OPERATION_TIMEOUT = 5000;

  class ResponseObserver implements StreamObserver<Messages.StreamingOutputCallResponse> {
    public LinkedBlockingQueue<Object> responses = new LinkedBlockingQueue<Object>();
    final Object magicTailResponse = new Object();

    @Override
    public void onNext(Messages.StreamingOutputCallResponse value) {
      responses.add(value);
    }

    @Override
    public void onError(Throwable t) {
      Log.e(TesterActivity.LOG_TAG, "Encounter an error", t);
      responses.add(t);
    }

    @Override
    public void onCompleted() {
      responses.add(magicTailResponse);
    }
  }


  public InteropTester(String testCase,
                       String host,
                       int port,
                       @Nullable String serverHostOverride,
                       boolean useTls,
                       @Nullable InputStream testCa,
                       @Nullable String androidSocketFactoryTls,
                       TestListener listener) {
    this.testCase = testCase;
    this.listener = listener;

    OkHttpChannelBuilder channelBuilder = OkHttpChannelBuilder.forAddress(host, port);
    if (serverHostOverride != null) {
      // Force the hostname to match the cert the server uses.
      channelBuilder.overrideHostForAuthority(serverHostOverride);
    }
    if (useTls) {
      try {
        SSLSocketFactory factory;
        if (androidSocketFactoryTls != null) {
          factory = getSslCertificateSocketFactory(testCa, androidSocketFactoryTls);
        } else {
          factory = getSslSocketFactory(testCa);
        }
        channelBuilder.sslSocketFactory(factory);
      } catch (Exception e) {
        throw new RuntimeException(e);
      }
    }

    channel = channelBuilder.build();
    blockingStub = TestServiceGrpc.newBlockingStub(channel);
    asyncStub = TestServiceGrpc.newStub(channel);
  }

  @Override
  protected void onPreExecute() {
    listener.onPreTest();
  }

  @Override
  protected String doInBackground(Void... nothing) {
    try {
      runTest(testCase);
      return SUCCESS_MESSAGE;
    } catch (Exception | AssertionError e) {
      // Print the stack trace to logcat.
      e.printStackTrace();
      // Then print to the error message.
      StringWriter sw = new StringWriter();
      e.printStackTrace(new PrintWriter(sw));
      return "Failed... : " + e.getMessage() + "\n" + sw.toString();
    } finally {
      shutdown();
    }
  }

  @Override
  protected void onPostExecute(String result) {
    listener.onPostTest(result);
  }


  public void shutdown() {
    channel.shutdown();
  }

  public void runTest(String testCase) throws Exception {
    if ("all".equals(testCase)) {
      emptyUnary();
      largeUnary();
      clientStreaming();
      serverStreaming();
      pingPong();
      emptyStream();
      cancelAfterBegin();
      cancelAfterFirstResponse();
      fullDuplexCallShouldSucceed();
      halfDuplexCallShouldSucceed();
      serverStreamingShouldBeFlowControlled();
      veryLargeRequest();
      veryLargeResponse();
      deadlineNotExceeded();
      deadlineExceeded();
      deadlineExceededServerStreaming();
      unimplementedMethod();
      timeoutOnSleepingServer();
      // This has to be the last one, because it will shut down the channel.
      gracefulShutdown();
    } else if ("empty_unary".equals(testCase)) {
      emptyUnary();
    } else if ("large_unary".equals(testCase)) {
      largeUnary();
    } else if ("client_streaming".equals(testCase)) {
      clientStreaming();
    } else if ("server_streaming".equals(testCase)) {
      serverStreaming();
    } else if ("ping_pong".equals(testCase)) {
      pingPong();
    } else if ("empty_stream".equals(testCase)) {
      emptyStream();
    } else if ("cancel_after_begin".equals(testCase)) {
      cancelAfterBegin();
    } else if ("cancel_after_first_response".equals(testCase)) {
      cancelAfterFirstResponse();
    } else if ("full_duplex_call_should_succeed".equals(testCase)) {
      fullDuplexCallShouldSucceed();
    } else if ("half_duplex_call_should_succeed".equals(testCase)) {
      halfDuplexCallShouldSucceed();
    } else if ("server_streaming_should_be_flow_controlled".equals(testCase)) {
      serverStreamingShouldBeFlowControlled();
    } else if ("very_large_request".equals(testCase)) {
      veryLargeRequest();
    } else if ("very_large_response".equals(testCase)) {
      veryLargeResponse();
    } else if ("deadline_not_exceeded".equals(testCase)) {
      deadlineNotExceeded();
    } else if ("deadline_exceeded".equals(testCase)) {
      deadlineExceeded();
    } else if ("deadline_exceeded_server_streaming".equals(testCase)) {
      deadlineExceededServerStreaming();
    } else if ("unimplemented_method".equals(testCase)) {
      unimplementedMethod();
    } else if ("timeout_on_sleeping_server".equals(testCase)) {
      timeoutOnSleepingServer();
    } else if ("graceful_shutdown".equals(testCase)) {
      gracefulShutdown();
    } else {
      throw new IllegalArgumentException("Unimplemented/Unknown test case: " + testCase);
    }
  }

  public void emptyUnary() {
    assertMessageEquals(new EmptyProtos.Empty(), blockingStub.emptyCall(new EmptyProtos.Empty()));
  }

  public void largeUnary() {
    final Messages.SimpleRequest request = new Messages.SimpleRequest();
    request.responseSize = 314159;
    request.responseType = Messages.COMPRESSABLE;
    request.payload = new Payload();
    request.payload.body = new byte[271828];

    final Messages.SimpleResponse goldenResponse = new Messages.SimpleResponse();
    goldenResponse.payload = new Payload();
    goldenResponse.payload.body = new byte[314159];
    Messages.SimpleResponse response = blockingStub.unaryCall(request);
    assertMessageEquals(goldenResponse, response);
  }

  public void serverStreaming() throws Exception {
    final Messages.StreamingOutputCallRequest request = new Messages.StreamingOutputCallRequest();
    request.responseType = Messages.COMPRESSABLE;
    request.responseParameters = new Messages.ResponseParameters[4];
    for (int i = 0; i < 4; i++) {
      request.responseParameters[i] = new Messages.ResponseParameters();
    }
    request.responseParameters[0].size = 31415;
    request.responseParameters[1].size = 9;
    request.responseParameters[2].size = 2653;
    request.responseParameters[3].size = 58979;

    final Messages.StreamingOutputCallResponse[] goldenResponses =
        new Messages.StreamingOutputCallResponse[4];
    for (int i = 0; i < 4; i++) {
      goldenResponses[i] = new Messages.StreamingOutputCallResponse();
      goldenResponses[i].payload = new Payload();
      goldenResponses[i].payload.type = Messages.COMPRESSABLE;
    }
    goldenResponses[0].payload.body = new byte[31415];
    goldenResponses[1].payload.body = new byte[9];
    goldenResponses[2].payload.body = new byte[2653];
    goldenResponses[3].payload.body = new byte[58979];

    StreamRecorder<Messages.StreamingOutputCallResponse> recorder = StreamRecorder.create();
    asyncStub.streamingOutputCall(request, recorder);
    recorder.awaitCompletion();
    assertSuccess(recorder);
    assertMessageEquals(Arrays.asList(goldenResponses), recorder.getValues());
  }

  public void clientStreaming() throws Exception {
    final Messages.StreamingInputCallRequest[] requests = new Messages.StreamingInputCallRequest[4];
    for (int i = 0; i < 4; i++) {
      requests[i] = new Messages.StreamingInputCallRequest();
      requests[i].payload = new Payload();
    }
    requests[0].payload.body = new byte[27182];
    requests[1].payload.body = new byte[8];
    requests[2].payload.body = new byte[1828];
    requests[3].payload.body = new byte[45904];

    final Messages.StreamingInputCallResponse goldenResponse =
        new Messages.StreamingInputCallResponse();
    goldenResponse.aggregatedPayloadSize = 74922;

    StreamRecorder<Messages.StreamingInputCallResponse> responseObserver = StreamRecorder.create();
    StreamObserver<Messages.StreamingInputCallRequest> requestObserver =
        asyncStub.streamingInputCall(responseObserver);
    for (Messages.StreamingInputCallRequest request : requests) {
      requestObserver.onNext(request);
    }
    requestObserver.onCompleted();
    assertMessageEquals(goldenResponse, responseObserver.firstValue().get());
  }

  public void pingPong() throws Exception {
    final Messages.StreamingOutputCallRequest[] requests =
        new Messages.StreamingOutputCallRequest[4];
    for (int i = 0; i < 4; i++) {
      requests[i] = new Messages.StreamingOutputCallRequest();
      requests[i].responseParameters = new Messages.ResponseParameters[1];
      requests[i].responseParameters[0] = new Messages.ResponseParameters();
      requests[i].payload = new Payload();
    }
    requests[0].responseParameters[0].size = 31415;
    requests[0].payload.body = new byte[27182];
    requests[1].responseParameters[0].size = 9;
    requests[1].payload.body = new byte[8];
    requests[2].responseParameters[0].size = 2653;
    requests[2].payload.body = new byte[1828];
    requests[3].responseParameters[0].size = 58979;
    requests[3].payload.body = new byte[45904];


    final Messages.StreamingOutputCallResponse[] goldenResponses =
        new Messages.StreamingOutputCallResponse[4];
    for (int i = 0; i < 4; i++) {
      goldenResponses[i] = new Messages.StreamingOutputCallResponse();
      goldenResponses[i].payload = new Payload();
      goldenResponses[i].payload.type = Messages.COMPRESSABLE;
    }
    goldenResponses[0].payload.body = new byte[31415];
    goldenResponses[1].payload.body = new byte[9];
    goldenResponses[2].payload.body = new byte[2653];
    goldenResponses[3].payload.body = new byte[58979];

    @SuppressWarnings("unchecked")
    ResponseObserver responseObserver = new ResponseObserver();
    StreamObserver<Messages.StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    for (int i = 0; i < requests.length; i++) {
      requestObserver.onNext(requests[i]);
      Object response = responseObserver.responses.poll(OPERATION_TIMEOUT, TimeUnit.SECONDS);
      if (!(response instanceof Messages.StreamingOutputCallResponse)) {
        fail("Unexpected: " + response);
      }
      assertMessageEquals(goldenResponses[i], (Messages.StreamingOutputCallResponse) response);
      assertTrue("More than 1 responses received for ping pong test.",
          responseObserver.responses.isEmpty());
    }
    requestObserver.onCompleted();
    assertEquals(responseObserver.magicTailResponse,
        responseObserver.responses.poll(OPERATION_TIMEOUT, TimeUnit.SECONDS));
  }

  public void emptyStream() throws Exception {
    @SuppressWarnings("unchecked")
    ResponseObserver responseObserver = new ResponseObserver();
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    requestObserver.onCompleted();
    assertEquals(responseObserver.magicTailResponse,
        responseObserver.responses.poll(OPERATION_TIMEOUT, TimeUnit.SECONDS));
  }

  public void cancelAfterBegin() throws Exception {
    StreamRecorder<StreamingInputCallResponse> responseObserver = StreamRecorder.create();
    StreamObserver<StreamingInputCallRequest> requestObserver =
        asyncStub.streamingInputCall(responseObserver);
    requestObserver.onError(new RuntimeException());
    responseObserver.awaitCompletion();
    assertEquals(Arrays.<StreamingInputCallResponse>asList(), responseObserver.getValues());
    assertEquals(io.grpc.Status.CANCELLED.getCode(),
        io.grpc.Status.fromThrowable(responseObserver.getError()).getCode());
  }

  public void cancelAfterFirstResponse() throws Exception {
    final StreamingOutputCallRequest request = new StreamingOutputCallRequest();
    request.responseParameters = new Messages.ResponseParameters[1];
    request.responseParameters[0] = new ResponseParameters();
    request.responseParameters[0].size = 31415;
    request.payload = new Payload();
    request.payload.body = new byte[27182];
    final StreamingOutputCallResponse goldenResponse = new StreamingOutputCallResponse();
    goldenResponse.payload = new Payload();
    goldenResponse.payload.type = Messages.COMPRESSABLE;
    goldenResponse.payload.body = new byte[31415];

    ResponseObserver responseObserver = new ResponseObserver();
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    requestObserver.onNext(request);
    Object response = responseObserver.responses.poll(OPERATION_TIMEOUT, TimeUnit.SECONDS);
    if (!(response instanceof Messages.StreamingOutputCallResponse)) {
      fail("Unexpected: " + response);
    }
    assertMessageEquals(goldenResponse, (Messages.StreamingOutputCallResponse) response);

    requestObserver.onError(new RuntimeException());
    response = responseObserver.responses.poll(OPERATION_TIMEOUT, TimeUnit.SECONDS);
    if (!(response instanceof Throwable)) {
      fail("Unexpected: " + response);
    }
    assertEquals(io.grpc.Status.CANCELLED.getCode(),
        io.grpc.Status.fromThrowable((Throwable) response).getCode());
  }

  public void fullDuplexCallShouldSucceed() throws Exception {
    // Build the request.
    Integer[] responseSizes = {50, 100, 150, 200};
    final StreamingOutputCallRequest request = new StreamingOutputCallRequest();
    request.responseParameters = new ResponseParameters[responseSizes.length];
    request.responseType = Messages.COMPRESSABLE;
    for (int i = 0; i < responseSizes.length; ++i) {
      request.responseParameters[i] = new ResponseParameters();
      request.responseParameters[i].size = responseSizes[i];
      request.responseParameters[i].intervalUs = 0;
    }

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
    assertEquals(responseSizes.length * numRequests, recorder.getValues().size());
    for (int ix = 0; ix < recorder.getValues().size(); ++ix) {
      StreamingOutputCallResponse response = recorder.getValues().get(ix);
      assertEquals(Messages.COMPRESSABLE, response.payload.type);
      int length = response.payload.body.length;
      int expectedSize = responseSizes[ix % responseSizes.length];
      assertEquals("comparison failed at index " + ix, expectedSize, length);
    }
  }

  public void halfDuplexCallShouldSucceed() throws Exception {
    // Build the request.
    Integer[] responseSizes = {50, 100, 150, 200};
    final StreamingOutputCallRequest request = new StreamingOutputCallRequest();
    request.responseParameters = new ResponseParameters[responseSizes.length];
    request.responseType = Messages.COMPRESSABLE;
    for (int i = 0; i < responseSizes.length; ++i) {
      request.responseParameters[i] = new ResponseParameters();
      request.responseParameters[i].size = responseSizes[i];
      request.responseParameters[i].intervalUs = 0;
    }

    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<StreamingOutputCallRequest> requestStream = asyncStub.halfDuplexCall(recorder);

    final int numRequests = 10;
    for (int ix = numRequests; ix > 0; --ix) {
      requestStream.onNext(request);
    }
    requestStream.onCompleted();
    recorder.awaitCompletion();
    assertSuccess(recorder);
    assertEquals(responseSizes.length * numRequests, recorder.getValues().size());
    for (int ix = 0; ix < recorder.getValues().size(); ++ix) {
      StreamingOutputCallResponse response = recorder.getValues().get(ix);
      assertEquals(Messages.COMPRESSABLE, response.payload.type);
      int length = response.payload.body.length;
      int expectedSize = responseSizes[ix % responseSizes.length];
      assertEquals("comparison failed at index " + ix, expectedSize, length);
    }
  }

  public void serverStreamingShouldBeFlowControlled() throws Exception {
    final StreamingOutputCallRequest request = new StreamingOutputCallRequest();
    request.responseType = Messages.COMPRESSABLE;
    request.responseParameters = new ResponseParameters[2];
    request.responseParameters[0] = new ResponseParameters();
    request.responseParameters[0].size = 100000;
    request.responseParameters[1] = new ResponseParameters();
    request.responseParameters[1].size = 100001;
    final StreamingOutputCallResponse[] goldenResponses = new StreamingOutputCallResponse[2];
    goldenResponses[0] = new StreamingOutputCallResponse();
    goldenResponses[0].payload = new Payload();
    goldenResponses[0].payload.type = Messages.COMPRESSABLE;
    goldenResponses[0].payload.body = new byte[100000];
    goldenResponses[1] = new StreamingOutputCallResponse();
    goldenResponses[1].payload = new Payload();
    goldenResponses[1].payload.type = Messages.COMPRESSABLE;
    goldenResponses[1].payload.body = new byte[100001];

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
      public void onClose(io.grpc.Status status, Metadata trailers) {
        queue.add(status);
      }
    }, new Metadata());
    call.sendMessage(request);
    call.halfClose();

    // Time how long it takes to get the first response.
    call.request(1);
    assertMessageEquals(goldenResponses[0],
        (StreamingOutputCallResponse) queue.poll(OPERATION_TIMEOUT, TimeUnit.MILLISECONDS));
    long firstCallDuration = System.nanoTime() - start;

    // Without giving additional flow control, make sure that we don't get another response. We wait
    // until we are comfortable the next message isn't coming. We may have very low nanoTime
    // resolution (like on Windows) or be using a testing, in-process transport where message
    // handling is instantaneous. In both cases, firstCallDuration may be 0, so round up sleep time
    // to at least 1ms.
    assertNull(queue.poll(Math.max(firstCallDuration * 4, 1 * 1000 * 1000), TimeUnit.NANOSECONDS));

    // Make sure that everything still completes.
    call.request(1);
    assertMessageEquals(goldenResponses[1],
        (StreamingOutputCallResponse) queue.poll(OPERATION_TIMEOUT, TimeUnit.MILLISECONDS));
    assertEquals(io.grpc.Status.OK, queue.poll(OPERATION_TIMEOUT, TimeUnit.MILLISECONDS));
  }

  public void veryLargeRequest() throws Exception {
    final SimpleRequest request = new SimpleRequest();
    request.payload = new Payload();
    request.payload.type = Messages.COMPRESSABLE;
    request.payload.body = new byte[unaryPayloadLength()];
    request.responseSize = 10;
    request.responseType = Messages.COMPRESSABLE;
    final SimpleResponse goldenResponse = new SimpleResponse();
    goldenResponse.payload = new Payload();
    goldenResponse.payload.type = Messages.COMPRESSABLE;
    goldenResponse.payload.body = new byte[10];

    assertMessageEquals(goldenResponse, blockingStub.unaryCall(request));
  }

  public void veryLargeResponse() throws Exception {
    final SimpleRequest request = new SimpleRequest();
    request.responseSize = unaryPayloadLength();
    request.responseType = Messages.COMPRESSABLE;

    final SimpleResponse goldenResponse = new SimpleResponse();
    goldenResponse.payload = new Payload();
    goldenResponse.payload.type = Messages.COMPRESSABLE;
    goldenResponse.payload.body = new byte[unaryPayloadLength()];

    assertMessageEquals(goldenResponse, blockingStub.unaryCall(request));
  }

  public void deadlineNotExceeded() {
    // warm up the channel and JVM
    blockingStub.emptyCall(new EmptyProtos.Empty());
    StreamingOutputCallRequest request = new StreamingOutputCallRequest();
    request.responseParameters = new ResponseParameters[1];
    request.responseParameters[0] = new ResponseParameters();
    request.responseParameters[0].intervalUs = 0;
    TestServiceGrpc.newBlockingStub(channel)
        .withDeadlineAfter(10, TimeUnit.SECONDS)
        .streamingOutputCall(request);
  }

  public void deadlineExceeded() {
    // warm up the channel and JVM
    blockingStub.emptyCall(new EmptyProtos.Empty());
    TestServiceGrpc.TestServiceBlockingStub stub = TestServiceGrpc.newBlockingStub(channel)
        .withDeadlineAfter(10, TimeUnit.MILLISECONDS);
    try {
      StreamingOutputCallRequest request = new StreamingOutputCallRequest();
      request.responseParameters = new ResponseParameters[1];
      request.responseParameters[0] = new ResponseParameters();
      request.responseParameters[0].intervalUs = 20000;
      stub.streamingOutputCall(request).next();
      fail("Expected deadline to be exceeded");
    } catch (Throwable t) {
      assertEquals(io.grpc.Status.DEADLINE_EXCEEDED.getCode(),
          io.grpc.Status.fromThrowable(t).getCode());
    }
  }

  public void deadlineExceededServerStreaming() throws Exception {
    // warm up the channel and JVM
    blockingStub.emptyCall(new EmptyProtos.Empty());
    ResponseParameters responseParameters = new ResponseParameters();
    responseParameters.size = 1;
    responseParameters.intervalUs = 10000;
    StreamingOutputCallRequest request = new StreamingOutputCallRequest();
    request.responseType = Messages.COMPRESSABLE;
    request.responseParameters = new ResponseParameters[4];
    request.responseParameters[0] = responseParameters;
    request.responseParameters[1] = responseParameters;
    request.responseParameters[2] = responseParameters;
    request.responseParameters[3] = responseParameters;
    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    TestServiceGrpc.newStub(channel)
        .withDeadlineAfter(30, TimeUnit.MILLISECONDS)
        .streamingOutputCall(request, recorder);
    recorder.awaitCompletion();
    assertEquals(io.grpc.Status.DEADLINE_EXCEEDED.getCode(),
        io.grpc.Status.fromThrowable(recorder.getError()).getCode());
  }

  protected int unaryPayloadLength() {
    // 10MiB.
    return 10485760;
  }

  public void gracefulShutdown() throws Exception {
    StreamingOutputCallRequest[] requests = new StreamingOutputCallRequest[3];
    requests[0] = new StreamingOutputCallRequest();
    requests[0].responseParameters = new ResponseParameters[1];
    requests[0].responseParameters[0] = new ResponseParameters();
    requests[0].responseParameters[0].size = 3;
    requests[0].payload = new Payload();
    requests[0].payload.body = new byte[2];
    requests[1] = new StreamingOutputCallRequest();
    requests[1].responseParameters = new ResponseParameters[1];
    requests[1].responseParameters[0] = new ResponseParameters();
    requests[1].responseParameters[0].size = 1;
    requests[1].payload = new Payload();
    requests[1].payload.body = new byte[7];
    requests[2] = new StreamingOutputCallRequest();
    requests[2].responseParameters = new ResponseParameters[1];
    requests[2].responseParameters[0] = new ResponseParameters();
    requests[2].responseParameters[0].size = 4;
    requests[2].payload = new Payload();
    requests[2].payload.body = new byte[1];

    StreamingOutputCallResponse[] goldenResponses = new StreamingOutputCallResponse[3];
    goldenResponses[0] = new StreamingOutputCallResponse();
    goldenResponses[0].payload = new Payload();
    goldenResponses[0].payload.type = Messages.COMPRESSABLE;
    goldenResponses[0].payload.body = new byte[3];
    goldenResponses[1] = new StreamingOutputCallResponse();
    goldenResponses[1].payload = new Payload();
    goldenResponses[1].payload.type = Messages.COMPRESSABLE;
    goldenResponses[1].payload.body = new byte[1];
    goldenResponses[2] = new StreamingOutputCallResponse();
    goldenResponses[2].payload = new Payload();
    goldenResponses[2].payload.type = Messages.COMPRESSABLE;
    goldenResponses[2].payload.body = new byte[4];


    ResponseObserver responseObserver = new ResponseObserver();
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    requestObserver.onNext(requests[0]);
    Object response = responseObserver.responses.poll(OPERATION_TIMEOUT, TimeUnit.SECONDS);
    assertTrue(response instanceof Messages.StreamingOutputCallResponse);
    assertMessageEquals(goldenResponses[0], (Messages.StreamingOutputCallResponse) response);
    // Initiate graceful shutdown.
    channel.shutdown();
    // The previous ping-pong could have raced with the shutdown, but this one certainly shouldn't.
    requestObserver.onNext(requests[1]);
    response = responseObserver.responses.poll(OPERATION_TIMEOUT, TimeUnit.SECONDS);
    assertTrue(response instanceof Messages.StreamingOutputCallResponse);
    assertMessageEquals(goldenResponses[1], (Messages.StreamingOutputCallResponse) response);
    requestObserver.onNext(requests[2]);
    response = responseObserver.responses.poll(OPERATION_TIMEOUT, TimeUnit.SECONDS);
    assertTrue(response instanceof Messages.StreamingOutputCallResponse);
    assertMessageEquals(goldenResponses[2], (Messages.StreamingOutputCallResponse) response);
    requestObserver.onCompleted();
    assertEquals(responseObserver.magicTailResponse,
        responseObserver.responses.poll(OPERATION_TIMEOUT, TimeUnit.SECONDS));
  }

  /** Sends an rpc to an unimplemented method on the server. */
  public void unimplementedMethod() {
    UnimplementedServiceGrpc.UnimplementedServiceBlockingStub stub =
        UnimplementedServiceGrpc.newBlockingStub(channel);
    try {
      stub.unimplementedCall(new EmptyProtos.Empty());
      fail();
    } catch (StatusRuntimeException e) {
      assertEquals(io.grpc.Status.UNIMPLEMENTED.getCode(), e.getStatus().getCode());
    }
  }

  /** Start a fullDuplexCall which the server will not respond, and verify the deadline expires. */
  public void timeoutOnSleepingServer() throws Exception {
    TestServiceGrpc.TestService stub = TestServiceGrpc.newStub(channel)
        .withDeadlineAfter(1, TimeUnit.MILLISECONDS);
    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = stub.fullDuplexCall(recorder);

    try {
      StreamingOutputCallRequest request = new StreamingOutputCallRequest();
      request.payload = new Messages.Payload();
      request.payload.body = new byte[27182];
      requestObserver.onNext(request);
    } catch (IllegalStateException expected) {
      // This can happen if the stream has already been terminated due to deadline exceeded.
    }

    recorder.awaitCompletion();
    assertEquals(io.grpc.Status.DEADLINE_EXCEEDED.getCode(),
        io.grpc.Status.fromThrowable(recorder.getError()).getCode());
  }

  public static void assertMessageEquals(MessageNano expected, MessageNano actual) {
    if (!MessageNano.messageNanoEquals(expected, actual)) {
      assertEquals(expected.toString(), actual.toString());
      fail("Messages not equal, but assertEquals didn't throw");
    }
  }

  private static void assertSuccess(StreamRecorder<?> recorder) {
    if (recorder.getError() != null) {
      throw new AssertionError(recorder.getError());
    }
  }

  public static void assertMessageEquals(List<? extends MessageNano> expected,
                                         List<? extends MessageNano> actual) {
    if (expected == null || actual == null) {
      assertEquals(expected, actual);
    } else if (expected.size() != actual.size()) {
      assertEquals(expected, actual);
    } else {
      for (int i = 0; i < expected.size(); i++) {
        assertMessageEquals(expected.get(i), actual.get(i));
      }
    }
  }

  private SSLSocketFactory getSslSocketFactory(@Nullable InputStream testCa) throws Exception {
    if (testCa == null) {
      return (SSLSocketFactory) SSLSocketFactory.getDefault();
    }

    SSLContext context = SSLContext.getInstance("TLS");
    context.init(null, getTrustManagers(testCa) , null);
    return context.getSocketFactory();
  }

  @TargetApi(14)
  private SSLCertificateSocketFactory getSslCertificateSocketFactory(
      @Nullable InputStream testCa, String androidSocketFatoryTls) throws Exception {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.ICE_CREAM_SANDWICH /* API level 14 */) {
      throw new RuntimeException(
          "android_socket_factory_tls doesn't work with API level less than 14.");
    }
    SSLCertificateSocketFactory factory = (SSLCertificateSocketFactory)
        SSLCertificateSocketFactory.getDefault(5000 /* Timeout in ms*/);
    // Use HTTP/2.0
    byte[] h2 = "h2".getBytes();
    byte[][] protocols = new byte[][]{h2};
    if (androidSocketFatoryTls.equals("alpn")) {
      Method setAlpnProtocols =
          factory.getClass().getDeclaredMethod("setAlpnProtocols", byte[][].class);
      setAlpnProtocols.invoke(factory, new Object[] { protocols });
    } else if (androidSocketFatoryTls.equals("npn")) {
      Method setNpnProtocols =
          factory.getClass().getDeclaredMethod("setNpnProtocols", byte[][].class);
      setNpnProtocols.invoke(factory, new Object[]{protocols});
    } else {
      throw new RuntimeException("Unknown protocol: " + androidSocketFatoryTls);
    }

    if (testCa != null) {
      factory.setTrustManagers(getTrustManagers(testCa));
    }

    return factory;
  }

  private TrustManager[] getTrustManagers(InputStream testCa) throws Exception {
    KeyStore ks = KeyStore.getInstance(KeyStore.getDefaultType());
    ks.load(null);
    CertificateFactory cf = CertificateFactory.getInstance("X.509");
    X509Certificate cert = (X509Certificate) cf.generateCertificate(testCa);
    X500Principal principal = cert.getSubjectX500Principal();
    ks.setCertificateEntry(principal.getName("RFC2253"), cert);
    // Set up trust manager factory to use our key store.
    TrustManagerFactory trustManagerFactory =
        TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
    trustManagerFactory.init(ks);
    return trustManagerFactory.getTrustManagers();
  }

  public interface TestListener {
    void onPreTest();

    void onPostTest(String result);
  }
}