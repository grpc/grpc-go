package com.google.net.stubby.examples;

import com.google.net.stubby.ServerImpl;
import com.google.net.stubby.stub.StreamObserver;
import com.google.net.stubby.transport.netty.NettyServerBuilder;
import com.google.protos.net.stubby.examples.CalcGrpc;
import com.google.protos.net.stubby.examples.Math.DivArgs;
import com.google.protos.net.stubby.examples.Math.DivReply;
import com.google.protos.net.stubby.examples.Math.FibArgs;
import com.google.protos.net.stubby.examples.Math.Num;

import java.util.logging.Level;
import java.util.logging.Logger;


/**
 * A sample gRPC server that serve the Calc (see math.proto) service.
 */
public class MathServer {
  private final Logger logger = Logger.getLogger(MathServer.class.getName());
  private final int port;
  private ServerImpl gRpcServer;

  public MathServer(int port) {
    this.port = port;
  }

  public void start() {
    gRpcServer = NettyServerBuilder.forPort(port)
        .addService(CalcGrpc.bindService(new CalcService()))
        .buildAndWaitForRunning();
    logger.info("Server started, listening on " + port);
    // TODO(user): gRPC server should register JVM shutdown hook to shutdown itself, remove this
    // after we support that.
    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        // Use stderr here since the logger may has been reset by its JVM shutdown hook.
        System.err.println("*** shutting down gRPC server since JVM is shutting down");
        MathServer.this.stop();
        System.err.println("*** server shut down");
      }
    });
  }

  public void stop() {
    if (gRpcServer != null) {
      gRpcServer.stopAsync();
      gRpcServer.awaitTerminated();
    }
  }

  public static void main(String[] args) throws Exception {
    MathServer server = new MathServer(8980);
    server.start();
    server.gRpcServer.awaitTerminated();
  }

  /**
   * Our implementation of Calc service.
   *
   * <p> See math.proto for details of the methods.
   */
  private class CalcService implements CalcGrpc.Calc {

    @Override
    public void div(DivArgs request, StreamObserver<DivReply> responseObserver) {
      if (request.getDivisor() == 0) {
        responseObserver.onError(new IllegalArgumentException("divisor is 0"));
        return;
      }
      responseObserver.onValue(div(request));
      responseObserver.onCompleted();
    }

    @Override
    public StreamObserver<DivArgs> divMany(final StreamObserver<DivReply> responseObserver) {
      return new StreamObserver<DivArgs>() {
        @Override
        public void onValue(DivArgs value) {
          if (value.getDivisor() == 0) {
            responseObserver.onError(new IllegalArgumentException("divisor is 0"));
            return;
          }
          responseObserver.onValue(div(value));
        }

        @Override
        public void onError(Throwable t) {
          logger.log(Level.WARNING, "Encountered error in divMany", t);
        }

        @Override
        public void onCompleted() {
          responseObserver.onCompleted();
        }};
    }

    @Override
    public void fib(FibArgs request, StreamObserver<Num> responseObserver) {
      int limit = (int) request.getLimit();
      if (limit <= 0) {
        // TODO(user): Support "send until cancel". Currently, client application can not
        // cancel a server streaming call.
        return;
      }
      long preLast = 1;
      long last = 1;
      for (int i = 0; i < limit; i++) {
        if (i < 2) {
          responseObserver.onValue(Num.newBuilder().setNum(1).build());
        } else {
          long fib = preLast + last;
          responseObserver.onValue(Num.newBuilder().setNum(fib).build());
          preLast = last;
          last = fib;
        }
      }
      responseObserver.onCompleted();
    }

    @Override
    public StreamObserver<Num> sum(final StreamObserver<Num> responseObserver) {
      return new StreamObserver<Num>() {
        long sum = 0;

        @Override
        public void onValue(Num value) {
          sum += value.getNum();
        }

        @Override
        public void onError(Throwable t) {
          logger.log(Level.WARNING, "Encountered error in sum", t);
        }

        @Override
        public void onCompleted() {
          responseObserver.onValue(Num.newBuilder().setNum(sum).build());
          responseObserver.onCompleted();
        }
      };
    }

    private DivReply div(DivArgs request) {
      long divisor = request.getDivisor();
      long dividend = request.getDividend();
      return DivReply.newBuilder()
          .setQuotient(dividend / divisor)
          .setRemainder(dividend % divisor)
          .build();
    }
  }
}
