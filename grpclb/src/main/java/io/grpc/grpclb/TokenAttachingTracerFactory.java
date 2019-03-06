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

package io.grpc.grpclb;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.Objects;
import io.grpc.Attributes;
import io.grpc.ClientStreamTracer;
import io.grpc.Metadata;
import io.grpc.internal.GrpcAttributes;
import javax.annotation.Nullable;

/**
 * Wraps a {@link ClientStreamTracer.Factory}, retrieves tokens from transport attributes and
 * attaches them to headers.  This is only used in the PICK_FIRST mode.
 */
final class TokenAttachingTracerFactory extends ClientStreamTracer.Factory {
  private static final ClientStreamTracer NOOP_TRACER = new ClientStreamTracer() {};

  @Nullable
  private final ClientStreamTracer.Factory delegate;

  TokenAttachingTracerFactory(@Nullable ClientStreamTracer.Factory delegate) {
    this.delegate = delegate;
  }

  @Override
  public ClientStreamTracer newClientStreamTracer(
      ClientStreamTracer.StreamInfo info, Metadata headers) {
    Attributes transportAttrs = checkNotNull(info.getTransportAttrs(), "transportAttrs");
    Attributes eagAttrs =
        checkNotNull(transportAttrs.get(GrpcAttributes.ATTR_CLIENT_EAG_ATTRS), "eagAttrs");
    String token = eagAttrs.get(GrpclbConstants.TOKEN_ATTRIBUTE_KEY);
    headers.discardAll(GrpclbConstants.TOKEN_METADATA_KEY);
    if (token != null) {
      headers.put(GrpclbConstants.TOKEN_METADATA_KEY, token);
    }
    if (delegate != null) {
      return delegate.newClientStreamTracer(info, headers);
    } else {
      return NOOP_TRACER;
    }
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(delegate);
  }

  @Override
  public boolean equals(Object other) {
    if (!(other instanceof TokenAttachingTracerFactory)) {
      return false;
    }
    return Objects.equal(delegate, ((TokenAttachingTracerFactory) other).delegate);
  }
}
