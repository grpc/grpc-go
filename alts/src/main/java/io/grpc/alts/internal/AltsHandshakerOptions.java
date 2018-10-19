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

package io.grpc.alts.internal;

import javax.annotation.Nullable;

/** Handshaker options for creating ALTS channel. */
public class AltsHandshakerOptions {
  @Nullable private final RpcProtocolVersions rpcProtocolVersions;

  public AltsHandshakerOptions(RpcProtocolVersions rpcProtocolVersions) {
    this.rpcProtocolVersions = rpcProtocolVersions;
  }

  public RpcProtocolVersions getRpcProtocolVersions() {
    return rpcProtocolVersions;
  }
}
