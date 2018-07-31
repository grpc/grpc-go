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

package io.grpc.stub;

import io.grpc.ExperimentalApi;

/**
 * Specialization of {@link StreamObserver} implemented by clients in order to interact with the
 * advanced features of a call such as flow-control.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4693")
public interface ClientResponseObserver<ReqT, RespT> extends StreamObserver<RespT> {
  /**
   * Called by the runtime priot to the start of a call to provide a reference to the
   * {@link ClientCallStreamObserver} for the outbound stream. This can be used to listen to
   * onReady events, disable auto inbound flow and perform other advanced functions.
   *
   * <p>Only the methods {@link ClientCallStreamObserver#setOnReadyHandler(Runnable)} and
   * {@link ClientCallStreamObserver#disableAutoInboundFlowControl()} may be called within this
   * callback
   *
   * <pre>
   *   // Copy an iterator to the request stream under flow-control
   *   someStub.fullDuplexCall(new ClientResponseObserver&lt;ReqT, RespT&gt;() {
   *     public void beforeStart(final ClientCallStreamObserver&lt;Req&gt; requestStream) {
   *       StreamObservers.copyWithFlowControl(someIterator, requestStream);
   *   });
   * </pre>
   */
  void beforeStart(final ClientCallStreamObserver<ReqT> requestStream);
}
