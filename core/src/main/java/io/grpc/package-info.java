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

/**
 * The gRPC core public API.
 *
 * <p>gRPC is based on a client-server model of remote procedure calls. A client creates a channel
 * which is connected to a server. RPCs are initiated from the client and sent to the server which
 * then responds back to the client. When the client and server are done sending messages, they half
 * close their respective connections. The RPC is complete as soon as the server closes.
 *
 * <p>To send an RPC, first create a {@link io.grpc.Channel} using {@link
 * io.grpc.ManagedChannelBuilder#forTarget}. When using auto generate Protobuf stubs, the stub class
 * will have constructors for wrapping the channel. These include {@code newBlockingStub}, {@code
 * newStub}, and {@code newFutureStub} which you can use based on your design. The stub is the
 * primary way a client interacts with a server.
 *
 * <p>To receive RPCs, create a {@link io.grpc.Server} using {@link io.grpc.ServerBuilder#forPort}.
 * The Protobuf stub will contain an abstract class called AbstractFoo, where Foo is the name of
 * your service. Extend this class, and pass an instance of it to {@link
 * io.grpc.ServerBuilder#addService}. Once your server is built, call {@link io.grpc.Server#start}
 * to begin accepting RPCs.
 *
 * <p>Both Clients and Servers should use a custom {@link java.util.concurrent.Executor}. The gRPC
 * runtime includes a default executor that eases testing and examples, but is not ideal for use in
 * a production environment. See the associated documentation in the respective builders.
 *
 * <p>Clients and Servers can also be shutdown gracefully using the {@code shutdown} method. The API
 * to conduct an orderly shutdown is modeled from the {@link java.util.concurrent.ExecutorService}.
 *
 * <p>gRPC also includes support for more advanced features, such as name resolution, load
 * balancing, bidirectional streaming, health checking, and more. See the relative methods in the
 * client and server builders.
 *
 * <p>Development of gRPC is done primary on Github at <a
 * href="https://github.com/grpc/grpc-java">https://github.com/grpc/grpc-java</a>, where the gRPC
 * team welcomes contributions and bug reports. There is also a mailing list at <a
 * href="https://groups.google.com/forum/#!forum/grpc-io">grpc-io</a> if you have questions about
 * gRPC.
 */
package io.grpc;
