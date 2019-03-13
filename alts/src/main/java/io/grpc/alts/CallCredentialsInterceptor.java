/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.alts;

import io.grpc.CallCredentials;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import javax.annotation.Nullable;

/** An implementation of {@link ClientInterceptor} that adds call credentials on each call. */
final class CallCredentialsInterceptor implements ClientInterceptor {

  @Nullable private final CallCredentials credentials;
  private final Status status;

  public CallCredentialsInterceptor(@Nullable CallCredentials credentials, Status status) {
    this.credentials = credentials;
    this.status = status;
  }

  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
      MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
    if (!status.isOk()) {
      return new FailingClientCall<>(status);
    }
    return next.newCall(method, callOptions.withCallCredentials(credentials));
  }
}
