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

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.Objects;
import io.grpc.CallOptions;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;

final class PickSubchannelArgsImpl extends PickSubchannelArgs {
  private final CallOptions callOptions;
  private final Metadata headers;
  private final MethodDescriptor<?, ?> method;

  /**
   * Creates call args object for given method with its call options, metadata.
   */
  PickSubchannelArgsImpl(MethodDescriptor<?, ?> method, Metadata headers, CallOptions callOptions) {
    this.method = checkNotNull(method, "method");
    this.headers = checkNotNull(headers, "headers");
    this.callOptions = checkNotNull(callOptions, "callOptions");
  }

  @Override
  public Metadata getHeaders() {
    return headers;
  }

  @Override
  public CallOptions getCallOptions() {
    return callOptions;
  }

  @Override
  public MethodDescriptor<?, ?> getMethodDescriptor() {
    return method;
  }

  @Override
  public boolean equals(Object o) {
    if (this == o) {
      return true;
    }
    if (o == null || getClass() != o.getClass()) {
      return false;
    }
    PickSubchannelArgsImpl that = (PickSubchannelArgsImpl) o;
    return Objects.equal(callOptions, that.callOptions)
        && Objects.equal(headers, that.headers)
        && Objects.equal(method, that.method);
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(callOptions, headers, method);
  }

  @Override
  public final String toString() {
    return "[method=" + method + " headers=" + headers + " callOptions=" + callOptions + "]";
  }
}
