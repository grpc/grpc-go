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

package io.grpc.stub;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

import com.google.common.util.concurrent.ListenableFuture;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerServiceDefinition;
import io.grpc.ServiceDescriptor;
import io.grpc.Status;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.ManagedChannelImpl;
import io.grpc.stub.ServerCalls.NoopStreamObserver;
import io.grpc.stub.ServerCallsTest.IntegerMarshaller;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.util.Iterator;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Semaphore;
import java.util.concurrent.TimeUnit;

/**
 * Unit tests for {@link ClientCalls}.
 */
@RunWith(JUnit4.class)
public class ClientCallsTest {

  static final MethodDescriptor<Integer, Integer> STREAMING_METHOD = MethodDescriptor.create(
      MethodDescriptor.MethodType.BIDI_STREAMING,
      "some/method",
      new IntegerMarshaller(), new IntegerMarshaller());

  @Mock
  private ClientCall<Integer, String> call;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
  }

  @Test
  public void unaryFutureCallSuccess() throws Exception {
    Integer req = 2;
    ListenableFuture<String> future = ClientCalls.futureUnaryCall(call, req);
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    ClientCall.Listener<String> listener = listenerCaptor.getValue();
    verify(call).sendMessage(req);
    verify(call).halfClose();
    listener.onMessage("bar");
    listener.onClose(Status.OK, new Metadata());
    assertEquals("bar", future.get());
  }

  @Test
  public void unaryFutureCallFailed() throws Exception {
    Integer req = 2;
    ListenableFuture<String> future = ClientCalls.futureUnaryCall(call, req);
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    ClientCall.Listener<String> listener = listenerCaptor.getValue();
    Metadata trailers = new Metadata();
    listener.onClose(Status.INTERNAL, trailers);
    try {
      future.get();
      fail("Should fail");
    } catch (ExecutionException e) {
      Status status = Status.fromThrowable(e);
      assertEquals(Status.INTERNAL, status);
      Metadata metadata = Status.trailersFromThrowable(e);
      assertSame(trailers, metadata);
    }
  }

  @Test
  public void unaryFutureCallCancelled() throws Exception {
    Integer req = 2;
    ListenableFuture<String> future = ClientCalls.futureUnaryCall(call, req);
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    ClientCall.Listener<String> listener = listenerCaptor.getValue();
    future.cancel(true);
    verify(call).cancel("GrpcFuture was cancelled", null);
    listener.onMessage("bar");
    listener.onClose(Status.OK, new Metadata());
    try {
      future.get();
      fail("Should fail");
    } catch (CancellationException e) {
      // Exepcted
    }
  }

  @Test
  public void cannotSetOnReadyAfterCallStarted() throws Exception {
    CallStreamObserver<Integer> callStreamObserver =
        (CallStreamObserver<Integer>) ClientCalls.asyncClientStreamingCall(call,
            new NoopStreamObserver<String>());
    Runnable noOpRunnable = new Runnable() {
      @Override
      public void run() {
      }
    };
    try {
      callStreamObserver.setOnReadyHandler(noOpRunnable);
      fail("Should not be able to set handler after call started");
    } catch (IllegalStateException ise) {
      // expected
    }
  }

  @Test
  public void disablingInboundAutoFlowControlSuppressesRequestsForMoreMessages()
      throws Exception {
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    CallStreamObserver<Integer> requestObserver =
        (CallStreamObserver<Integer>)
            ClientCalls.asyncBidiStreamingCall(call, new ClientResponseObserver<Integer, String>() {
              @Override
              public void beforeStart(ClientCallStreamObserver<Integer> requestStream) {
                requestStream.disableAutoInboundFlowControl();
              }

              @Override
              public void onNext(String value) {

              }

              @Override
              public void onError(Throwable t) {

              }

              @Override
              public void onCompleted() {

              }
            });
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    listenerCaptor.getValue().onMessage("message");
    verify(call, times(1)).request(1);
  }

  @Test
  public void callStreamObserverPropagatesFlowControlRequestsToCall()
      throws Exception {
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    ClientResponseObserver<Integer, String> responseObserver =
        new ClientResponseObserver<Integer, String>() {
          @Override
          public void beforeStart(ClientCallStreamObserver<Integer> requestStream) {
            requestStream.disableAutoInboundFlowControl();
          }

          @Override
          public void onNext(String value) {
          }

          @Override
          public void onError(Throwable t) {
          }

          @Override
          public void onCompleted() {
          }
        };
    CallStreamObserver<Integer> requestObserver =
        (CallStreamObserver<Integer>)
            ClientCalls.asyncBidiStreamingCall(call, responseObserver);
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    listenerCaptor.getValue().onMessage("message");
    requestObserver.request(5);
    verify(call, times(1)).request(5);
  }

  @Test
  public void canCaptureInboundFlowControlForServerStreamingObserver()
      throws Exception {
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    ClientResponseObserver<Integer, String> responseObserver =
        new ClientResponseObserver<Integer, String>() {
      @Override
      public void beforeStart(ClientCallStreamObserver<Integer> requestStream) {
        requestStream.disableAutoInboundFlowControl();
        requestStream.request(5);
      }

      @Override
      public void onNext(String value) {
      }

      @Override
      public void onError(Throwable t) {
      }

      @Override
      public void onCompleted() {
      }
    };
    ClientCalls.asyncServerStreamingCall(call, 1, responseObserver);
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    listenerCaptor.getValue().onMessage("message");
    verify(call, times(1)).request(1);
    verify(call, times(1)).request(5);
  }

  @Test
  public void inprocessTransportInboundFlowControl() throws Exception {
    final Semaphore semaphore = new Semaphore(1);
    ServerServiceDefinition service = ServerServiceDefinition.builder(
        new ServiceDescriptor("some", STREAMING_METHOD))
        .addMethod(STREAMING_METHOD, ServerCalls.asyncBidiStreamingCall(
            new ServerCalls.BidiStreamingMethod<Integer, Integer>() {
              int iteration;

              @Override
              public StreamObserver<Integer> invoke(StreamObserver<Integer> responseObserver) {
                final ServerCallStreamObserver<Integer> serverCallObserver =
                    (ServerCallStreamObserver<Integer>) responseObserver;
                serverCallObserver.setOnReadyHandler(new Runnable() {
                  @Override
                  public void run() {
                    while (serverCallObserver.isReady()) {
                      serverCallObserver.onNext(iteration);
                    }
                    iteration++;
                    semaphore.release();
                  }
                });
                return new ServerCalls.NoopStreamObserver<Integer>() {
                  @Override
                  public void onCompleted() {
                    serverCallObserver.onCompleted();
                  }
                };
              }
            }))
        .build();
    long tag = System.nanoTime();
    InProcessServerBuilder.forName("go-with-the-flow" + tag).addService(service).build().start();
    ManagedChannelImpl channel = InProcessChannelBuilder.forName("go-with-the-flow" + tag).build();
    final ClientCall<Integer, Integer> clientCall = channel.newCall(STREAMING_METHOD,
        CallOptions.DEFAULT);
    final CountDownLatch latch = new CountDownLatch(1);
    final int[] receivedMessages = new int[6];
    semaphore.acquire();

    ClientResponseObserver<Integer, Integer> responseObserver =
        new ClientResponseObserver<Integer, Integer>() {
          int index;
          @Override
          public void beforeStart(final ClientCallStreamObserver<Integer> requestStream) {
            requestStream.disableAutoInboundFlowControl();
          }

          @Override
          public void onNext(Integer value) {
            receivedMessages[index++] = value;
          }

          @Override
          public void onError(Throwable t) {
            latch.countDown();
          }

          @Override
          public void onCompleted() {
            latch.countDown();
          }
        };

    CallStreamObserver<Integer> integerStreamObserver = (CallStreamObserver<Integer>)
        ClientCalls.asyncBidiStreamingCall(clientCall, responseObserver);
    semaphore.acquire();
    integerStreamObserver.request(2);
    semaphore.acquire();
    integerStreamObserver.request(3);
    integerStreamObserver.onCompleted();
    latch.await(5, TimeUnit.SECONDS);
    // Very that number of messages produced in each onReady handler call matches the number
    // requested by the client. Note that ClientCalls.asyncBidiStreamingCall will request(1)

    assertArrayEquals(new int[]{0, 1, 1, 2, 2, 2}, receivedMessages);
  }

  @Test
  public void inprocessTransportOutboundFlowControl() throws Exception {
    final CountDownLatch latch = new CountDownLatch(1);
    final Semaphore semaphore = new Semaphore(1);
    final int[] receivedMessages = new int[6];
    ServerServiceDefinition service = ServerServiceDefinition.builder(
        new ServiceDescriptor("some", STREAMING_METHOD))
        .addMethod(STREAMING_METHOD, ServerCalls.asyncBidiStreamingCall(
            new ServerCalls.BidiStreamingMethod<Integer, Integer>() {
              @Override
              public StreamObserver<Integer> invoke(StreamObserver<Integer> responseObserver) {
                final ServerCallStreamObserver<Integer> serverCallObserver =
                    (ServerCallStreamObserver<Integer>) responseObserver;
                serverCallObserver.disableAutoInboundFlowControl();
                new Thread(new Runnable() {
                  @Override
                  public void run() {
                    try {
                      serverCallObserver.request(1);
                      semaphore.acquire();
                      serverCallObserver.request(2);
                      semaphore.acquire();
                      serverCallObserver.request(3);
                    } catch (Throwable t) {
                      throw new RuntimeException(t);
                    }
                  }
                }).start();
                return new ServerCalls.NoopStreamObserver<Integer>() {
                  int index;
                  @Override
                  public void onNext(Integer value) {
                    receivedMessages[index++] = value;
                  }

                  @Override
                  public void onCompleted() {
                    serverCallObserver.onCompleted();
                    latch.countDown();
                  }
                };
              }
            }))
        .build();
    long tag = System.nanoTime();
    InProcessServerBuilder.forName("go-with-the-flow" + tag).addService(service).build().start();
    ManagedChannelImpl channel = InProcessChannelBuilder.forName("go-with-the-flow" + tag).build();
    final ClientCall<Integer, Integer> clientCall = channel.newCall(STREAMING_METHOD,
        CallOptions.DEFAULT);
    semaphore.acquire();

    ClientResponseObserver<Integer, Integer> responseObserver =
        new ClientResponseObserver<Integer, Integer>() {
          @Override
          public void beforeStart(final ClientCallStreamObserver<Integer> requestStream) {
            requestStream.setOnReadyHandler(new Runnable() {
              int iteration;
              @Override
              public void run() {
                while (requestStream.isReady()) {
                  requestStream.onNext(iteration);
                }
                iteration++;
                if (iteration == 3) {
                  requestStream.onCompleted();
                }
                semaphore.release();
              }
            });
          }

          @Override
          public void onNext(Integer value) {
          }

          @Override
          public void onError(Throwable t) {
          }

          @Override
          public void onCompleted() {
          }
        };

    ClientCalls.asyncBidiStreamingCall(clientCall, responseObserver);
    latch.await(5, TimeUnit.SECONDS);
    // Very that number of messages produced in each onReady handler call matches the number
    // requested by the client.
    assertArrayEquals(new int[]{0, 1, 1, 2, 2, 2}, receivedMessages);
  }

  @Test
  public void blockingResponseStreamFailed() throws Exception {
    Integer req = 2;
    Iterator<String> iter = ClientCalls.blockingServerStreamingCall(call, req);
    ArgumentCaptor<ClientCall.Listener<String>> listenerCaptor = ArgumentCaptor.forClass(null);
    verify(call).start(listenerCaptor.capture(), any(Metadata.class));
    ClientCall.Listener<String> listener = listenerCaptor.getValue();
    Metadata trailers = new Metadata();
    listener.onClose(Status.INTERNAL, trailers);
    try {
      iter.next();
      fail("Should fail");
    } catch (Throwable e) {
      Status status = Status.fromThrowable(e);
      assertEquals(Status.INTERNAL, status);
      Metadata metadata = Status.trailersFromThrowable(e);
      assertSame(trailers, metadata);
    }
  }
}
