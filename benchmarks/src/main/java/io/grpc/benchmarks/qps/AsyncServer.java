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

import static grpc.testing.Qpstest.Payload;
import static grpc.testing.Qpstest.PayloadType;
import static grpc.testing.Qpstest.SimpleRequest;
import static grpc.testing.Qpstest.SimpleResponse;
import static io.grpc.testing.integration.Util.loadCert;
import static io.grpc.testing.integration.Util.pickUnusedPort;

import com.google.common.util.concurrent.MoreExecutors;
import com.google.protobuf.ByteString;

import grpc.testing.TestServiceGrpc;
import io.grpc.ServerImpl;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import io.grpc.transport.netty.NettyServerBuilder;
import io.netty.handler.ssl.SslContext;

import java.io.File;
import java.util.concurrent.TimeUnit;

/**
 * QPS server using the non-blocking API.
 */
public class AsyncServer {

  private boolean tls;
  private int port;
  private int connectionWindow = NettyServerBuilder.DEFAULT_CONNECTION_WINDOW_SIZE;
  private int streamWindow = NettyServerBuilder.DEFAULT_STREAM_WINDOW_SIZE;
  private boolean directExecutor;

  /**
   * checkstyle complains if there is no javadoc comment here.
   */
  public static void main(String... args) throws Exception {
    new AsyncServer().run(args);
  }

  /** Equivalent of "main", but non-static. */
  public void run(String[] args) throws Exception {
    if (!parseArgs(args)) {
      return;
    }

    SslContext sslContext = null;
    if (tls) {
      System.out.println("Using fake CA for TLS certificate.\n"
                         + "Run the Java client with --tls --testca");

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
            .executor(directExecutor ? MoreExecutors.newDirectExecutorService() : null)
            .connectionWindowSize(connectionWindow)
            .streamWindowSize(streamWindow)
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
        } else if ("tls".equals(key)) {
          tls = true;
        } else if ("directexecutor".equals(key)) {
          directExecutor = true;
        } else if ("connection_window".equals(key)) {
          connectionWindow = Integer.parseInt(value);
        } else if ("stream_window".equals(key)) {
          streamWindow = Integer.parseInt(value);
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
    AsyncServer s = new AsyncServer();
    System.out.println(
            "Usage: [ARGS...]"
            + "\n"
            + "\n  --port=INT                  Port of the server. Required. No default."
            + "\n  --tls                       Enable TLS. Default disabled."
            + "\n  --directexecutor            Use a direct executor i.e. execute all RPC"
            + "\n                              calls directly in Netty's event loop"
            + "\n                              overhead of a thread pool."
            + "\n  --connection_window=BYTES   The HTTP/2 connection flow control window."
            + "\n                              Default " + connectionWindow + " byte."
            + "\n  --stream_window=BYTES       The HTTP/2 per-stream flow control window."
            + "\n                              Default " + streamWindow + " byte."
    );
  }

  private static class TestServiceImpl implements TestServiceGrpc.TestService {

    @Override
    public void unaryCall(SimpleRequest request, StreamObserver<SimpleResponse> responseObserver) {
      SimpleResponse response = buildSimpleResponse(request);
      responseObserver.onValue(response);
      responseObserver.onCompleted();
    }

    @Override
    public StreamObserver<SimpleRequest> streamingCall(
        final StreamObserver<SimpleResponse> responseObserver) {
      return new StreamObserver<SimpleRequest>() {
        @Override
        public void onValue(SimpleRequest request) {
          SimpleResponse response = buildSimpleResponse(request);
          responseObserver.onValue(response);
        }

        @Override
        public void onError(Throwable t) {
          System.out.println("Encountered an error in streamingCall");
          t.printStackTrace();
        }

        @Override
        public void onCompleted() {
          responseObserver.onCompleted();
        }
      };
    }

    private static SimpleResponse buildSimpleResponse(SimpleRequest request) {
      if (!request.hasResponseSize()) {
        throw Status.INTERNAL.augmentDescription("responseSize required").asRuntimeException();
      }
      if (!request.hasResponseType()) {
        throw Status.INTERNAL.augmentDescription("responseType required").asRuntimeException();
      }
      if (request.getResponseSize() > 0) {
        if (!PayloadType.COMPRESSABLE.equals(request.getResponseType())) {
          throw Status.INTERNAL.augmentDescription("Error creating payload.").asRuntimeException();
        }

        ByteString body = ByteString.copyFrom(new byte[request.getResponseSize()]);
        PayloadType type = request.getResponseType();

        Payload payload = Payload.newBuilder().setType(type).setBody(body).build();
        SimpleResponse response = SimpleResponse.newBuilder().setPayload(payload).build();
        return response;
      }
      return SimpleResponse.getDefaultInstance();
    }
  }
}
