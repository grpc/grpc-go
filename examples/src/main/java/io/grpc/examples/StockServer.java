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

package io.grpc.examples;

import com.google.protos.io.grpc.examples.StockGrpc;
import com.google.protos.io.grpc.examples.StockOuterClass.StockReply;
import com.google.protos.io.grpc.examples.StockOuterClass.StockRequest;

import io.grpc.ServerImpl;
import io.grpc.stub.StreamObserver;
import io.grpc.transport.netty.NettyServerBuilder;

import java.util.Random;

/**
 * A sample GRPC server that implements the Stock service.
 */
public class StockServer implements StockGrpc.Stock {

  private final Random rand = new Random();

  private float getStockPrice(String symbol) {
    return rand.nextFloat() * 1000;
  }

  // Simple request-response RPC
  @Override
  public void getLastTradePrice(StockRequest request, StreamObserver<StockReply> responseObserver) {
    String symbol = request.getSymbol();
    float price = getStockPrice(symbol);
    responseObserver.onValue(
        StockReply.newBuilder().setSymbol(symbol).setPrice(price).build());
    responseObserver.onCompleted();
  }

  // Bi-directional streaming
  @Override
  public StreamObserver<StockRequest> getLastTradePriceMultiple(
      final StreamObserver<StockReply> responseObserver) {
    return new StreamObserver<StockRequest>() {
      @Override
      public void onValue(StockRequest request) {
        String symbol = request.getSymbol();
        float price = getStockPrice(symbol);
        responseObserver.onValue(
            StockReply.newBuilder().setSymbol(symbol).setPrice(price).build());
      }

      @Override
      public void onError(Throwable t) {
      }

      @Override
      public void onCompleted() {
        responseObserver.onCompleted();
      }
    };
  }

  // Server streaming
  @Override
  public void watchFutureTrades(StockRequest request, StreamObserver<StockReply> responseObserver) {
    String symbol = request.getSymbol();
    for (int i = 0; i < request.getNumTradesToWatch(); i++) {
      float price = getStockPrice(symbol);
      responseObserver.onValue(
          StockReply.newBuilder().setSymbol(symbol).setPrice(price).build());
    }
    responseObserver.onCompleted();
  }

  // Client streaming
  @Override
  public StreamObserver<StockRequest> getHighestTradePrice(
      final StreamObserver<StockReply> responseObserver) {
    return new StreamObserver<StockRequest>() {
      String highestSymbol;
      float highestPrice;

      @Override
      public void onValue(StockRequest request) {
        String symbol = request.getSymbol();
        float price = getStockPrice(symbol);
        if (highestSymbol == null || price > highestPrice) {
          highestPrice = price;
          highestSymbol = symbol;
        }
      }

      @Override
      public void onError(Throwable t) {
      }

      @Override
      public void onCompleted() {
        if (highestSymbol != null) {
          responseObserver.onValue(
              StockReply.newBuilder().setSymbol(highestSymbol).setPrice(highestPrice).build());
        }
        responseObserver.onCompleted();
      }
    };
  }

  public static void main(String[] args) throws Exception {
    ServerImpl server = NettyServerBuilder.forPort(8980)
        .addService(StockGrpc.bindService(new StockServer()))
        .build().start();
    System.out.println("Server started");
  }
}
