/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc;

/**
 * Definition of a method exposed by a {@link Server}.
 *
 * @see ServerServiceDefinition
 */
public final class ServerMethodDefinition<ReqT, RespT> {
  private final MethodDescriptor<ReqT, RespT> method;
  private final ServerCallHandler<ReqT, RespT> handler;

  private ServerMethodDefinition(MethodDescriptor<ReqT, RespT> method,
      ServerCallHandler<ReqT, RespT> handler) {
    this.method = method;
    this.handler = handler;
  }

  /**
   * Create a new instance.
   *
   * @param method the {@link MethodDescriptor} for this method.
   * @param handler to dispatch calls to.
   * @return a new instance.
   */
  public static <ReqT, RespT> ServerMethodDefinition<ReqT, RespT> create(
      MethodDescriptor<ReqT, RespT> method,
      ServerCallHandler<ReqT, RespT> handler) {
    return new ServerMethodDefinition<ReqT, RespT>(method, handler);
  }

  /** The {@code MethodDescriptor} for this method. */
  public MethodDescriptor<ReqT, RespT> getMethodDescriptor() {
    return method;
  }

  /** Handler for incoming calls. */
  public ServerCallHandler<ReqT, RespT> getServerCallHandler() {
    return handler;
  }

  /**
   * Create a new method definition with a different call handler.
   *
   * @param handler to bind to a cloned instance of this.
   * @return a cloned instance of this with the new handler bound.
   */
  public ServerMethodDefinition<ReqT, RespT> withServerCallHandler(
      ServerCallHandler<ReqT, RespT> handler) {
    return new ServerMethodDefinition<ReqT, RespT>(method, handler);
  }
}
