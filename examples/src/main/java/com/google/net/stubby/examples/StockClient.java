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

import com.google.net.stubby.ChannelImpl;
import com.google.net.stubby.stub.StreamObserver;
import com.google.net.stubby.transport.netty.NegotiationType;
import com.google.net.stubby.transport.netty.NettyChannelBuilder;
import com.google.protos.net.stubby.examples.StockGrpc;
import com.google.protos.net.stubby.examples.StockGrpc.StockBlockingStub;
import com.google.protos.net.stubby.examples.StockGrpc.StockStub;
import com.google.protos.net.stubby.examples.StockOuterClass.StockReply;
import com.google.protos.net.stubby.examples.StockOuterClass.StockRequest;

import java.util.Iterator;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * Sample client code that makes GRPC calls to the server.
 */
public class StockClient {

  private final ChannelImpl channel;

  public StockClient() throws Exception {
    channel = NettyChannelBuilder.forAddress("localhost", 8980)
        .negotiationType(NegotiationType.PLAINTEXT)
        .buildAndWaitForRunning(5, TimeUnit.SECONDS);
  }

  public void shutdown() throws Exception {
    channel.stopAsync().awaitTerminated(5, TimeUnit.SECONDS);
  }

  public void makeBlockingSimpleCall() {
    StockBlockingStub stub = StockGrpc.newBlockingStub(channel)
        .configureNewStub().setTimeout(2, TimeUnit.SECONDS).build();
    StockRequest request = StockRequest.newBuilder().setSymbol("GOOG").build();
    System.out.println("***Blocking simple call, request=" + request);
    StockReply reply = stub.getLastTradePrice(request);
    System.out.println("response=" + reply);
  }

  public void makeAsyncSimpleCall() {
    StockStub stub = StockGrpc.newStub(channel)
        .configureNewStub().setTimeout(2, TimeUnit.SECONDS).build();
    StockRequest request = StockRequest.newBuilder().setSymbol("MSFT").build();
    System.out.println("***Async simple call, request=" + request);
    stub.getLastTradePrice(request, new StreamObserver<StockReply>() {
      StockReply response;
      @Override
      public void onValue(StockReply response) {
        this.response = response;
      }

      @Override
      public void onError(Throwable t) {
        t.printStackTrace();
      }

      @Override
      public void onCompleted() {
        System.out.println("Completed, response=" + response);
      }
    });
  }

  public void makeSequentialCalls() {
    StockBlockingStub stub = StockGrpc.newBlockingStub(channel)
        .configureNewStub().setTimeout(2, TimeUnit.SECONDS).build();
    System.out.println("***Making sequential calls");
    StockRequest request1 = StockRequest.newBuilder().setSymbol("AMZN").build();
    System.out.println("First request=" + request1);
    StockReply response1 = stub.getLastTradePrice(request1);
    System.out.println("First response=" + response1);
    StockRequest request2 = StockRequest.newBuilder().setSymbol(response1.getSymbol()).build();
    System.out.println("Second request=" + request2);
    StockReply response2 = stub.getLastTradePrice(request2);
    System.out.println("Second response=" + response2);
  }

  public void makeAsyncCalls() throws Exception {
    StockStub stub = StockGrpc.newStub(channel)
        .configureNewStub().setTimeout(2, TimeUnit.SECONDS).build();
    System.out.println("***Making two calls in parallel");
    final StockRequest request1 = StockRequest.newBuilder().setSymbol("IBM").build();
    final StockRequest request2 = StockRequest.newBuilder().setSymbol("APPL").build();
    final CountDownLatch completeLatch = new CountDownLatch(2);
    stub.getLastTradePrice(request1, new StreamObserver<StockReply>() {
      StockReply response;
      @Override
      public void onValue(StockReply response) {
        this.response = response;
      }

      @Override
      public void onError(Throwable t) {
        t.printStackTrace();
      }

      @Override
      public void onCompleted() {
        System.out.println("Completed for first request=" + request1 + ", response=" + response);
        completeLatch.countDown();
      }
    });
    stub.getLastTradePrice(request2, new StreamObserver<StockReply>() {
      StockReply response;
      @Override
      public void onValue(StockReply response) {
        this.response = response;
      }

      @Override
      public void onError(Throwable t) {
        t.printStackTrace();
      }

      @Override
      public void onCompleted() {
        System.out.println("Completed for second request=" + request2 + ", response=" + response);
        completeLatch.countDown();
      }
    });
    completeLatch.await();
  }

  public void makeServerStreamingCall() throws Exception {
    StockBlockingStub stub = StockGrpc.newBlockingStub(channel)
        .configureNewStub().setTimeout(2, TimeUnit.SECONDS).build();
    StockRequest request = StockRequest.newBuilder().setSymbol("FB").setNumTradesToWatch(5).build();
    System.out.println("***Making a server streaming call, request=" + request);
    for (Iterator<StockReply> responses = stub.watchFutureTrades(request); responses.hasNext(); ) {
      StockReply response = responses.next();
      System.out.println("Response=" + response);
    }
    System.out.println("Completed");
  }

  public void makeClientStreamingCall() throws Exception {
    StockStub stub = StockGrpc.newStub(channel)
        .configureNewStub().setTimeout(2, TimeUnit.SECONDS).build();
    System.out.println("***Making a client streaming call");
    final CountDownLatch completeLatch = new CountDownLatch(1);
    StreamObserver<StockRequest> requestSink = stub.getHighestTradePrice(
        new StreamObserver<StockReply>() {
          StockReply response;
          @Override
          public void onValue(StockReply response) {
            this.response = response;
          }

          @Override
          public void onError(Throwable t) {
            t.printStackTrace();
          }

          @Override
          public void onCompleted() {
            System.out.println("Completed. response=" + response);
            completeLatch.countDown();
          }
        });
    for (String symbol : new String[] {"ORCL", "TWTR", "QQQ", "SIRI", "ZNGA", "RAD"}) {
      StockRequest request = StockRequest.newBuilder().setSymbol(symbol).build();
      System.out.println("request=" + request);
      requestSink.onValue(request);
    }
    requestSink.onCompleted();
    completeLatch.await();
  }

  public static void main(String[] args) throws Exception {
    StockClient client = new StockClient();
    try {
      client.makeBlockingSimpleCall();
      client.makeAsyncSimpleCall();
      client.makeSequentialCalls();
      client.makeAsyncCalls();
      client.makeServerStreamingCall();
      client.makeClientStreamingCall();
      System.out.println("***All done");
    } finally {
      client.shutdown();
    }
  }
}
