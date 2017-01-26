/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.benchmarks.driver;

import com.google.common.util.concurrent.ThreadFactoryBuilder;
import io.grpc.Status;
import io.grpc.benchmarks.proto.Control;
import io.grpc.benchmarks.proto.Control.ClientArgs;
import io.grpc.benchmarks.proto.Control.ServerArgs;
import io.grpc.benchmarks.proto.Control.ServerArgs.ArgtypeCase;
import io.grpc.benchmarks.proto.WorkerServiceGrpc;
import io.grpc.internal.ServerImpl;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.stub.StreamObserver;
import io.netty.channel.nio.NioEventLoopGroup;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A load worker process which a driver can use to create clients and servers. The worker
 * implements the contract defined in 'control.proto'.
 */
public class LoadWorker {

  private static final Logger log = Logger.getLogger(LoadWorker.class.getName());

  private final int serverPort;
  private final ServerImpl driverServer;

  LoadWorker(int driverPort, int serverPort) throws Exception {
    this.serverPort = serverPort;
    NioEventLoopGroup singleThreadGroup = new NioEventLoopGroup(1,
        new ThreadFactoryBuilder()
            .setDaemon(true)
            .setNameFormat("load-worker-%d")
            .build());
    this.driverServer = NettyServerBuilder.forPort(driverPort)
        .directExecutor()
        .workerEventLoopGroup(singleThreadGroup)
        .bossEventLoopGroup(singleThreadGroup)
        .addService(new WorkerServiceImpl())
        .build();
  }

  public void start() throws Exception {
    driverServer.start();
  }

  /**
   * Start the load worker process.
   */
  public static void main(String[] args) throws Exception {
    boolean usage = false;
    int serverPort = 0;
    int driverPort = 0;
    for (String arg : args) {
      if (!arg.startsWith("--")) {
        System.err.println("All arguments must start with '--': " + arg);
        usage = true;
        break;
      }
      String[] parts = arg.substring(2).split("=", 2);
      String key = parts[0];
      if ("help".equals(key)) {
        usage = true;
        break;
      }
      if (parts.length != 2) {
        System.err.println("All arguments must be of the form --arg=value");
        usage = true;
        break;
      }
      String value = parts[1];
      if ("server_port".equals(key)) {
        serverPort = Integer.valueOf(value);
      } else if ("driver_port".equals(key)) {
        driverPort = Integer.valueOf(value);
      } else {
        System.err.println("Unknown argument: " + key);
        usage = true;
        break;
      }
    }
    if (usage || driverPort == 0) {
      System.err.println(
          "Usage: [ARGS...]"
              + "\n"
              + "\n  --driver_port=<port>"
              + "\n    Port to expose grpc.testing.WorkerService, used by driver to initiate work."
              + "\n  --server_port=<port>"
              + "\n    Port to start load servers on. Defaults to any available port");
      System.exit(1);
    }
    LoadWorker loadWorker = new LoadWorker(driverPort, serverPort);
    loadWorker.start();
    loadWorker.driverServer.awaitTermination();
    log.log(Level.INFO, "DriverServer has terminated.");

    // Allow enough time for quitWorker to deliver OK status to the driver.
    Thread.sleep(3000);
  }

  /**
   * Implement the worker service contract which can launch clients and servers.
   */
  private class WorkerServiceImpl extends WorkerServiceGrpc.WorkerServiceImplBase {

    private LoadServer workerServer;
    private LoadClient workerClient;

    @Override
    public StreamObserver<ServerArgs> runServer(
        final StreamObserver<Control.ServerStatus> responseObserver) {
      return new StreamObserver<ServerArgs>() {
        @Override
        public void onNext(ServerArgs value) {
          try {
            ArgtypeCase argTypeCase = value.getArgtypeCase();
            if (argTypeCase == ServerArgs.ArgtypeCase.SETUP && workerServer == null) {
              if (serverPort != 0 && value.getSetup().getPort() == 0) {
                Control.ServerArgs.Builder builder = value.toBuilder();
                builder.getSetupBuilder().setPort(serverPort);
                value = builder.build();
              }
              workerServer = new LoadServer(value.getSetup());
              workerServer.start();
              responseObserver.onNext(Control.ServerStatus.newBuilder()
                  .setPort(workerServer.getPort())
                  .setCores(workerServer.getCores())
                  .build());
            } else if (argTypeCase == ArgtypeCase.MARK && workerServer != null) {
              responseObserver.onNext(Control.ServerStatus.newBuilder()
                  .setStats(workerServer.getStats())
                  .build());
            } else {
              responseObserver.onError(Status.ALREADY_EXISTS
                  .withDescription("Server already started")
                  .asRuntimeException());
            }
          } catch (Throwable t) {
            log.log(Level.WARNING, "Error running server", t);
            responseObserver.onError(Status.INTERNAL.withCause(t).asException());
            // Shutdown server if we can
            onCompleted();
          }
        }

        @Override
        public void onError(Throwable t) {
          Status status = Status.fromThrowable(t);
          if (status.getCode() != Status.Code.CANCELLED) {
            log.log(Level.WARNING, "Error driving server", t);
          }
          onCompleted();
        }

        @Override
        public void onCompleted() {
          try {
            if (workerServer != null) {
              workerServer.shutdownNow();
            }
          } finally {
            workerServer = null;
            responseObserver.onCompleted();
          }
        }
      };
    }

    @Override
    public StreamObserver<ClientArgs> runClient(
        final StreamObserver<Control.ClientStatus> responseObserver) {
      return new StreamObserver<ClientArgs>() {
        @Override
        public void onNext(ClientArgs value) {
          try {
            ClientArgs.ArgtypeCase argTypeCase = value.getArgtypeCase();
            if (argTypeCase == ClientArgs.ArgtypeCase.SETUP && workerClient == null) {
              workerClient = new LoadClient(value.getSetup());
              workerClient.start();
              responseObserver.onNext(Control.ClientStatus.newBuilder().build());
            } else if (argTypeCase == ClientArgs.ArgtypeCase.MARK && workerClient != null) {
              responseObserver.onNext(Control.ClientStatus.newBuilder()
                  .setStats(workerClient.getStats())
                  .build());
            } else {
              responseObserver.onError(Status.ALREADY_EXISTS
                  .withDescription("Client already started")
                  .asRuntimeException());
            }
          } catch (Throwable t) {
            log.log(Level.WARNING, "Error running client", t);
            responseObserver.onError(Status.INTERNAL.withCause(t).asException());
            // Shutdown the client if we can
            onCompleted();
          }
        }

        @Override
        public void onError(Throwable t) {
          Status status = Status.fromThrowable(t);
          if (status.getCode() != Status.Code.CANCELLED) {
            log.log(Level.WARNING, "Error driving client", t);
          }
          onCompleted();
        }

        @Override
        public void onCompleted() {
          try {
            if (workerClient != null) {
              workerClient.shutdownNow();
            }
          } finally {
            workerClient = null;
            responseObserver.onCompleted();
          }
        }
      };
    }

    @Override
    public void coreCount(Control.CoreRequest request,
                          StreamObserver<Control.CoreResponse> responseObserver) {
      responseObserver.onNext(
          Control.CoreResponse.newBuilder()
              .setCores(Runtime.getRuntime().availableProcessors())
              .build());
      responseObserver.onCompleted();
    }

    @Override
    public void quitWorker(Control.Void request,
                           StreamObserver<Control.Void> responseObserver) {
      try {
        log.log(Level.INFO, "Received quitWorker request.");
        responseObserver.onNext(Control.Void.getDefaultInstance());
        responseObserver.onCompleted();
        driverServer.shutdownNow();
      } catch (Throwable t) {
        log.log(Level.WARNING, "Error during shutdown", t);
      }
    }
  }
}
