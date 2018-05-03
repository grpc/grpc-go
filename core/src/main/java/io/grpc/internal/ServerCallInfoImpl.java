/*
 * Copyright 2018 The gRPC Authors
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

import com.google.common.base.Objects;
import io.grpc.Attributes;
import io.grpc.MethodDescriptor;
import io.grpc.ServerStreamTracer.ServerCallInfo;
import javax.annotation.Nullable;

/**
 * An implementation of {@link ServerCallInfo}.
 */
final class ServerCallInfoImpl<ReqT, RespT> extends ServerCallInfo<ReqT, RespT> {
  private final MethodDescriptor<ReqT, RespT> methodDescriptor;
  private final Attributes attributes;
  private final String authority;

  ServerCallInfoImpl(
      MethodDescriptor<ReqT, RespT> methodDescriptor,
      Attributes attributes,
      @Nullable String authority) {
    this.methodDescriptor = methodDescriptor;
    this.attributes = attributes;
    this.authority = authority;
  }

  @Override
  public MethodDescriptor<ReqT, RespT> getMethodDescriptor() {
    return methodDescriptor;
  }

  @Override
  public Attributes getAttributes() {
    return attributes;
  }

  @Override
  @Nullable
  public String getAuthority() {
    return authority;
  }

  @Override
  public boolean equals(Object other) {
    if (!(other instanceof ServerCallInfoImpl)) {
      return false;
    }
    ServerCallInfoImpl<?, ?> that = (ServerCallInfoImpl) other;
    return Objects.equal(methodDescriptor, that.methodDescriptor)
        && Objects.equal(attributes, that.attributes)
        && Objects.equal(authority, that.authority);
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(methodDescriptor, attributes, authority);
  }
}
