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

package com.google.net.stubby.examples;

import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.ChannelImpl;
import com.google.net.stubby.stub.StreamObserver;
import com.google.net.stubby.transport.netty.NegotiationType;
import com.google.net.stubby.transport.netty.NettyChannelBuilder;
import com.google.protos.net.stubby.examples.CalcGrpc;
import com.google.protos.net.stubby.examples.CalcGrpc.CalcBlockingStub;
import com.google.protos.net.stubby.examples.CalcGrpc.CalcStub;
import com.google.protos.net.stubby.examples.Math.DivArgs;
import com.google.protos.net.stubby.examples.Math.DivReply;
import com.google.protos.net.stubby.examples.Math.FibArgs;
import com.google.protos.net.stubby.examples.Math.Num;

import java.util.Iterator;
import java.util.Random;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Sample client code that makes gRPC calls to the server.
 */
public class MathClient {
  private final Logger logger = Logger.getLogger(MathClient.class.getName());
  private final Random rand = new Random();
  private final ChannelImpl channel;
  private final CalcBlockingStub blockingStub;
  private final CalcStub asyncStub;

  public MathClient(String host, int port) {
    channel = NettyChannelBuilder.forAddress(host, port)
        .negotiationType(NegotiationType.PLAINTEXT)
        .build();
    blockingStub = CalcGrpc.newBlockingStub(channel);
    asyncStub = CalcGrpc.newStub(channel);
  }

  public void shutdown() throws InterruptedException {
    channel.shutdown().awaitTerminated(5, TimeUnit.SECONDS);
  }

  /**
   * This example shows how to make a blocking unary call.
   */
  public void blockingDiv(long dividend, long divisor) {
    logger.info("*** Blocking Div: Dividend=" + dividend + ", Divisor=" + divisor);
    DivReply reply;
    try {
      reply = blockingStub.div(
          DivArgs.newBuilder().setDividend(dividend).setDivisor(divisor).build());
    } catch (RuntimeException e) {
      logger.log(Level.WARNING, "RPC failed", e);
      return;
    }
    logger.info("Result: Quotient=" + reply.getQuotient() + ", remainder=" + reply.getRemainder());
  }

  /**
   * This example shows how to make an asynchronous unary call.
   */
  public void asyncDiv(long dividend, long divisor) {
    logger.info("*** Async Div: Dividend=" + dividend + ", Divisor=" + divisor);
    DivArgs request = DivArgs.newBuilder().setDividend(dividend).setDivisor(divisor).build();
    final SettableFuture<DivReply> responseFuture = SettableFuture.create();
    asyncStub.div(request, new StreamObserver<DivReply> (){
      DivReply reply;
      @Override
      public void onValue(DivReply value) {
        reply = value;
      }

      @Override
      public void onError(Throwable t) {
        responseFuture.setException(t);
      }

      @Override
      public void onCompleted() {
        responseFuture.set(reply);
      }
    });

    logger.info("Waiting for result...");
    try {
      DivReply reply = responseFuture.get();
      logger.info(
          "Result: Quotient=" + reply.getQuotient() + ", remainder=" + reply.getRemainder());
    } catch (InterruptedException e) {
      logger.log(Level.WARNING, "Interrupted while waiting for result...", e);
    } catch (ExecutionException e) {
      logger.log(Level.WARNING, "RPC failed", e);
    }
  }

  /**
   * This example shows how to make a bi-directional streaming call, which can only be asynchronous.
   */
  public void divMany() {
    logger.info("*** DivMany");
    final SettableFuture<Void> finishFuture = SettableFuture.create();
    StreamObserver<DivArgs> requestSink = asyncStub.divMany(new StreamObserver<DivReply>() {
      int count = 0;
      @Override
      public void onValue(DivReply value) {
        count++;
        logger.info("Result " + count + ": Quotient=" + value.getQuotient() + ", remainder="
            + value.getRemainder());
      }

      @Override
      public void onError(Throwable t) {
        finishFuture.setException(t);
      }

      @Override
      public void onCompleted() {
        finishFuture.set(null);
      }
    });

    int count = 5;
    for (int i = 0; i < count; i++) {
      long dividend = rand.nextLong();
      long divisor = rand.nextInt() + 1; // plus 1 to avoid 0 divisor.
      logger.info("Request " + (i + 1) + ": Dividend=" + dividend + ", Divisor=%d" + divisor);
      requestSink.onValue(
          DivArgs.newBuilder().setDividend(dividend).setDivisor(divisor).build());
    }
    requestSink.onCompleted();

    try {
      finishFuture.get();
      logger.info("Finished " + count + " div requests");
    } catch (InterruptedException e) {
      logger.log(Level.WARNING, "Interrupted while waiting for result...", e);
    } catch (ExecutionException e) {
      logger.log(Level.WARNING, "RPC failed", e);
    }
  }

  /**
   * This example shows how to make a client streaming call.
   */
  public void sum() throws InterruptedException {
    final CountDownLatch completed = new CountDownLatch(1);
    logger.info("*** Sum");
    int count = 5;
    StreamObserver<Num> responseObserver = new StreamObserver<Num>() {
      @Override
      public void onValue(Num value) {
        logger.info("Sum=" + value);
      }

      @Override
      public void onError(Throwable t) {
        logger.log(Level.SEVERE, "Error receiving response", t);
      }

      @Override
      public void onCompleted() {
        completed.countDown();
      }
    };
    StreamObserver<Num> requestObserver = asyncStub.sum(responseObserver);
    StringBuilder numMsg = new StringBuilder();
    for (int i = 0; i < count; i++) {
      int value = rand.nextInt();
      requestObserver.onValue(Num.newBuilder().setNum(value).build());
      numMsg.append(value);
      if (i != count - 1) {
        numMsg.append(" + ");
      }
    }
    logger.info(numMsg.toString());
    requestObserver.onCompleted();
    completed.await();
  }

  /**
   * This example shows how to make a blocking server streaming call.
   *
   * <p> The asynchronous usage is similar to {@link #divMany}.
   */
  public void blockingFib() {
    // TODO(user): Support "send until cancel". Currently, client application can not
    // cancel a server streaming call.
    int limit = rand.nextInt(20) + 10;
    logger.info("*** Blocking Fib, print the first " + limit + " fibonacci numbers.");
    Iterator<Num> iterator = blockingStub.fib(FibArgs.newBuilder().setLimit(limit).build());
    StringBuilder resultMsg = new StringBuilder();
    int realCount = 0;
    while (iterator.hasNext()) {
      realCount++;
      resultMsg.append(iterator.next().getNum());
      resultMsg.append(", ");
    }
    logger.info("The first " + realCount + " fibonacci numbers: " + resultMsg.toString());
  }

  public static void main(String[] args) throws InterruptedException {
    MathClient client = new MathClient("localhost", 8980);
    try {
      client.blockingDiv(2014, 4);
      // Note: This call should be failed, but we can not get the server specified error info
      // currently, so the error message logged is not clear.
      client.blockingDiv(73, 0);
      client.asyncDiv(1986, 12);
      client.divMany();
      client.sum();
      client.blockingFib();
    } finally {
      client.shutdown();
    }
  }
}
