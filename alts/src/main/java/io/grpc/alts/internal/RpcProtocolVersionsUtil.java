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
import io.grpc.alts.internal.RpcProtocolVersions.Version;
import javax.annotation.Nullable;

/** Utility class for Rpc Protocol Versions. */
public final class RpcProtocolVersionsUtil {

  private static final int MAX_RPC_VERSION_MAJOR = 2;
  private static final int MAX_RPC_VERSION_MINOR = 1;
  private static final int MIN_RPC_VERSION_MAJOR = 2;
  private static final int MIN_RPC_VERSION_MINOR = 1;
  private static final RpcProtocolVersions RPC_PROTOCOL_VERSIONS =
      RpcProtocolVersions.newBuilder()
          .setMaxRpcVersion(
              RpcProtocolVersions.Version.newBuilder()
                  .setMajor(MAX_RPC_VERSION_MAJOR)
                  .setMinor(MAX_RPC_VERSION_MINOR)
                  .build())
          .setMinRpcVersion(
              RpcProtocolVersions.Version.newBuilder()
                  .setMajor(MIN_RPC_VERSION_MAJOR)
                  .setMinor(MIN_RPC_VERSION_MINOR)
                  .build())
          .build();

  /** Returns default Rpc Protocol Versions. */
  public static RpcProtocolVersions getRpcProtocolVersions() {
    return RPC_PROTOCOL_VERSIONS;
  }

  /**
   * Returns true if first Rpc Protocol Version is greater than or equal to the second one. Returns
   * false otherwise.
   */
  @VisibleForTesting
  static boolean isGreaterThanOrEqualTo(Version first, Version second) {
    if ((first.getMajor() > second.getMajor())
        || (first.getMajor() == second.getMajor() && first.getMinor() >= second.getMinor())) {
      return true;
    }
    return false;
  }

  /**
   * Performs check between local and peer Rpc Protocol Versions. This function returns true and the
   * highest common version if there exists a common Rpc Protocol Version to use, and returns false
   * and null otherwise.
   */
  static RpcVersionsCheckResult checkRpcProtocolVersions(
      RpcProtocolVersions localVersions, RpcProtocolVersions peerVersions) {
    Version maxCommonVersion;
    Version minCommonVersion;
    // maxCommonVersion is MIN(local.max, peer.max)
    if (isGreaterThanOrEqualTo(localVersions.getMaxRpcVersion(), peerVersions.getMaxRpcVersion())) {
      maxCommonVersion = peerVersions.getMaxRpcVersion();
    } else {
      maxCommonVersion = localVersions.getMaxRpcVersion();
    }
    // minCommonVersion is MAX(local.min, peer.min)
    if (isGreaterThanOrEqualTo(localVersions.getMinRpcVersion(), peerVersions.getMinRpcVersion())) {
      minCommonVersion = localVersions.getMinRpcVersion();
    } else {
      minCommonVersion = peerVersions.getMinRpcVersion();
    }
    if (isGreaterThanOrEqualTo(maxCommonVersion, minCommonVersion)) {
      return new RpcVersionsCheckResult.Builder()
          .setResult(true)
          .setHighestCommonVersion(maxCommonVersion)
          .build();
    }
    return new RpcVersionsCheckResult.Builder().setResult(false).build();
  }

  /** Wrapper class that stores results of Rpc Protocol Versions check. */
  static final class RpcVersionsCheckResult {
    private final boolean result;
    @Nullable private final Version highestCommonVersion;

    private RpcVersionsCheckResult(Builder builder) {
      result = builder.result;
      highestCommonVersion = builder.highestCommonVersion;
    }

    boolean getResult() {
      return result;
    }

    Version getHighestCommonVersion() {
      return highestCommonVersion;
    }

    static final class Builder {
      private boolean result;
      @Nullable private Version highestCommonVersion = null;

      public Builder setResult(boolean result) {
        this.result = result;
        return this;
      }

      public Builder setHighestCommonVersion(Version highestCommonVersion) {
        this.highestCommonVersion = highestCommonVersion;
        return this;
      }

      public RpcVersionsCheckResult build() {
        return new RpcVersionsCheckResult(this);
      }
    }
  }
}
