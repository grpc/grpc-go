package com.google.net.stubby.examples;

import com.google.net.stubby.ServerImpl;
import com.google.net.stubby.stub.StreamObserver;
import com.google.net.stubby.transport.netty.NettyServerBuilder;
import com.google.protos.net.stubby.examples.StockGrpc;
import com.google.protos.net.stubby.examples.StockOuterClass.StockReply;
import com.google.protos.net.stubby.examples.StockOuterClass.StockRequest;

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
        .buildAndWaitForRunning();
    System.out.println("Server started");
    server.awaitTerminated();
  }
}
