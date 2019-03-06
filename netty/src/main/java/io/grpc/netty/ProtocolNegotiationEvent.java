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

package io.grpc.netty;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import io.grpc.Attributes;
import io.grpc.Internal;
import io.grpc.InternalChannelz.Security;
import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;

/**
 * Represents a completion of a protocol negotiation stage.
 */
@CheckReturnValue
@Internal
public final class ProtocolNegotiationEvent {

  static final ProtocolNegotiationEvent DEFAULT =
      new ProtocolNegotiationEvent(Attributes.EMPTY, /*security=*/ null);

  private final Attributes attributes;
  @Nullable
  private final Security security;

  private ProtocolNegotiationEvent(Attributes attributes, @Nullable Security security) {
    this.attributes = checkNotNull(attributes, "attributes");
    this.security = security;
  }

  @Nullable
  Security getSecurity() {
    return security;
  }

  Attributes getAttributes() {
    return attributes;
  }

  ProtocolNegotiationEvent withAttributes(Attributes attributes) {
    return new ProtocolNegotiationEvent(attributes, this.security);
  }

  ProtocolNegotiationEvent withSecurity(@Nullable Security security) {
    return new ProtocolNegotiationEvent(this.attributes, security);
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("attributes", attributes)
        .add("security", security)
        .toString();
  }

  /**
   * This method is not efficient and is intended for testing.
   */
  @Override
  public int hashCode() {
    return Objects.hashCode(attributes, security);
  }

  @Override
  public boolean equals(Object other) {
    if (!(other instanceof ProtocolNegotiationEvent)) {
      return false;
    }
    ProtocolNegotiationEvent that = (ProtocolNegotiationEvent) other;
    return Objects.equal(this.attributes, that.attributes)
        && Objects.equal(this.security, that.security);
  }
}
