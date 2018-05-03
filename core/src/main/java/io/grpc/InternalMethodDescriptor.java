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

package io.grpc;

import static com.google.common.base.Preconditions.checkNotNull;

/**
 * Accesses internal data and methods.  Do not use this.
 */
@Internal
public final class InternalMethodDescriptor {
  private final InternalKnownTransport transport;

  public InternalMethodDescriptor(InternalKnownTransport transport) {
    // TODO(carl-mastrangelo): maybe restrict access to this.
    this.transport = checkNotNull(transport, "transport");
  }

  public Object geRawMethodName(MethodDescriptor<?, ?> md) {
    return md.getRawMethodName(transport.ordinal());
  }

  public void setRawMethodName(MethodDescriptor<?, ?> md, Object o) {
    md.setRawMethodName(transport.ordinal(), o);
  }
}
