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

package io.grpc.stub;

import io.grpc.Marshaller;
import io.grpc.MethodType;

import javax.annotation.concurrent.Immutable;

/**
 * A description of a method exposed by a service. Typically instances are created via code
 * generation.
 */
@Immutable
public class Method<RequestT, ResponseT> {

  private final MethodType type;
  private final String name;
  private final Marshaller<RequestT> requestMarshaller;
  private final Marshaller<ResponseT> responseMarshaller;

  /**
   * Constructor.
   *
   * @param type the call type of the method
   * @param name the name of the method, not including the service name
   * @param requestMarshaller used to serialize/deserialize the request
   * @param responseMarshaller used to serialize/deserialize the response
   */
  public static <RequestT, ResponseT> Method<RequestT, ResponseT> create(
      MethodType type, String name,
      Marshaller<RequestT> requestMarshaller, Marshaller<ResponseT> responseMarshaller) {
    return new Method<RequestT, ResponseT>(type, name, requestMarshaller, responseMarshaller);
  }

  private Method(MethodType type, String name, Marshaller<RequestT> requestMarshaller,
      Marshaller<ResponseT> responseMarshaller) {
    this.type = type;
    this.name = name;
    this.requestMarshaller = requestMarshaller;
    this.responseMarshaller = responseMarshaller;
  }

  /**
   * The call type of the method.
   */
  public MethodType getType() {
    return type;
  }

  /**
   * The name of the method, not including the service name
   */
  public String getName() {
    return name;
  }

  /**
   * The marshaller used to serialize/deserialize the request.
   */
  public Marshaller<RequestT> getRequestMarshaller() {
    return requestMarshaller;
  }

  /**
   * The marshaller used to serialize/deserialize the response.
   */
  public Marshaller<ResponseT> getResponseMarshaller() {
    return responseMarshaller;
  }
}
