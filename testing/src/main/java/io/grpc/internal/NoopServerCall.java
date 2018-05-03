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

package io.grpc.internal;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.Status;

/**
 * {@link NoopServerCall} is a class that is designed for use in tests.  It is designed to be used
 * in places where a scriptable call is necessary.  By default, all methods are noops, and designed
 * to be overridden.
 */
public class NoopServerCall<ReqT, RespT> extends ServerCall<ReqT, RespT> {

  /**
   * {@link NoopServerCall.NoopServerCallListener} is a class that is designed for use in tests.
   * It is designed to be used in places where a scriptable call listener is necessary.  By
   * default, all methods are noops, and designed to be overridden.
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
