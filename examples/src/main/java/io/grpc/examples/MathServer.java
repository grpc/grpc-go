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

import io.grpc.examples.CalcGrpc;
import io.grpc.examples.Math.DivArgs;
import io.grpc.examples.Math.DivReply;
import io.grpc.examples.Math.FibArgs;
import io.grpc.examples.Math.Num;

import io.grpc.ServerImpl;
import io.grpc.stub.StreamObserver;
import io.grpc.transport.netty.NettyServerBuilder;

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
        .build().start();
    logger.info("Server started, listening on " + port);
    // TODO(simonma): gRPC server should register JVM shutdown hook to shutdown itself, remove this
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
      gRpcServer.shutdown();
    }
  }

  public static void main(String[] args) throws Exception {
    MathServer server = new MathServer(8980);
    server.start();
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
        // TODO(simonma): Support "send until cancel". Currently, client application can not
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
