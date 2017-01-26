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

package io.grpc.testing.integration;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.integration.Messages.StreamingInputCallRequest;
import io.grpc.testing.integration.Messages.StreamingInputCallResponse;
import io.grpc.testing.integration.TestServiceGrpc.TestServiceImplBase;
import io.grpc.util.MutableHandlerRegistry;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.Timeout;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link io.grpc.inprocess}.
 * Test more corner usecases, client not playing by the rules, server not playing by the rules, etc.
 */
@RunWith(JUnit4.class)
public class MoreInProcessTest {
  private static final String UNIQUE_SERVER_NAME =
      "in-process server for " + MoreInProcessTest.class;
  @Rule
  public final Timeout globalTimeout = new Timeout(1000);
  // use a mutable service registry for later registering the service impl for each test case.
  private final MutableHandlerRegistry serviceRegistry = new MutableHandlerRegistry();
  private final Server inProcessServer = InProcessServerBuilder.forName(UNIQUE_SERVER_NAME)
      .fallbackHandlerRegistry(serviceRegistry).directExecutor().build();
  private final ManagedChannel inProcessChannel =
      InProcessChannelBuilder.forName(UNIQUE_SERVER_NAME).directExecutor().build();

  @Before
  public void setUp() throws Exception {
    inProcessServer.start();
  }

  @After
  public void tearDown() throws Exception {
    inProcessChannel.shutdown();
    inProcessServer.shutdown();
    assertTrue(inProcessChannel.awaitTermination(900, TimeUnit.MILLISECONDS));
    assertTrue(inProcessServer.awaitTermination(900, TimeUnit.MILLISECONDS));
  }

  @Test
  public void asyncClientStreaming_serverResponsePriorToRequest() throws Exception {
    // implement a service
    final StreamingInputCallResponse fakeResponse =
        StreamingInputCallResponse.newBuilder().setAggregatedPayloadSize(100).build();
    TestServiceImplBase clientStreamingImpl = new TestServiceImplBase() {
      @Override
      public StreamObserver<StreamingInputCallRequest> streamingInputCall(
          StreamObserver<StreamingInputCallResponse> responseObserver) {
        // send response directly
        responseObserver.onNext(fakeResponse);
        responseObserver.onCompleted();
        return new StreamObserver<StreamingInputCallRequest>() {
          @Override
          public void onNext(StreamingInputCallRequest value) {
          }

          @Override
          public void onError(Throwable t) {
          }

          @Override
          public void onCompleted() {
          }
        };
      }
    };
    serviceRegistry.addService(clientStreamingImpl);
    // implement a client
    final CountDownLatch finishLatch = new CountDownLatch(1);
    final AtomicReference<StreamingInputCallResponse> responseRef =
        new AtomicReference<StreamingInputCallResponse>();
    final AtomicReference<Throwable> throwableRef = new AtomicReference<Throwable>();
    StreamObserver<StreamingInputCallResponse> responseObserver =
        new StreamObserver<StreamingInputCallResponse>() {
          @Override
          public void onNext(StreamingInputCallResponse response) {
            responseRef.set(response);
          }

          @Override
          public void onError(Throwable t) {
            throwableRef.set(t);
            finishLatch.countDown();
          }

          @Override
          public void onCompleted() {
            finishLatch.countDown();
          }
        };

    // make a gRPC call
    TestServiceGrpc.newStub(inProcessChannel).streamingInputCall(responseObserver);

    assertTrue(finishLatch.await(900, TimeUnit.MILLISECONDS));
    assertEquals(fakeResponse, responseRef.get());
    assertNull(throwableRef.get());
  }

  @Test
  public void asyncClientStreaming_serverErrorPriorToRequest() throws Exception {
    // implement a service
    final Status fakeError = Status.INVALID_ARGUMENT;
    TestServiceImplBase clientStreamingImpl = new TestServiceImplBase() {
      @Override
      public StreamObserver<StreamingInputCallRequest> streamingInputCall(
          StreamObserver<StreamingInputCallResponse> responseObserver) {
        // send error directly
        responseObserver.onError(new StatusRuntimeException(fakeError));
        responseObserver.onCompleted();
        return new StreamObserver<StreamingInputCallRequest>() {
          @Override
          public void onNext(StreamingInputCallRequest value) {
          }

          @Override
          public void onError(Throwable t) {
          }

          @Override
          public void onCompleted() {
          }
        };
      }
    };
    serviceRegistry.addService(clientStreamingImpl);
    // implement a client
    final CountDownLatch finishLatch = new CountDownLatch(1);
    final AtomicReference<StreamingInputCallResponse> responseRef =
        new AtomicReference<StreamingInputCallResponse>();
    final AtomicReference<Throwable> throwableRef = new AtomicReference<Throwable>();
    StreamObserver<StreamingInputCallResponse> responseObserver =
        new StreamObserver<StreamingInputCallResponse>() {
          @Override
          public void onNext(StreamingInputCallResponse response) {
            responseRef.set(response);
          }

          @Override
          public void onError(Throwable t) {
            throwableRef.set(t);
            finishLatch.countDown();
          }

          @Override
          public void onCompleted() {
            finishLatch.countDown();
          }
        };

    // make a gRPC call
    TestServiceGrpc.newStub(inProcessChannel).streamingInputCall(responseObserver);

    assertTrue(finishLatch.await(900, TimeUnit.MILLISECONDS));
    assertEquals(fakeError.getCode(), Status.fromThrowable(throwableRef.get()).getCode());
    assertNull(responseRef.get());
  }

  @Test
  public void asyncClientStreaming_erroneousServiceImpl() throws Exception {
    // implement a service
    TestServiceImplBase clientStreamingImpl = new TestServiceImplBase() {
      @Override
      public StreamObserver<StreamingInputCallRequest> streamingInputCall(
          StreamObserver<StreamingInputCallResponse> responseObserver) {
        StreamObserver<StreamingInputCallRequest> requestObserver =
            new StreamObserver<StreamingInputCallRequest>() {
              @Override
              public void onNext(StreamingInputCallRequest value) {
                throw new RuntimeException(
                    "unexpected error due to careless implementation of serviceImpl");
              }

              @Override
              public void onError(Throwable t) {
              }

              @Override
              public void onCompleted() {
              }
            };
        return requestObserver;
      }
    };
    serviceRegistry.addService(clientStreamingImpl);
    // implement a client
    final CountDownLatch finishLatch = new CountDownLatch(1);
    final AtomicReference<StreamingInputCallResponse> responseRef =
        new AtomicReference<StreamingInputCallResponse>();
    final AtomicReference<Throwable> throwableRef = new AtomicReference<Throwable>();
    StreamObserver<StreamingInputCallResponse> responseObserver =
        new StreamObserver<StreamingInputCallResponse>() {
          @Override
          public void onNext(StreamingInputCallResponse response) {
            responseRef.set(response);
          }

          @Override
          public void onError(Throwable t) {
            throwableRef.set(t);
            finishLatch.countDown();
          }

          @Override
          public void onCompleted() {
            finishLatch.countDown();
          }
        };

    // make a gRPC call
    TestServiceGrpc.newStub(inProcessChannel).streamingInputCall(responseObserver)
        .onNext(StreamingInputCallRequest.getDefaultInstance());

    assertTrue(finishLatch.await(900, TimeUnit.MILLISECONDS));
    assertEquals(Status.UNKNOWN, Status.fromThrowable(throwableRef.get()));
    assertNull(responseRef.get());
  }
}
