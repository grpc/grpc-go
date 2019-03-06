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

import io.grpc.Attributes;
import io.grpc.InternalChannelz.Security;
import javax.annotation.Nullable;

/**
 * Internal accessor for {@link ProtocolNegotiationEvent}.
 */
public final class InternalProtocolNegotiationEvent {
  private InternalProtocolNegotiationEvent() {}

  public static ProtocolNegotiationEvent getDefault() {
    return ProtocolNegotiationEvent.DEFAULT;
  }

  public static ProtocolNegotiationEvent withAttributes(
      ProtocolNegotiationEvent event, Attributes attributes) {
    return event.withAttributes(attributes);
  }

  public static ProtocolNegotiationEvent withSecurity(
      ProtocolNegotiationEvent event, @Nullable Security security) {
    return event.withSecurity(security);
  }

  public static Attributes getAttributes(ProtocolNegotiationEvent event) {
    return event.getAttributes();
  }

  @Nullable
  public static Security getSecurity(ProtocolNegotiationEvent event) {
    return event.getSecurity();
  }
}
