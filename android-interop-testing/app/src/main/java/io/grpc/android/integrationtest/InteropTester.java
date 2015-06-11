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

import com.google.protobuf.nano.MessageNano;

import android.os.AsyncTask;
import android.support.annotation.Nullable;
import android.util.Log;

import io.grpc.ChannelImpl;
import io.grpc.stub.StreamObserver;
import io.grpc.stub.StreamRecorder;
import io.grpc.transport.okhttp.OkHttpChannelBuilder;

import junit.framework.Assert;

import java.io.InputStream;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.security.KeyStore;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.util.Arrays;
import java.util.List;
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

  private ChannelImpl channel;
  private TestServiceGrpc.TestServiceBlockingStub blockingStub;
  private TestServiceGrpc.TestService asyncStub;
  private String testCase;
  private TestListener listener;

  public InteropTester(String testCase,
      String host,
      int port,
      @Nullable String serverHostOverride,
      boolean useTls,
      @Nullable InputStream testCa,
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
        channelBuilder.sslSocketFactory(getSslSocketFactory(testCa));
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
    } else {
      throw new IllegalArgumentException("Unimplemented/Unknown test case: " + testCase);
    }
  }

  public void emptyUnary() {
    assertEquals(new EmptyProtos.Empty(), blockingStub.emptyCall(new EmptyProtos.Empty()));
  }

  public void largeUnary() {
    final Messages.SimpleRequest request = new Messages.SimpleRequest();
    request.responseSize = 314159;
    request.responseType = Messages.COMPRESSABLE;
    request.payload = new Messages.Payload();
    request.payload.body = new byte[271828];

    final Messages.SimpleResponse goldenResponse = new Messages.SimpleResponse();
    goldenResponse.payload = new Messages.Payload();
    goldenResponse.payload.body = new byte[314159];
    Messages.SimpleResponse response = blockingStub.unaryCall(request);
    assertEquals(goldenResponse, response);
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
      goldenResponses[i].payload = new Messages.Payload();
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
    assertEquals(Arrays.asList(goldenResponses), recorder.getValues());
  }

  public void clientStreaming() throws Exception {
    final Messages.StreamingInputCallRequest[] requests = new Messages.StreamingInputCallRequest[4];
    for (int i = 0; i < 4; i++) {
      requests[i] = new Messages.StreamingInputCallRequest();
      requests[i].payload = new Messages.Payload();
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
      requestObserver.onValue(request);
    }
    requestObserver.onCompleted();
    assertEquals(goldenResponse, responseObserver.firstValue().get());
  }

  public void pingPong() throws Exception {
    final Messages.StreamingOutputCallRequest[] requests =
        new Messages.StreamingOutputCallRequest[4];
    for (int i = 0; i < 4; i++) {
      requests[i] = new Messages.StreamingOutputCallRequest();
      requests[i].responseParameters = new Messages.ResponseParameters[1];
      requests[i].responseParameters[0] = new Messages.ResponseParameters();
      requests[i].payload = new Messages.Payload();
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
      goldenResponses[i].payload = new Messages.Payload();
      goldenResponses[i].payload.type = Messages.COMPRESSABLE;
    }
    goldenResponses[0].payload.body = new byte[31415];
    goldenResponses[1].payload.body = new byte[9];
    goldenResponses[2].payload.body = new byte[2653];
    goldenResponses[3].payload.body = new byte[58979];

    final LinkedBlockingQueue<Object> responses = new LinkedBlockingQueue<>();
    final Object magicTailResponse = new Object();
    @SuppressWarnings("unchecked")
    StreamObserver<Messages.StreamingOutputCallResponse> responseObserver =
        new StreamObserver<Messages.StreamingOutputCallResponse>() {

          @Override
          public void onValue(Messages.StreamingOutputCallResponse value) {
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
        };
    StreamObserver<Messages.StreamingOutputCallRequest> requestObserver
        = asyncStub.fullDuplexCall(responseObserver);
    for (int i = 0; i < requests.length; i++) {
      requestObserver.onValue(requests[i]);
      Object response = responses.poll(5, TimeUnit.SECONDS);
      if (!(response instanceof Messages.StreamingOutputCallResponse)) {
        Assert.fail("Unexpected: " + response);
      }
      assertEquals(goldenResponses[i], (Messages.StreamingOutputCallResponse) response);
      Assert.assertTrue("More than 1 responses received for ping pong test.", responses.isEmpty());
    }
    requestObserver.onCompleted();
    Assert.assertEquals(magicTailResponse, responses.poll(5, TimeUnit.SECONDS));
  }

  public static void assertEquals(MessageNano expected, MessageNano actual) {
    if (!MessageNano.messageNanoEquals(expected, actual)) {
      Assert.assertEquals(expected.toString(), actual.toString());
      Assert.fail("Messages not equal, but assertEquals didn't throw");
    }
  }

  private static void assertSuccess(StreamRecorder<?> recorder) {
    if (recorder.getError() != null) {
      throw new AssertionError(recorder.getError());
    }
  }

  public static void assertEquals(List<? extends MessageNano> expected,
      List<? extends MessageNano> actual) {
    if (expected == null || actual == null) {
      Assert.assertEquals(expected, actual);
    } else if (expected.size() != actual.size()) {
      Assert.assertEquals(expected, actual);
    } else {
      for (int i = 0; i < expected.size(); i++) {
        assertEquals(expected.get(i), actual.get(i));
      }
    }
  }

  private SSLSocketFactory getSslSocketFactory(@Nullable InputStream testCa) throws Exception {
    if (testCa == null) {
      return (SSLSocketFactory) SSLSocketFactory.getDefault();
    }
    KeyStore ks = KeyStore.getInstance("AndroidKeyStore");
    ks.load(null);
    CertificateFactory cf = CertificateFactory.getInstance("X.509");
    X509Certificate cert = (X509Certificate) cf.generateCertificate(testCa);
    X500Principal principal = cert.getSubjectX500Principal();
    ks.setCertificateEntry(principal.getName("RFC2253"), cert);
    // Set up trust manager factory to use our key store.
    TrustManagerFactory trustManagerFactory =
        TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
    trustManagerFactory.init(ks);
    SSLContext context = SSLContext.getInstance("TLS");
    context.init(null, trustManagerFactory.getTrustManagers() , null);
    return context.getSocketFactory();
  }

  public interface TestListener {
    void onPreTest();

    void onPostTest(String result);
  }
}
