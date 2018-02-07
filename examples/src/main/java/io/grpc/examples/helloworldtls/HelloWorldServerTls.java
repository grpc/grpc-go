/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

package io.grpc.examples.helloworldtls;

import io.grpc.Server;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.stub.StreamObserver;
import io.netty.handler.ssl.ClientAuth;
import io.netty.handler.ssl.SslContextBuilder;
import io.netty.handler.ssl.SslProvider;

import java.io.File;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.util.logging.Logger;

/**
 * Server that manages startup/shutdown of a {@code Greeter} server with TLS enabled.
 */
public class HelloWorldServerTls {
    private static final Logger logger = Logger.getLogger(HelloWorldServerTls.class.getName());

    private Server server;

    private final String host;
    private final int port;
    private final String certChainFilePath;
    private final String privateKeyFilePath;
    private final String clientCertChainFilePath;

    public HelloWorldServerTls(String host,
                               int port,
                               String certChainFilePath,
                               String privateKeyFilePath,
                               String clientCertChainFilePath) {
        this.host = host;
        this.port = port;
        this.certChainFilePath = certChainFilePath;
        this.privateKeyFilePath = privateKeyFilePath;
        this.clientCertChainFilePath = clientCertChainFilePath;
    }

    private SslContextBuilder getSslContextBuilder() {
        SslContextBuilder sslClientContextBuilder = SslContextBuilder.forServer(new File(certChainFilePath),
                new File(privateKeyFilePath));
        if (clientCertChainFilePath != null) {
            sslClientContextBuilder.trustManager(new File(clientCertChainFilePath));
            sslClientContextBuilder.clientAuth(ClientAuth.OPTIONAL);
        }
        return GrpcSslContexts.configure(sslClientContextBuilder,
                SslProvider.OPENSSL);
    }

    private void start() throws IOException {
        server = NettyServerBuilder.forAddress(new InetSocketAddress(host, port))
                .addService(new GreeterImpl())
                .sslContext(getSslContextBuilder().build())
                .build()
                .start();
        logger.info("Server started, listening on " + port);
        Runtime.getRuntime().addShutdownHook(new Thread() {
            @Override
            public void run() {
                // Use stderr here since the logger may have been reset by its JVM shutdown hook.
                System.err.println("*** shutting down gRPC server since JVM is shutting down");
                HelloWorldServerTls.this.stop();
                System.err.println("*** server shut down");
            }
        });
    }

    private void stop() {
        if (server != null) {
            server.shutdown();
        }
    }

    /**
     * Await termination on the main thread since the grpc library uses daemon threads.
     */
    private void blockUntilShutdown() throws InterruptedException {
        if (server != null) {
            server.awaitTermination();
        }
    }

    /**
     * Main launches the server from the command line.
     */
    public static void main(String[] args) throws IOException, InterruptedException {

        if (args.length < 4 || args.length > 5) {
            System.out.println(
                    "USAGE: HelloWorldServerTls host port certChainFilePath privateKeyFilePath " +
                    "[clientCertChainFilePath]\n  Note: You only need to supply clientCertChainFilePath if you want " +
                    "to enable Mutual TLS.");
            System.exit(0);
        }

        final HelloWorldServerTls server = new HelloWorldServerTls(args[0],
                Integer.parseInt(args[1]),
                args[2],
                args[3],
                args.length == 5 ? args[4] : null);
        server.start();
        server.blockUntilShutdown();
    }

    static class GreeterImpl extends GreeterGrpc.GreeterImplBase {

        @Override
        public void sayHello(HelloRequest req, StreamObserver<HelloReply> responseObserver) {
            HelloReply reply = HelloReply.newBuilder().setMessage("Hello " + req.getName()).build();
            responseObserver.onNext(reply);
            responseObserver.onCompleted();
        }
    }
}
