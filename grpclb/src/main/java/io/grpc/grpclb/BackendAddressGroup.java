/*
 * Copyright 2017 The gRPC Authors
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

import io.grpc.EquivalentAddressGroup;
import javax.annotation.Nullable;

final class BackendAddressGroup {
  private final EquivalentAddressGroup addresses;
  @Nullable
  private final String token;

  BackendAddressGroup(EquivalentAddressGroup addresses, @Nullable String token) {
    this.addresses = checkNotNull(addresses, "addresses");
    this.token = token;
  }

  EquivalentAddressGroup getAddresses() {
    return addresses;
  }

  @Nullable
  String getToken() {
    return token;
  }

  @Override
  public String toString() {
    // This is printed in logs.  Be concise.
    StringBuilder buffer = new StringBuilder();
    buffer.append(addresses);
    if (token != null) {
      buffer.append("(").append(token).append(")");
    }
    return buffer.toString();
  }
}
