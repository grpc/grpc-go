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
import io.grpc.MethodDescriptor;
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
import java.util.concurrent.Executor;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.Semaphore;
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
  private AtomicInteger depth;
  private AtomicInteger observedCancellations;
  private AtomicInteger receivedCancellations;
  private TestServiceGrpc.TestServiceBlockingStub blockingStub;
  private TestServiceGrpc.TestServiceStub asyncStub;
  private ScheduledExecutorService scheduler;

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    channel = InProcessChannelBuilder.forName("channel").build();
    depth = new AtomicInteger();
    observedCancellations = new AtomicInteger();
    receivedCancellations = new AtomicInteger();
    blockingStub = TestServiceGrpc.newBlockingStub(channel);
    asyncStub = TestServiceGrpc.newStub(channel);
    scheduler = Executors.newScheduledThreadPool(1);
  }

  @After
  public void tearDown() {
    Context.ROOT.attach();
    channel.shutdownNow();
    server.shutdownNow();
  }

  /**
   * Test {@link Context} expiration propagates from the first node in the call chain all the way
   * to the last.
   */
  @Test
  public void testCascadingCancellationViaOuterContextExpiration() throws Exception {
    startChainingServer(3);
    Context.current().withDeadlineAfter(150, TimeUnit.MILLISECONDS, scheduler).attach();
    try {
      blockingStub.unaryCall(Messages.SimpleRequest.getDefaultInstance());
      fail("Expected cancellation");
    } catch (StatusRuntimeException sre) {
      // Wait for the workers to finish
      Thread.sleep(500);
      Status status = Status.fromThrowable(sre);
      assertEquals(Status.Code.CANCELLED, status.getCode());

      // Should have 3 calls before timeout propagates
      assertEquals(3, depth.get());

      // Should have observed 2 cancellations responses from downstream servers
      assertEquals(2, observedCancellations.get());
      // and received 3 cancellations from upstream clients
      assertEquals(3, receivedCancellations.get());
    }
  }

  /**
   * Test that cancellation via method deadline propagates down the call.
   */
  @Test
  public void testCascadingCancellationViaMethodTimeout() throws Exception {
    startChainingServer(3);
    try {
      blockingStub.withDeadlineAfter(150, TimeUnit.MILLISECONDS)
          .unaryCall(Messages.SimpleRequest.getDefaultInstance());
      fail("Expected cancellation");
    } catch (StatusRuntimeException sre) {
      // Wait for the workers to finish
      Thread.sleep(150);
      Status status = Status.fromThrowable(sre);
      // Outermost caller observes deadline exceeded, the descendant RPCs are cancelled so they
      // receive cancellation.
      assertEquals(Status.Code.DEADLINE_EXCEEDED, status.getCode());

      // Should have 3 calls before deadline propagates
      assertEquals(3, depth.get());
      // Server should have observed 2 cancellations from downstream calls
      assertEquals(2, observedCancellations.get());
      // and received 2 cancellations
      assertEquals(3, receivedCancellations.get());
    }
  }

  /**
   * Test that when RPC cancellation propagates up a call chain, the cancellation of the parent
   * RPC triggers cancellation of all of its children.
   */
  @Test
  public void testCascadingCancellationViaLeafFailure() throws Exception {
    startCallTreeServer();
    try {
      // Use response size limit to control tree depth.
      blockingStub.unaryCall(Messages.SimpleRequest.newBuilder().setResponseSize(3).build());
      fail("Expected abort");
    } catch (StatusRuntimeException sre) {
      // Wait for the workers to finish
      Thread.sleep(100);
      Status status = Status.fromThrowable(sre);
      // Outermost caller observes ABORTED propagating up from the failing leaf,
      // The descendant RPCs are cancelled so they receive CANCELLED.
      assertEquals(Status.Code.ABORTED, status.getCode());
      // All nodes (15) except one edge of the tree (4) will be cancelled.
      assertEquals(11, observedCancellations.get());
      assertEquals(11, receivedCancellations.get());
    }
  }

  /**
   * Create a chain of client to server calls which can be cancelled top down.
   */
  private void startChainingServer(final int depthThreshold)
      throws IOException {
    final Executor otherWork = Context.propagate(Executors.newCachedThreadPool());
    server = InProcessServerBuilder.forName("channel").addService(
        ServerInterceptors.intercept(TestServiceGrpc.bindService(service),
            new ServerInterceptor() {
              @Override
              public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
                  MethodDescriptor<ReqT, RespT> method,
                  final ServerCall<RespT> call,
                  Metadata headers,
                  ServerCallHandler<ReqT, RespT> next) {
                // Respond with the headers but nothing else.
                call.sendHeaders(new Metadata());
                call.request(1);
                return new ServerCall.Listener<ReqT>() {
                  @Override
                  public void onMessage(final ReqT message) {
                    // Wait and then recurse.
                    if (depth.incrementAndGet() == depthThreshold) {
                      // No need to abort so just wait for top-down cancellation
                      return;
                    }

                    otherWork.execute(new Runnable() {
                      @Override
                      public void run() {
                        try {
                          blockingStub.unaryCall((Messages.SimpleRequest) message);
                        } catch (Exception e) {
                          Status status = Status.fromThrowable(e);
                          if (status.getCode() == Status.Code.CANCELLED) {
                            observedCancellations.incrementAndGet();
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
                    receivedCancellations.incrementAndGet();
                  }
                };
              }
            })
    ).build();
    server.start();
  }

  /**
   * Create a tree of client to server calls where each received call on the server
   * fans out to two downstream calls. Uses SimpleRequest.response_size to limit the depth
   * of the tree. One of the leaves will ABORT to trigger cancellation back up to tree.
   */
  private void startCallTreeServer() throws IOException {
    final Semaphore semaphore = new Semaphore(1);
    server = InProcessServerBuilder.forName("channel").addService(
        ServerInterceptors.intercept(TestServiceGrpc.bindService(service),
            new ServerInterceptor() {
              @Override
              public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
                  MethodDescriptor<ReqT, RespT> method,
                  final ServerCall<RespT> call,
                  Metadata headers,
                  ServerCallHandler<ReqT, RespT> next) {
                // Respond with the headers but nothing else.
                call.sendHeaders(new Metadata());
                call.request(1);
                return new ServerCall.Listener<ReqT>() {
                  @Override
                  public void onMessage(final ReqT message) {
                    Messages.SimpleRequest req = (Messages.SimpleRequest) message;
                    // we are at a leaf node, acquire the semaphore and cause this edge of the
                    // tree to ABORT.
                    if (req.getResponseSize() == 0) {
                      if (semaphore.tryAcquire(1)) {
                        Executors.newScheduledThreadPool(1).schedule(
                            new Runnable() {
                              @Override
                              public void run() {
                                call.close(Status.ABORTED, new Metadata());
                              }
                            }, 50, TimeUnit.MILLISECONDS);
                      }
                    } else {
                      // Decrement tree depth limit
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
                                  observedCancellations.incrementAndGet();
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
                    receivedCancellations.incrementAndGet();
                  }
                };
              }
            })
    ).build();
    server.start();
  }
}
