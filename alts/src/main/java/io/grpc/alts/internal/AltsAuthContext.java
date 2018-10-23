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

import com.google.common.annotations.VisibleForTesting;

/** AltsAuthContext contains security-related context information about an ALTs connection. */
public final class AltsAuthContext {
  final AltsContext context;

  /** Create a new AltsAuthContext. */
  public AltsAuthContext(HandshakerResult result) {
    context =
        AltsContext.newBuilder()
            .setApplicationProtocol(result.getApplicationProtocol())
            .setRecordProtocol(result.getRecordProtocol())
            // TODO: Set security level based on the handshaker result.
            .setSecurityLevel(SecurityLevel.INTEGRITY_AND_PRIVACY)
            .setPeerServiceAccount(result.getPeerIdentity().getServiceAccount())
            .setLocalServiceAccount(result.getLocalIdentity().getServiceAccount())
            .setPeerRpcVersions(result.getPeerRpcVersions())
            .build();
  }

  @VisibleForTesting
  public static AltsAuthContext getDefaultInstance() {
    return new AltsAuthContext(HandshakerResult.newBuilder().build());
  }

  /**
   * Get application protocol.
   *
   * @return the context's application protocol.
   */
  public String getApplicationProtocol() {
    return context.getApplicationProtocol();
  }

  /**
   * Get negotiated record protocol.
   *
   * @return the context's negotiated record protocol.
   */
  public String getRecordProtocol() {
    return context.getRecordProtocol();
  }

  /**
   * Get security level.
   *
   * @return the context's security level.
   */
  public SecurityLevel getSecurityLevel() {
    return context.getSecurityLevel();
  }

  /**
   * Get peer service account.
   *
   * @return the context's peer service account.
   */
  public String getPeerServiceAccount() {
    return context.getPeerServiceAccount();
  }

  /**
   * Get local service account.
   *
   * @return the context's local service account.
   */
  public String getLocalServiceAccount() {
    return context.getLocalServiceAccount();
  }

  /**
   * Get peer RPC versions.
   *
   * @return the context's peer RPC versions.
   */
  public RpcProtocolVersions getPeerRpcVersions() {
    return context.getPeerRpcVersions();
  }
}
