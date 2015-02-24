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

import java.io.InputStream;

/**
 * Definition of a method bound by a {@link io.grpc.HandlerRegistry} and exposed
 * by a {@link Server}.
 */
public final class ServerMethodDefinition<RequestT, ResponseT> {
  private final String name;
  private final Marshaller<RequestT> requestMarshaller;
  private final Marshaller<ResponseT> responseMarshaller;
  private final ServerCallHandler<RequestT, ResponseT> handler;

  // ServerMethodDefinition has no form of public construction. It is only created within the
  // context of a ServerServiceDefinition.Builder.
  ServerMethodDefinition(String name, Marshaller<RequestT> requestMarshaller,
      Marshaller<ResponseT> responseMarshaller, ServerCallHandler<RequestT, ResponseT> handler) {
    this.name = name;
    this.requestMarshaller = requestMarshaller;
    this.responseMarshaller = responseMarshaller;
    this.handler = handler;
  }

  /**
   * Create a new instance.
   *
   * @param name the simple name of a method.
   * @param requestMarshaller marshaller for request messages.
   * @param responseMarshaller marshaller for response messages.
   * @param handler to dispatch calls to.
   * @return a new instance.
   */
  public static <RequestT, ResponseT> ServerMethodDefinition<RequestT, ResponseT> create(
      String name, Marshaller<RequestT> requestMarshaller,
      Marshaller<ResponseT> responseMarshaller, ServerCallHandler<RequestT, ResponseT> handler) {
    return new ServerMethodDefinition<RequestT, ResponseT>(name, requestMarshaller,
        responseMarshaller, handler);
  }

  /** The simple name of the method. It is not an absolute path. */
  public String getName() {
    return name;
  }

  /**
   * Parse an incoming request message.
   *
   * @param input the serialized message as a byte stream.
   * @return a parsed instance of the message.
   */
  public RequestT parseRequest(InputStream input) {
    return requestMarshaller.parse(input);
  }

  /**
   * Serialize an outgoing response message.
   *
   * @param response the response message to serialize.
   * @return the serialized message as a byte stream.
   */
  public InputStream streamResponse(ResponseT response) {
    return responseMarshaller.stream(response);
  }

  /** Handler for incoming calls. */
  public ServerCallHandler<RequestT, ResponseT> getServerCallHandler() {
    return handler;
  }

  /**
   * Create a new method definition with a different call handler.
   *
   * @param handler to bind to a cloned instance of this.
   * @return a cloned instance of this with the new handler bound.
   */
  public ServerMethodDefinition<RequestT, ResponseT> withServerCallHandler(
      ServerCallHandler<RequestT, ResponseT> handler) {
    return new ServerMethodDefinition<RequestT, ResponseT>(
        name, requestMarshaller, responseMarshaller, handler);
  }
}
