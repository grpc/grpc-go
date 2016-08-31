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

package io.grpc.testing;

import io.grpc.ExperimentalApi;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.Status;

/**
 * {@link NoopServerCall} is a class that is designed for use in tests.  It is designed to be used
 * in places where a scriptable call is necessary.  By default, all methods are noops, and designed
 * to be overriden.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2234")
public class NoopServerCall<ReqT, RespT> extends ServerCall<ReqT, RespT> {

  /**
   * {@link NoopServerCall.NoopServerCallListener} is a class that is designed for use in tests.
   * It is designed to be used in places where a scriptable call listener is necessary.  By
   * default, all methods are noops, and designed to be overriden.
   */
  public static class NoopServerCallListener<T> extends ServerCall.Listener<T> {
  }

  @Override
  public void request(int numMessages) {}

  @Override
  public void sendHeaders(Metadata headers) {}

  @Override
  public void sendMessage(RespT message) {}

  @Override
  public void close(Status status, Metadata trailers) {}

  @Override
  public boolean isCancelled() {
    return false;
  }

  @Override
  public MethodDescriptor<ReqT, RespT> getMethodDescriptor() {
    return null;
  }
}

