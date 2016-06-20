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
import static org.junit.Assert.fail;

import io.grpc.Context;
import io.grpc.Metadata;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerInterceptors;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.ManagedChannelImpl;
import io.grpc.internal.ServerImpl;
import io.grpc.stub.StreamObserver;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.IOException;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Integration test for various forms of cancellation & deadline propagation.
 */
@RunWith(JUnit4.class)
public class CascadingTest {

  @Mock
  TestServiceGrpc.TestService service;
  private ManagedChannelImpl channel;
  private ServerImpl server;
  private AtomicInteger nodeCount;
  private CountDownLatch observedCancellations;
  private CountDownLatch receivedCancellations;
  private TestServiceGrpc.TestServiceBlockingStub blockingStub;
  private TestServiceGrpc.TestServiceStub asyncStub;
  private ScheduledExecutorService scheduler;
  private ExecutorService otherWork;

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    nodeCount = new AtomicInteger();
    scheduler = Executors.newScheduledThreadPool(1);
    // Use a cached thread pool as we need a thread for each blocked call
    otherWork = Executors.newCachedThreadPool();
    channel = InProcessChannelBuilder.forName("channel").executor(otherWork).build();
    blockingStub = TestServiceGrpc.newBlockingStub(channel);
    asyncStub = TestServiceGrpc.newStub(channel);
  }

  @After
  public void tearDown() {
    Context.ROOT.attach();
    channel.shutdownNow();
    server.shutdownNow();
    otherWork.shutdownNow();
    scheduler.shutdownNow();
  }

  /**
   * Test {@link Context} expiration propagates from the first node in the call chain all the way
   * to the last.
   */
  @Test
  public void testCascadingCancellationViaOuterContextExpiration() throws Exception {
    observedCancellations = new CountDownLatch(2);
    receivedCancellations = new CountDownLatch(3);
    startChainingServer(3);

    Context.current().withDeadlineAfter(500, TimeUnit.MILLISECONDS, scheduler).attach();
    try {
      blockingStub.unaryCall(Messages.SimpleRequest.getDefaultInstance());
      fail("Expected cancellation");
    } catch (StatusRuntimeException sre) {
      // Wait for the workers to finish
      Status status = Status.fromThrowable(sre);
      assertEquals(Status.Code.DEADLINE_EXCEEDED, status.getCode());

      // Should have 3 calls before timeout propagates
      assertEquals(3, nodeCount.get());

      // Should have observed 2 cancellations responses from downstream servers
      if (!observedCancellations.await(5, TimeUnit.SECONDS)) {
        fail("Expected number of cancellations not observed by clients");
      }
      if (!receivedCancellations.await(5, TimeUnit.SECONDS)) {
        fail("Expected number of cancellations to be received by servers not observed");
      }
    }
  }

  /**
   * Test that cancellation via method deadline propagates down the call.
   */
  @Test
  public void testCascadingCancellationViaMethodTimeout() throws Exception {
    observedCancellations = new CountDownLatch(2);
    receivedCancellations = new CountDownLatch(3);
    startChainingServer(3);

    try {
      blockingStub.withDeadlineAfter(500, TimeUnit.MILLISECONDS)
          .unaryCall(Messages.SimpleRequest.getDefaultInstance());
      fail("Expected cancellation");
    } catch (StatusRuntimeException sre) {
      // Wait for the workers to finish
      Status status = Status.fromThrowable(sre);
      // Outermost caller observes deadline exceeded, the descendant RPCs are cancelled so they
      // receive cancellation.
      assertEquals(Status.Code.DEADLINE_EXCEEDED, status.getCode());

      // Should have 3 calls before deadline propagates
      assertEquals(3, nodeCount.get());
      if (!observedCancellations.await(5, TimeUnit.SECONDS)) {
        fail("Expected number of cancellations not observed by clients");
      }
      if (!receivedCancellations.await(5, TimeUnit.SECONDS)) {
        fail("Expected number of cancellations to be received by servers not observed");
      }
    }
  }

  /**
   * Test that when RPC cancellation propagates up a call chain, the cancellation of the parent
   * RPC triggers cancellation of all of its children.
   */
  @Test
  public void testCascadingCancellationViaLeafFailure() throws Exception {
    // All nodes (15) except one edge of the tree (4) will be cancelled.
    observedCancellations = new CountDownLatch(11);
    receivedCancellations = new CountDownLatch(11);
    startCallTreeServer(3);
    try {
      // Use response size limit to control tree nodeCount.
      blockingStub.unaryCall(Messages.SimpleRequest.newBuilder().setResponseSize(3).build());
      fail("Expected abort");
    } catch (StatusRuntimeException sre) {
      // Wait for the workers to finish
      Status status = Status.fromThrowable(sre);
      // Outermost caller observes ABORTED propagating up from the failing leaf,
      // The descendant RPCs are cancelled so they receive CANCELLED.
      assertEquals(Status.Code.ABORTED, status.getCode());

      if (!observedCancellations.await(5, TimeUnit.SECONDS)) {
        fail("Expected number of cancellations not observed by clients");
      }
      if (!receivedCancellations.await(5, TimeUnit.SECONDS)) {
        fail("Expected number of cancellations to be received by servers not observed");
      }
    }
  }

  /**
   * Create a chain of client to server calls which can be cancelled top down.
   */
  private void startChainingServer(final int depthThreshold)
      throws IOException {
    server = InProcessServerBuilder.forName("channel").executor(otherWork).addService(
        ServerInterceptors.intercept(TestServiceGrpc.bindService(service),
            new ServerInterceptor() {
              @Override
              public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
                  final ServerCall<ReqT, RespT> call,
                  Metadata headers,
                  ServerCallHandler<ReqT, RespT> next) {
                // Respond with the headers but nothing else.
                call.sendHeaders(new Metadata());
                call.request(1);
                return new ServerCall.Listener<ReqT>() {
                  @Override
                  public void onMessage(final ReqT message) {
                    if (nodeCount.incrementAndGet() == depthThreshold) {
                      // No need to abort so just wait for top-down cancellation
                      return;
                    }

                    Context.currentContextExecutor(otherWork).execute(new Runnable() {
                      @Override
                      public void run() {
                        try {
                          blockingStub.unaryCall((Messages.SimpleRequest) message);
                        } catch (Exception e) {
                          Status status = Status.fromThrowable(e);
                          if (status.getCode() == Status.Code.CANCELLED
                              || status.getCode() == Status.Code.DEADLINE_EXCEEDED) {
                            observedCancellations.countDown();
                          } else if (status.getCode() == Status.Code.ABORTED) {
                            // Propagate aborted back up
                            call.close(status, new Metadata());
                          }
                        }
                      }
                    });
                  }

                  @Override
                  public void onCancel() {
                    receivedCancellations.countDown();
                  }
                };
              }
            })
    ).build();
    server.start();
  }

  /**
   * Create a tree of client to server calls where each received call on the server
   * fans out to two downstream calls. Uses SimpleRequest.response_size to limit the nodeCount
   * of the tree. One of the leaves will ABORT to trigger cancellation back up to tree.
   */
  private void startCallTreeServer(int depthThreshold) throws IOException {
    final AtomicInteger nodeCount = new AtomicInteger((2 << depthThreshold) - 1);
    server = InProcessServerBuilder.forName("channel").executor(otherWork).addService(
        ServerInterceptors.intercept(TestServiceGrpc.bindService(service),
            new ServerInterceptor() {
              @Override
              public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
                  final ServerCall<ReqT, RespT> call,
                  Metadata headers,
                  ServerCallHandler<ReqT, RespT> next) {
                // Respond with the headers but nothing else.
                call.sendHeaders(new Metadata());
                call.request(1);
                return new ServerCall.Listener<ReqT>() {
                  @Override
                  public void onMessage(final ReqT message) {
                    Messages.SimpleRequest req = (Messages.SimpleRequest) message;
                    if (nodeCount.decrementAndGet() == 0) {
                      // we are in the final leaf node so trigger an ABORT upwards
                      Context.currentContextExecutor(otherWork).execute(new Runnable() {
                        @Override
                        public void run() {
                          call.close(Status.ABORTED, new Metadata());
                        }
                      });
                    } else if (req.getResponseSize() != 0) {
                      // We are in a non leaf node so fire off two requests
                      req = req.toBuilder().setResponseSize(req.getResponseSize() - 1).build();
                      for (int i = 0; i < 2; i++) {
                        asyncStub.unaryCall(req,
                            new StreamObserver<Messages.SimpleResponse>() {
                              @Override
                              public void onNext(Messages.SimpleResponse value) {

                              }

                              @Override
                              public void onError(Throwable t) {
                                Status status = Status.fromThrowable(t);
                                if (status.getCode() == Status.Code.CANCELLED) {
                                  observedCancellations.countDown();
                                }
                                // Propagate closure upwards.
                                try {
                                  call.close(status, new Metadata());
                                } catch (Throwable t2) {
                                  // Ignore error if already closed.
                                }
                              }

                              @Override
                              public void onCompleted() {
                              }
                            });
                      }
                    }
                  }

                  @Override
                  public void onCancel() {
                    receivedCancellations.countDown();
                  }
                };
              }
            })
    ).build();
    server.start();
  }
}
