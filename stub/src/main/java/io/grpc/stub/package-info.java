/*
 * Copyright 2017, Google Inc. All rights reserved.
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
 * API for the Stub layer.
 *
 * <p>The gRPC Java API is split into two main parts: The stub layer and the call layer. The stub
 * layer is a wrapper around the call layer.
 *
 * <p>In the most common case of gRPC over Protocol Buffers, stub classes are automatically
 * generated from service definition .proto files by the Protobuf compiler. See <a
 * href="http://www.grpc.io/docs/generatedcode/java.html">gRPC java Generated Code Guide</a>.
 *
 * <p>The server side stub classes are abstract classes with RPC methods for the server application
 * to implement/override. These classes internally use {@link io.grpc.stub.ServerCalls} to interact
 * with the call layer. The RPC methods consume a {@link io.grpc.stub.StreamObserver} object {@code
 * responseObserver} as one of its arguments. When users are implementing/override these
 * methods in the server application, they may call the {@link io.grpc.stub.StreamObserver#onNext
 * onNext()}, {@link io.grpc.stub.StreamObserver#onError onError()} and {@link
 * io.grpc.stub.StreamObserver#onCompleted onCompleted()} methods on the {@code responseObserver}
 * argument to send out a response message, error and completion notification respectively. If the
 * RPC is client-streaming or bidirectional-streaming, the abstract RPC method should return a
 * {@code requestObserver} which is also a {@link io.grpc.stub.StreamObserver} object. User should
 * typically implement the {@link io.grpc.stub.StreamObserver#onNext onNext()}, {@link
 * io.grpc.stub.StreamObserver#onError onError()} and {@link io.grpc.stub.StreamObserver#onCompleted
 * onCompleted()} callbacks of {@code requestObserver} to define how the server application would
 * react when receiving a message, error and completion notification respectively from the client
 * side.
 *
 * <p>The client side stub classes are implementations of {@link io.grpc.stub.AbstractStub} that
 * provide the RPC methods for the client application to call. The RPC methods in the client stubs
 * internally use {@link io.grpc.stub.ClientCalls} to interact with the call layer. For asynchronous
 * stubs, the RPC methods also consume a {@link io.grpc.stub.StreamObserver} object {@code
 * responseObserver} as one of its arguments, and moreover for client-streaming or
 * bidirectional-streaming, also return a {@code requestObserver} which is also a {@link
 * io.grpc.stub.StreamObserver} object. In contrast to the server side, users should implement the
 * {@link io.grpc.stub.StreamObserver#onNext onNext()}, {@link io.grpc.stub.StreamObserver#onError
 * onError()} and {@link io.grpc.stub.StreamObserver#onCompleted onCompleted()} callbacks of {@code
 * responseObserver} to define what the client application would do when receiving a response
 * message, error and completion notification respectively from the server side, and then pass the
 * {@code responseObserver} to the RPC method in the client stub. If the RPC method returns a {@code
 * requestObserver}, users should call the {@link io.grpc.stub.StreamObserver#onNext onNext()},
 * {@link io.grpc.stub.StreamObserver#onError onError()} and {@link
 * io.grpc.stub.StreamObserver#onCompleted onCompleted()} methods on the {@code requestObserver} to
 * send out a request message, error and completion notification respectively.
 */
package io.grpc.stub;
