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

import static junit.framework.Assert.assertEquals;
import static junit.framework.Assert.assertNull;
import static junit.framework.Assert.assertTrue;
import static junit.framework.Assert.fail;

import android.os.AsyncTask;
import android.util.Log;
import com.google.protobuf.nano.EmptyProtos;
import com.google.protobuf.nano.MessageNano;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors;
import io.grpc.ClientInterceptors.CheckedForwardingClientCall;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
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
import io.grpc.android.integrationtest.nano.TestServiceGrpc;
import io.grpc.android.integrationtest.nano.UnimplementedServiceGrpc;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.StreamRecorder;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

/**
 * Implementation of the integration tests, as an AsyncTask.
 */
final class InteropTester extends AsyncTask<Void, Void, String> {
  static final String SUCCESS_MESSAGE = "Succeed!!!";
  static final String LOG_TAG = "GrpcTest";

  private ManagedChannel channel;
  private TestServiceGrpc.TestServiceBlockingStub blockingStub;
  private TestServiceGrpc.TestServiceStub asyncStub;
  private String testCase;
  private TestListener listener;
  private static final int TIMEOUT_MILLIS = 5000;

  private static final class ResponseObserver
      implements StreamObserver<Messages.StreamingOutputCallResponse> {
    public LinkedBlockingQueue<Object> responses = new LinkedBlockingQueue<Object>();
    final Object magicTailResponse = new Object();

    @Override
    public void onNext(Messages.StreamingOutputCallResponse value) {
      responses.add(value);
    }

    @Override
    public void onError(Throwable t) {
      Log.e(LOG_TAG, "Encounter an error", t);
      responses.add(t);
    }

    @Override
    public void onCompleted() {
      responses.add(magicTailResponse);
    }
  }


  public InteropTester(String testCase,
                       ManagedChannel channel,
                       TestListener listener,
                       boolean useGet) {
    this.testCase = testCase;
    this.listener = listener;
    this.channel = channel;
    Channel channelToUse = channel;
    if (useGet) {
      channelToUse = ClientInterceptors.intercept(channel, new SafeMethodChannelInterceptor());
    }
    blockingStub = TestServiceGrpc.newBlockingStub(channelToUse);
    asyncStub = TestServiceGrpc.newStub(channelToUse);
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
    } catch (Throwable t) {
      // Print the stack trace to logcat.
      t.printStackTrace();
      // Then print to the error message.
      StringWriter sw = new StringWriter();
      t.printStackTrace(new PrintWriter(sw));
      return "Failed... : " + t.getMessage() + "\n" + sw.toString();
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
    Log.i(LOG_TAG, "Running test " + testCase);
    if ("all".equals(testCase)) {
      runTest("empty_unary");
      runTest("large_unary");
      runTest("client_streaming");
      runTest("server_streaming");
      runTest("ping_pong");
      runTest("empty_stream");
      runTest("cancel_after_begin");
      runTest("cancel_after_first_response");
      runTest("full_duplex_call_should_succeed");
      runTest("half_duplex_call_should_succeed");
      runTest("server_streaming_should_be_flow_controlled");
      runTest("very_large_request");
      runTest("very_large_response");
      runTest("deadline_not_exceeded");
      runTest("deadline_exceeded");
      runTest("deadline_exceeded_server_streaming");
      runTest("unimplemented_method");
      runTest("timeout_on_sleeping_server");
      // This has to be the last one, because it will shut down the channel.
      runTest("graceful_shutdown");
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
    if (shouldSkip()) {
      return;
    }
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
    assertTrue(recorder.awaitCompletion(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
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

    ResponseObserver responseObserver = new ResponseObserver();
    StreamObserver<Messages.StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    for (int i = 0; i < requests.length; i++) {
      requestObserver.onNext(requests[i]);
      Object response = responseObserver.responses.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS);
      if (!(response instanceof Messages.StreamingOutputCallResponse)) {
        fail("Unexpected: " + response);
      }
      assertMessageEquals(goldenResponses[i], (Messages.StreamingOutputCallResponse) response);
      assertTrue("More than 1 responses received for ping pong test.",
          responseObserver.responses.isEmpty());
    }
    requestObserver.onCompleted();
    assertEquals(responseObserver.magicTailResponse,
        responseObserver.responses.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
  }

  public void emptyStream() throws Exception {
    ResponseObserver responseObserver = new ResponseObserver();
    StreamObserver<StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    requestObserver.onCompleted();
    assertEquals(responseObserver.magicTailResponse,
        responseObserver.responses.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
  }

  public void cancelAfterBegin() throws Exception {
    StreamRecorder<StreamingInputCallResponse> responseObserver = StreamRecorder.create();
    StreamObserver<StreamingInputCallRequest> requestObserver =
        asyncStub.streamingInputCall(responseObserver);
    requestObserver.onError(new RuntimeException());
    assertTrue(responseObserver.awaitCompletion(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
    assertEquals(Arrays.<StreamingInputCallResponse>asList(), responseObserver.getValues());
    assertCodeEquals(io.grpc.Status.CANCELLED,
        io.grpc.Status.fromThrowable(responseObserver.getError()));
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
    Object response = responseObserver.responses.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS);
    if (!(response instanceof Messages.StreamingOutputCallResponse)) {
      fail("Unexpected: " + response);
    }
    assertMessageEquals(goldenResponse, (Messages.StreamingOutputCallResponse) response);

    requestObserver.onError(new RuntimeException());
    response = responseObserver.responses.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS);
    if (!(response instanceof Throwable)) {
      fail("Unexpected: " + response);
    }
    assertCodeEquals(io.grpc.Status.CANCELLED, io.grpc.Status.fromThrowable((Throwable) response));
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
    assertTrue(recorder.awaitCompletion(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
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
    assertTrue(recorder.awaitCompletion(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
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
        (StreamingOutputCallResponse) queue.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
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
        (StreamingOutputCallResponse) queue.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
    assertCodeEquals(io.grpc.Status.OK,
        (io.grpc.Status) queue.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
  }

  public void veryLargeRequest() throws Exception {
    if (shouldSkip()) {
      return;
    }
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
    if (shouldSkip()) {
      return;
    }
    final SimpleRequest request = new SimpleRequest();
    request.responseSize = unaryPayloadLength();
    request.responseType = Messages.COMPRESSABLE;

    SimpleResponse resp = blockingStub.unaryCall(request);
    final SimpleResponse goldenResponse = new SimpleResponse();
    goldenResponse.payload = new Payload();
    goldenResponse.payload.type = Messages.COMPRESSABLE;
    goldenResponse.payload.body = new byte[unaryPayloadLength()];

    assertMessageSizeEquals(goldenResponse, resp);
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
    StreamingOutputCallRequest request = new StreamingOutputCallRequest();
    request.responseParameters = new ResponseParameters[1];
    request.responseParameters[0] = new ResponseParameters();
    request.responseParameters[0].intervalUs = 20000;
    try {
      stub.streamingOutputCall(request).next();
      fail("Expected deadline to be exceeded");
    } catch (StatusRuntimeException ex) {
      assertCodeEquals(io.grpc.Status.DEADLINE_EXCEEDED, ex.getStatus());
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
    assertTrue(recorder.awaitCompletion(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
    assertCodeEquals(io.grpc.Status.DEADLINE_EXCEEDED,
        io.grpc.Status.fromThrowable(recorder.getError()));
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
    Object response = responseObserver.responses.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS);
    assertTrue(response instanceof Messages.StreamingOutputCallResponse);
    assertMessageEquals(goldenResponses[0], (Messages.StreamingOutputCallResponse) response);
    // Initiate graceful shutdown.
    channel.shutdown();
    // The previous ping-pong could have raced with the shutdown, but this one certainly shouldn't.
    requestObserver.onNext(requests[1]);
    response = responseObserver.responses.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS);
    assertTrue(response instanceof Messages.StreamingOutputCallResponse);
    assertMessageEquals(goldenResponses[1], (Messages.StreamingOutputCallResponse) response);
    requestObserver.onNext(requests[2]);
    response = responseObserver.responses.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS);
    assertTrue(response instanceof Messages.StreamingOutputCallResponse);
    assertMessageEquals(goldenResponses[2], (Messages.StreamingOutputCallResponse) response);
    requestObserver.onCompleted();
    assertEquals(responseObserver.magicTailResponse,
        responseObserver.responses.poll(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
  }

  /** Sends an rpc to an unimplemented method on the server. */
  public void unimplementedMethod() {
    UnimplementedServiceGrpc.UnimplementedServiceBlockingStub stub =
        UnimplementedServiceGrpc.newBlockingStub(channel);
    try {
      stub.unimplementedCall(new EmptyProtos.Empty());
      fail();
    } catch (StatusRuntimeException e) {
      assertCodeEquals(io.grpc.Status.UNIMPLEMENTED, e.getStatus());
    }
  }

  /** Start a fullDuplexCall which the server will not respond, and verify the deadline expires. */
  public void timeoutOnSleepingServer() throws Exception {
    TestServiceGrpc.TestServiceStub stub = TestServiceGrpc.newStub(channel)
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

    assertTrue(recorder.awaitCompletion(TIMEOUT_MILLIS, TimeUnit.MILLISECONDS));
    assertCodeEquals(io.grpc.Status.DEADLINE_EXCEEDED,
        io.grpc.Status.fromThrowable(recorder.getError()));
  }

  public static void assertMessageSizeEquals(MessageNano expected, MessageNano actual) {
    assertEquals(expected.getSerializedSize(), actual.getSerializedSize());
  }


  private static void assertSuccess(StreamRecorder<?> recorder) {
    if (recorder.getError() != null) {
      throw new AssertionError(recorder.getError());
    }
  }

  public static void assertMessageEquals(MessageNano expected, MessageNano actual) {
    if (!MessageNano.messageNanoEquals(expected, actual)) {
      assertEquals(expected.toString(), actual.toString());
      fail("Messages not equal, but assertEquals didn't throw");
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

  private static void assertCodeEquals(io.grpc.Status expected, io.grpc.Status actual) {
    if (expected == null) {
      fail("expected should not be null");
    }
    if (actual == null || !expected.getCode().equals(actual.getCode())) {
      assertEquals(expected, actual);
    }
  }

  public interface TestListener {
    void onPreTest();

    void onPostTest(String result);
  }

  /**
   * Some tests run on memory constrained environments.  Rather than OOM, just give up.  64 is
   * choosen as a maximum amount of memory a large test would need.
   */
  private static boolean shouldSkip() {
    Runtime r = Runtime.getRuntime();
    long usedMem = r.totalMemory() - r.freeMemory();
    long actuallyFreeMemory = r.maxMemory() - usedMem;
    long wantedFreeMemory = 64 * 1024 * 1024;
    if (actuallyFreeMemory < wantedFreeMemory) {
      Log.i(LOG_TAG, "Skipping due to lack of memory.  "
          + "Have: " + actuallyFreeMemory + " Want: " + wantedFreeMemory);
      return true;
    }
    return false;
  }

  private static final class SafeMethodChannelInterceptor implements ClientInterceptor {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      return new CheckedForwardingClientCall<ReqT, RespT>(
          next.newCall(method.toBuilder().setSafe(true).build(), callOptions)) {
        @Override
        public void checkedStart(Listener<RespT> responseListener, Metadata headers) {
          delegate().start(responseListener, headers);
        }
      };
    }
  }
}
