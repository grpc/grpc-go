/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.stub;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Server;
import io.grpc.ServerServiceDefinition;
import io.grpc.ServiceDescriptor;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.NoopClientCall;
import io.grpc.stub.ServerCalls.NoopStreamObserver;
import io.grpc.stub.ServerCallsTest.IntegerMarshaller;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Iterator;
import java.util.List;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Semaphore;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link ClientCalls}.
 */
@RunWith(JUnit4.class)
public class ClientCallsTest {

  private static final MethodDescriptor<Integer, Integer> STREAMING_METHOD =
      MethodDescriptor.<Integer, Integer>newBuilder()
          .setType(MethodDescriptor.MethodType.BIDI_STREAMING)
          .setFullMethodName("some/method")
          .setRequestMarshaller(new IntegerMarshaller())
          .setResponseMarshaller(new IntegerMarshaller())
          .build();

  private Server server;
  private ManagedChannel channel;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
  }

  @After
  public void tearDown() {
    if (server != null) {
      server.shutdownNow();
    }
    if (channel != null) {
      channel.shutdownNow();
    }
  }

  @Test
  public void unaryBlockingCallSuccess() throws Exception {
    Integer req = 2;
    final String resp = "bar";
    final Status status = Status.OK;
    final Metadata trailers = new Metadata();

    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>() {
      @Override
      public void start(ClientCall.Listener<String> listener, Metadata headers) {
        listener.onMessage(resp);
        listener.onClose(status, trailers);
      }
    };

    String actualResponse = ClientCalls.blockingUnaryCall(call, req);
    assertEquals(resp, actualResponse);
  }

  @Test
  public void unaryBlockingCallFailed() throws Exception {
    Integer req = 2;
    final Status status = Status.INTERNAL.withDescription("Unique status");
    final Metadata trailers = new Metadata();

    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>() {
      @Override
      public void start(io.grpc.ClientCall.Listener<String> listener, Metadata headers) {
        listener.onClose(status, trailers);
      }
    };

    try {
      ClientCalls.blockingUnaryCall(call, req);
      fail("Should fail");
    } catch (StatusRuntimeException e) {
      assertSame(status, e.getStatus());
      assertSame(trailers, e.getTrailers());
    }
  }

  @Test
  public void unaryFutureCallSuccess() throws Exception {
    final AtomicReference<ClientCall.Listener<String>> listener =
        new AtomicReference<ClientCall.Listener<String>>();
    final AtomicReference<Integer> message = new AtomicReference<Integer>();
    final AtomicReference<Boolean> halfClosed = new AtomicReference<Boolean>();
    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>() {
      @Override
      public void start(io.grpc.ClientCall.Listener<String> responseListener, Metadata headers) {
        listener.set(responseListener);
      }

      @Override
      public void sendMessage(Integer msg) {
        message.set(msg);
      }

      @Override
      public void halfClose() {
        halfClosed.set(true);
      }
    };
    Integer req = 2;
    ListenableFuture<String> future = ClientCalls.futureUnaryCall(call, req);

    assertEquals(req, message.get());
    assertTrue(halfClosed.get());
    listener.get().onMessage("bar");
    listener.get().onClose(Status.OK, new Metadata());
    assertEquals("bar", future.get());
  }

  @Test
  public void unaryFutureCallFailed() throws Exception {
    final AtomicReference<ClientCall.Listener<String>> listener =
        new AtomicReference<ClientCall.Listener<String>>();
    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>() {
      @Override
      public void start(io.grpc.ClientCall.Listener<String> responseListener, Metadata headers) {
        listener.set(responseListener);
      }
    };
    Integer req = 2;
    ListenableFuture<String> future = ClientCalls.futureUnaryCall(call, req);
    Metadata trailers = new Metadata();
    listener.get().onClose(Status.INTERNAL, trailers);
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
    final AtomicReference<ClientCall.Listener<String>> listener =
        new AtomicReference<ClientCall.Listener<String>>();
    final AtomicReference<String> cancelMessage = new AtomicReference<String>();
    final AtomicReference<Throwable> cancelCause = new AtomicReference<Throwable>();
    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>() {
      @Override
      public void start(io.grpc.ClientCall.Listener<String> responseListener, Metadata headers) {
        listener.set(responseListener);
      }

      @Override
      public void cancel(String message, Throwable cause) {
        cancelMessage.set(message);
        cancelCause.set(cause);
      }
    };
    Integer req = 2;
    ListenableFuture<String> future = ClientCalls.futureUnaryCall(call, req);
    future.cancel(true);
    assertEquals("GrpcFuture was cancelled", cancelMessage.get());
    assertNull(cancelCause.get());
    listener.get().onMessage("bar");
    listener.get().onClose(Status.OK, new Metadata());
    try {
      future.get();
      fail("Should fail");
    } catch (CancellationException e) {
      // Exepcted
    }
  }

  @Test
  public void cannotSetOnReadyAfterCallStarted() throws Exception {
    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>();
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
    final AtomicReference<ClientCall.Listener<String>> listener =
        new AtomicReference<ClientCall.Listener<String>>();
    final List<Integer> requests = new ArrayList<>();
    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>() {
      @Override
      public void start(io.grpc.ClientCall.Listener<String> responseListener, Metadata headers) {
        listener.set(responseListener);
      }

      @Override
      public void request(int numMessages) {
        requests.add(numMessages);
      }
    };
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
    listener.get().onMessage("message");
    assertThat(requests).containsExactly(1);
  }

  @Test
  public void callStreamObserverPropagatesFlowControlRequestsToCall()
      throws Exception {
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
    final AtomicReference<ClientCall.Listener<String>> listener =
        new AtomicReference<ClientCall.Listener<String>>();
    final List<Integer> requests = new ArrayList<>();
    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>() {
      @Override
      public void start(io.grpc.ClientCall.Listener<String> responseListener, Metadata headers) {
        listener.set(responseListener);
      }

      @Override
      public void request(int numMessages) {
        requests.add(numMessages);
      }
    };
    CallStreamObserver<Integer> requestObserver =
        (CallStreamObserver<Integer>)
            ClientCalls.asyncBidiStreamingCall(call, responseObserver);
    listener.get().onMessage("message");
    requestObserver.request(5);
    assertThat(requests).contains(5);
  }

  @Test
  public void canCaptureInboundFlowControlForServerStreamingObserver()
      throws Exception {

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
    final AtomicReference<ClientCall.Listener<String>> listener =
            new AtomicReference<ClientCall.Listener<String>>();
    final List<Integer> requests = new ArrayList<>();
    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>() {
      @Override
      public void start(io.grpc.ClientCall.Listener<String> responseListener, Metadata headers) {
        listener.set(responseListener);
      }

      @Override
      public void request(int numMessages) {
        requests.add(numMessages);
      }
    };
    ClientCalls.asyncServerStreamingCall(call, 1, responseObserver);
    listener.get().onMessage("message");
    assertThat(requests).containsExactly(5, 1).inOrder();
  }

  @Test
  public void inprocessTransportInboundFlowControl() throws Exception {
    final Semaphore semaphore = new Semaphore(0);
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
    server = InProcessServerBuilder.forName("go-with-the-flow" + tag).directExecutor()
        .addService(service).build().start();
    channel = InProcessChannelBuilder.forName("go-with-the-flow" + tag).directExecutor().build();
    final ClientCall<Integer, Integer> clientCall = channel.newCall(STREAMING_METHOD,
        CallOptions.DEFAULT);
    final CountDownLatch latch = new CountDownLatch(1);
    final List<Object> receivedMessages = new ArrayList<>(6);

    ClientResponseObserver<Integer, Integer> responseObserver =
        new ClientResponseObserver<Integer, Integer>() {
          @Override
          public void beforeStart(final ClientCallStreamObserver<Integer> requestStream) {
            requestStream.disableAutoInboundFlowControl();
          }

          @Override
          public void onNext(Integer value) {
            receivedMessages.add(value);
          }

          @Override
          public void onError(Throwable t) {
            receivedMessages.add(t);
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
    assertTrue(latch.await(5, TimeUnit.SECONDS));
    // Verify that number of messages produced in each onReady handler call matches the number
    // requested by the client. Note that ClientCalls.asyncBidiStreamingCall will request(1)
    assertEquals(Arrays.asList(0, 1, 1, 2, 2, 2), receivedMessages);
  }

  @Test
  public void inprocessTransportOutboundFlowControl() throws Exception {
    final Semaphore semaphore = new Semaphore(0);
    final List<Object> receivedMessages = new ArrayList<>(6);
    final SettableFuture<ServerCallStreamObserver<Integer>> observerFuture
        = SettableFuture.create();
    ServerServiceDefinition service = ServerServiceDefinition.builder(
        new ServiceDescriptor("some", STREAMING_METHOD))
        .addMethod(STREAMING_METHOD, ServerCalls.asyncBidiStreamingCall(
            new ServerCalls.BidiStreamingMethod<Integer, Integer>() {
              @Override
              public StreamObserver<Integer> invoke(StreamObserver<Integer> responseObserver) {
                final ServerCallStreamObserver<Integer> serverCallObserver =
                    (ServerCallStreamObserver<Integer>) responseObserver;
                serverCallObserver.disableAutoInboundFlowControl();
                observerFuture.set(serverCallObserver);
                return new StreamObserver<Integer>() {
                  @Override
                  public void onNext(Integer value) {
                    receivedMessages.add(value);
                  }

                  @Override
                  public void onError(Throwable t) {
                    receivedMessages.add(t);
                  }

                  @Override
                  public void onCompleted() {
                    serverCallObserver.onCompleted();
                  }
                };
              }
            }))
        .build();
    long tag = System.nanoTime();
    server = InProcessServerBuilder.forName("go-with-the-flow" + tag).directExecutor()
        .addService(service).build().start();
    channel = InProcessChannelBuilder.forName("go-with-the-flow" + tag).directExecutor().build();
    final ClientCall<Integer, Integer> clientCall = channel.newCall(STREAMING_METHOD,
        CallOptions.DEFAULT);

    final SettableFuture<Void> future = SettableFuture.create();
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
            future.setException(t);
          }

          @Override
          public void onCompleted() {
            future.set(null);
          }
        };

    ClientCalls.asyncBidiStreamingCall(clientCall, responseObserver);
    ServerCallStreamObserver<Integer> serverCallObserver = observerFuture.get(5, TimeUnit.SECONDS);
    serverCallObserver.request(1);
    assertTrue(semaphore.tryAcquire(5, TimeUnit.SECONDS));
    serverCallObserver.request(2);
    assertTrue(semaphore.tryAcquire(5, TimeUnit.SECONDS));
    serverCallObserver.request(3);
    future.get(5, TimeUnit.SECONDS);
    // Verify that number of messages produced in each onReady handler call matches the number
    // requested by the client.
    assertEquals(Arrays.asList(0, 1, 1, 2, 2, 2), receivedMessages);
  }

  @Test
  public void blockingResponseStreamFailed() throws Exception {
    final AtomicReference<ClientCall.Listener<String>> listener =
        new AtomicReference<ClientCall.Listener<String>>();
    NoopClientCall<Integer, String> call = new NoopClientCall<Integer, String>() {
      @Override
      public void start(io.grpc.ClientCall.Listener<String> responseListener, Metadata headers) {
        listener.set(responseListener);
      }
    };

    Integer req = 2;
    Iterator<String> iter = ClientCalls.blockingServerStreamingCall(call, req);

    Metadata trailers = new Metadata();
    listener.get().onClose(Status.INTERNAL, trailers);
    try {
      iter.next();
      fail("Should fail");
    } catch (Exception e) {
      Status status = Status.fromThrowable(e);
      assertEquals(Status.INTERNAL, status);
      Metadata metadata = Status.trailersFromThrowable(e);
      assertSame(trailers, metadata);
    }
  }
}
