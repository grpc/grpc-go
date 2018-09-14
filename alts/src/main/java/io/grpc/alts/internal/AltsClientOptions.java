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

import io.grpc.alts.internal.TransportSecurityCommon.RpcProtocolVersions;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import javax.annotation.Nullable;

/** Handshaker options for creating ALTS client channel. */
public final class AltsClientOptions extends AltsHandshakerOptions {
  // targetName is the server service account name for secure name checking. This field is not yet
  // supported.
  @Nullable private final String targetName;
  // targetServiceAccounts contains a list of expected target service accounts. One of these service
  // accounts should match peer service account in the handshaker result. Otherwise, the handshake
  // fails.
  private final List<String> targetServiceAccounts;

  private AltsClientOptions(Builder builder) {
    super(builder.rpcProtocolVersions);
    targetName = builder.targetName;
    targetServiceAccounts =
        Collections.unmodifiableList(new ArrayList<>(builder.targetServiceAccounts));
  }

  public String getTargetName() {
    return targetName;
  }

  public List<String> getTargetServiceAccounts() {
    return targetServiceAccounts;
  }

  /** Builder for AltsClientOptions. */
  public static final class Builder {
    @Nullable private String targetName;
    @Nullable private RpcProtocolVersions rpcProtocolVersions;
    private ArrayList<String> targetServiceAccounts = new ArrayList<>();

    public Builder setTargetName(String targetName) {
      this.targetName = targetName;
      return this;
    }

    public Builder setRpcProtocolVersions(RpcProtocolVersions rpcProtocolVersions) {
      this.rpcProtocolVersions = rpcProtocolVersions;
      return this;
    }

    public Builder addTargetServiceAccount(String targetServiceAccount) {
      targetServiceAccounts.add(targetServiceAccount);
      return this;
    }

    public AltsClientOptions build() {
      return new AltsClientOptions(this);
    }
  }
}
