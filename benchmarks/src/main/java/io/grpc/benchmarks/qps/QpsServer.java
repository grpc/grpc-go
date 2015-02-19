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

package io.grpc.benchmarks.qps;

import static grpc.testing.Qpstest.StatsRequest;
import static grpc.testing.Qpstest.ServerStats;
import static grpc.testing.Qpstest.Latencies;
import static grpc.testing.Qpstest.StartArgs;
import static grpc.testing.Qpstest.Payload;
import static grpc.testing.Qpstest.PayloadType;
import static grpc.testing.Qpstest.SimpleResponse;
import static grpc.testing.Qpstest.SimpleRequest;
import static grpc.testing.Qpstest.StreamingInputCallResponse;
import static grpc.testing.Qpstest.StreamingOutputCallResponse;
import static grpc.testing.Qpstest.StreamingInputCallRequest;
import static grpc.testing.Qpstest.StreamingOutputCallRequest;
import static java.lang.Math.max;
import static io.grpc.testing.integration.Util.loadCert;
import static io.grpc.testing.integration.Util.pickUnusedPort;

import com.google.common.util.concurrent.MoreExecutors;
import com.google.protobuf.ByteString;

import grpc.testing.Qpstest;
import grpc.testing.TestServiceGrpc;
import io.grpc.ServerImpl;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import io.grpc.transport.netty.NettyServerBuilder;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.handler.ssl.SslContext;

import java.io.File;
import java.util.concurrent.TimeUnit;

public class QpsServer {

  private boolean enable_tls;
  private int port = 0;

  public static void main(String... args) throws Exception {
    new QpsServer().run(args);
  }

  public void run(String[] args) throws Exception {
    if (!parseArgs(args)) {
      return;
    }

    SslContext sslContext = null;
    if (enable_tls) {
      System.out.println("Using fake CA for TLS certificate.\n"
                         + "Run the Java client with --enable_tls --use_testca");

      File cert = loadCert("server1.pem");
      File key = loadCert("server1.key");
      sslContext = SslContext.newServerContext(cert, key);
    }

    if (port == 0) {
      port = pickUnusedPort();
    }

    final ServerImpl server = NettyServerBuilder
            .forPort(port)
            .addService(TestServiceGrpc.bindService(new TestServiceImpl()))
            .sslContext(sslContext)
            .build();
    server.start();

    System.out.println("QPS Server started on port " + port);

    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        try {
          System.out.println("QPS Server shutting down");
          server.shutdown();
          server.awaitTerminated(5, TimeUnit.SECONDS);
        } catch (Exception e) {
          e.printStackTrace();
        }
      }
    });
  }

  private boolean parseArgs(String[] args) {
    try {
      for (String arg : args) {
        if (!arg.startsWith("--")) {
          System.err.println("All arguments must start with '--': " + arg);
          printUsage();
          return false;
        }

        String[] pair = arg.substring(2).split("=", 2);
        String key = pair[0];
        String value = "";
        if (pair.length == 2) {
          value = pair[1];
        }

        if ("help".equals(key)) {
          printUsage();
          return false;
        } else if ("port".equals(key)) {
          port = Integer.parseInt(value);
        } else if ("enable_tls".equals(key)) {
          enable_tls = true;
        } else {
          System.err.println("Unrecognized argument '" + key + "'.");
        }
      }
    } catch (Exception e) {
      e.printStackTrace();
      printUsage();
      return false;
    }

    return true;
  }

  private void printUsage() {
    QpsServer s = new QpsServer();
    System.out.println(
            "Usage: [ARGS...]"
            + "\n"
            + "\n  --port             Port of the server. By default a random port is chosen."
            + "\n  --enable_tls       Enable TLS. Default disabled."
    );
  }

  private static class TestServiceImpl implements TestServiceGrpc.TestService {

    @Override
    public void startTest(StartArgs request, StreamObserver<Latencies> responseObserver) {
      throw Status.UNIMPLEMENTED.asRuntimeException();
    }

    @Override
    public void collectServerStats(StatsRequest request,
                                   StreamObserver<ServerStats> responseObserver) {
      double nowSeconds = System.currentTimeMillis() / 1000.0;

      ServerStats stats = ServerStats.newBuilder()
                                     .setTimeNow(nowSeconds)
                                     .setTimeUser(0)
                                     .setTimeSystem(0)
                                     .build();
      responseObserver.onValue(stats);
      responseObserver.onCompleted();
    }

    @Override
    public void unaryCall(SimpleRequest request,
                          StreamObserver<Qpstest.SimpleResponse> responseObserver) {
      if (!request.hasResponseSize()) {
        throw Status.INTERNAL.augmentDescription("responseSize required").asRuntimeException();
      } else if (!request.hasResponseType()) {
        throw Status.INTERNAL.augmentDescription("responseType required").asRuntimeException();
      } else if (request.getResponseSize() > 0) {
        // I just added this condition to mimic the C++ QPS Server behaviour.
        if (!PayloadType.COMPRESSABLE.equals(request.getResponseType())) {
          throw Status.INTERNAL.augmentDescription("Error creating payload.").asRuntimeException();
        }

        ByteString body = ByteString.copyFrom(new byte[request.getResponseSize()]);
        PayloadType type = request.getResponseType();

        Payload payload = Payload.newBuilder().setType(type).setBody(body).build();
        SimpleResponse response = SimpleResponse.newBuilder().setPayload(payload).build();

        responseObserver.onValue(response);
        responseObserver.onCompleted();
      } else {
        responseObserver.onValue(SimpleResponse.getDefaultInstance());
        responseObserver.onCompleted();
      }
    }

    @Override
    public void streamingOutputCall(StreamingOutputCallRequest request,
                                    StreamObserver<StreamingOutputCallResponse> responseObserver) {
      throw Status.UNIMPLEMENTED.asRuntimeException();
    }

    @Override
    public StreamObserver<StreamingInputCallRequest>
    streamingInputCall(StreamObserver<StreamingInputCallResponse> responseObserver) {
      throw Status.UNIMPLEMENTED.asRuntimeException();
    }

    @Override
    public StreamObserver<StreamingOutputCallRequest>
    fullDuplexCall(StreamObserver<StreamingOutputCallResponse> responseObserver) {
      throw Status.UNIMPLEMENTED.asRuntimeException();
    }

    @Override
    public StreamObserver<StreamingOutputCallRequest>
    halfDuplexCall(StreamObserver<StreamingOutputCallResponse> responseObserver) {
      throw Status.UNIMPLEMENTED.asRuntimeException();
    }
  }
}
