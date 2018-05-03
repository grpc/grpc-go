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

package io.grpc.examples.errorhandling;

import static com.google.common.util.concurrent.MoreExecutors.directExecutor;

import com.google.common.base.Verify;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.Uninterruptibles;
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.Metadata;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.Status;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.GreeterGrpc.GreeterBlockingStub;
import io.grpc.examples.helloworld.GreeterGrpc.GreeterFutureStub;
import io.grpc.examples.helloworld.GreeterGrpc.GreeterStub;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.stub.StreamObserver;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;

/**
 * Shows how to extract error information from a server response.
 */
public class ErrorHandlingClient {
  public static void main(String [] args) throws Exception {
    new ErrorHandlingClient().run();
  }

  private ManagedChannel channel;

  void run() throws Exception {
    // Port 0 means that the operating system will pick an available port to use.
    Server server = ServerBuilder.forPort(0).addService(new GreeterGrpc.GreeterImplBase() {
      @Override
      public void sayHello(HelloRequest request, StreamObserver<HelloReply> responseObserver) {
        responseObserver.onError(Status.INTERNAL
            .withDescription("Eggplant Xerxes Crybaby Overbite Narwhal").asRuntimeException());
      }
    }).build().start();
    channel =
        ManagedChannelBuilder.forAddress("localhost", server.getPort()).usePlaintext().build();

    blockingCall();
    futureCallDirect();
    futureCallCallback();
    asyncCall();
    advancedAsyncCall();

    channel.shutdown();
    server.shutdown();
    channel.awaitTermination(1, TimeUnit.SECONDS);
    server.awaitTermination();
  }

  void blockingCall() {
    GreeterBlockingStub stub = GreeterGrpc.newBlockingStub(channel);
    try {
      stub.sayHello(HelloRequest.newBuilder().setName("Bart").build());
    } catch (Exception e) {
      Status status = Status.fromThrowable(e);
      Verify.verify(status.getCode() == Status.Code.INTERNAL);
      Verify.verify(status.getDescription().contains("Eggplant"));
      // Cause is not transmitted over the wire.
    }
  }

  void futureCallDirect() {
    GreeterFutureStub stub = GreeterGrpc.newFutureStub(channel);
    ListenableFuture<HelloReply> response =
        stub.sayHello(HelloRequest.newBuilder().setName("Lisa").build());

    try {
      response.get();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new RuntimeException(e);
    } catch (ExecutionException e) {
      Status status = Status.fromThrowable(e.getCause());
      Verify.verify(status.getCode() == Status.Code.INTERNAL);
      Verify.verify(status.getDescription().contains("Xerxes"));
      // Cause is not transmitted over the wire.
    }
  }

  void futureCallCallback() {
    GreeterFutureStub stub = GreeterGrpc.newFutureStub(channel);
    ListenableFuture<HelloReply> response =
        stub.sayHello(HelloRequest.newBuilder().setName("Maggie").build());

    final CountDownLatch latch = new CountDownLatch(1);

    Futures.addCallback(
        response,
        new FutureCallback<HelloReply>() {
          @Override
          public void onSuccess(@Nullable HelloReply result) {
            // Won't be called, since the server in this example always fails.
          }

          @Override
          public void onFailure(Throwable t) {
            Status status = Status.fromThrowable(t);
            Verify.verify(status.getCode() == Status.Code.INTERNAL);
            Verify.verify(status.getDescription().contains("Crybaby"));
            // Cause is not transmitted over the wire..
            latch.countDown();
          }
        },
        directExecutor());

    if (!Uninterruptibles.awaitUninterruptibly(latch, 1, TimeUnit.SECONDS)) {
      throw new RuntimeException("timeout!");
    }
  }

  void asyncCall() {
    GreeterStub stub = GreeterGrpc.newStub(channel);
    HelloRequest request = HelloRequest.newBuilder().setName("Homer").build();
    final CountDownLatch latch = new CountDownLatch(1);
    StreamObserver<HelloReply> responseObserver = new StreamObserver<HelloReply>() {

      @Override
      public void onNext(HelloReply value) {
        // Won't be called.
      }

      @Override
      public void onError(Throwable t) {
        Status status = Status.fromThrowable(t);
        Verify.verify(status.getCode() == Status.Code.INTERNAL);
        Verify.verify(status.getDescription().contains("Overbite"));
        // Cause is not transmitted over the wire..
        latch.countDown();
      }

      @Override
      public void onCompleted() {
        // Won't be called, since the server in this example always fails.
      }
    };
    stub.sayHello(request, responseObserver);

    if (!Uninterruptibles.awaitUninterruptibly(latch, 1, TimeUnit.SECONDS)) {
      throw new RuntimeException("timeout!");
    }
  }


  /**
   * This is more advanced and does not make use of the stub.  You should not normally need to do
   * this, but here is how you would.
   */
  void advancedAsyncCall() {
    ClientCall<HelloRequest, HelloReply> call =
        channel.newCall(GreeterGrpc.getSayHelloMethod(), CallOptions.DEFAULT);

    final CountDownLatch latch = new CountDownLatch(1);

    call.start(new ClientCall.Listener<HelloReply>() {

      @Override
      public void onClose(Status status, Metadata trailers) {
        Verify.verify(status.getCode() == Status.Code.INTERNAL);
        Verify.verify(status.getDescription().contains("Narwhal"));
        // Cause is not transmitted over the wire.
        latch.countDown();
      }
    }, new Metadata());

    call.sendMessage(HelloRequest.newBuilder().setName("Marge").build());
    call.halfClose();

    if (!Uninterruptibles.awaitUninterruptibly(latch, 1, TimeUnit.SECONDS)) {
      throw new RuntimeException("timeout!");
    }
  }
}

