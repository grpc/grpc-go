package com.google.net.stubby.examples;

import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.ChannelImpl;
import com.google.net.stubby.stub.StreamObserver;
import com.google.net.stubby.transport.netty.NettyChannelBuilder;
import com.google.net.stubby.transport.netty.NettyClientTransportFactory.NegotiationType;
import com.google.protos.net.stubby.examples.CalcGrpc;
import com.google.protos.net.stubby.examples.CalcGrpc.CalcBlockingStub;
import com.google.protos.net.stubby.examples.CalcGrpc.CalcStub;
import com.google.protos.net.stubby.examples.Math.DivArgs;
import com.google.protos.net.stubby.examples.Math.DivReply;
import com.google.protos.net.stubby.examples.Math.FibArgs;
import com.google.protos.net.stubby.examples.Math.Num;

import java.util.HashSet;
import java.util.Iterator;
import java.util.Random;
import java.util.Set;
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

  public MathClient(String host, int port) throws Exception {
    channel = NettyChannelBuilder.forAddress(host, port)
        .negotiationType(NegotiationType.PLAINTEXT)
        .buildAndWaitForRunning(5, TimeUnit.SECONDS);
    blockingStub = CalcGrpc.newBlockingStub(channel);
    asyncStub = CalcGrpc.newStub(channel);
  }

  public void shutdown() throws Exception {
    channel.stopAsync().awaitTerminated(5, TimeUnit.SECONDS);
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
   * This example shows how to make a blocking client streaming call.
   *
   * <p> The asynchronous usage is similar to {@link #divMany}.
   */
  public void blockingSum() {
    logger.info("*** Blocking Sum");
    int count = 5;
    Set<Num> numSet = new HashSet<Num>();
    StringBuilder numMsg = new StringBuilder();
    for (int i = 0; i < count; i++) {
      int value = rand.nextInt();
      numSet.add(Num.newBuilder().setNum(value).build());
      numMsg.append(value);
      if (i != count - 1) {
        numMsg.append(" + ");
      }
    }
    logger.info(numMsg.toString());
    Num reply = blockingStub.sum(numSet.iterator());
    logger.info("Result: " + numMsg.toString() + " = " + reply.getNum());
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

  public static void main(String[] args) throws Exception {
    MathClient client = new MathClient("localhost", 8980);
    try {
      client.blockingDiv(2014, 4);
      // Note: This call should be failed, but we can not get the server specified error info
      // currently, so the error message logged is not clear.
      client.blockingDiv(73, 0);
      client.asyncDiv(1986, 12);
      client.divMany();
      client.blockingSum();
      client.blockingFib();
    } finally {
      client.shutdown();
    }
  }
}
