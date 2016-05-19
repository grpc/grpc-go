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

package io.grpc.stub;

import io.grpc.ExperimentalApi;

/**
 * Specialization of {@link StreamObserver} implemented by clients in order to interact with the
 * advanced features of a call such as flow-control.
 */
@ExperimentalApi
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
