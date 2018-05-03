/*
 * Copyright 2016 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.benchmarks.driver;

import com.google.common.util.concurrent.ThreadFactoryBuilder;
import io.grpc.Server;
import io.grpc.Status;
import io.grpc.benchmarks.proto.Control;
import io.grpc.benchmarks.proto.Control.ClientArgs;
import io.grpc.benchmarks.proto.Control.ServerArgs;
import io.grpc.benchmarks.proto.Control.ServerArgs.ArgtypeCase;
import io.grpc.benchmarks.proto.WorkerServiceGrpc;
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
  private final Server driverServer;

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
