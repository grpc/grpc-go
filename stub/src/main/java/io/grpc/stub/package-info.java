/*
 * Copyright 2017 The gRPC Authors
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

/**
 * API for the Stub layer.
 *
 * <p>The gRPC Java API is split into two main parts: The stub layer and the call layer. The stub
 * layer is a wrapper around the call layer.
 *
 * <p>In the most common case of gRPC over Protocol Buffers, stub classes are automatically
 * generated from service definition .proto files by the Protobuf compiler. See <a
 * href="https://grpc.io/docs/reference/java/generated-code.html">gRPC Java Generated Code Guide</a>
 * .
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
