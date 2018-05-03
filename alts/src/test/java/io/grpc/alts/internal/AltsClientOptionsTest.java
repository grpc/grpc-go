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

import static com.google.common.truth.Truth.assertThat;

import io.grpc.alts.internal.TransportSecurityCommon.RpcProtocolVersions;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link AltsClientOptions}. */
@RunWith(JUnit4.class)
public final class AltsClientOptionsTest {

  @Test
  public void setAndGet() throws Exception {
    String targetName = "foo";
    String serviceAccount1 = "bar1";
    String serviceAccount2 = "bar2";
    RpcProtocolVersions rpcVersions =
        RpcProtocolVersions.newBuilder()
            .setMaxRpcVersion(
                RpcProtocolVersions.Version.newBuilder().setMajor(2).setMinor(1).build())
            .setMinRpcVersion(
                RpcProtocolVersions.Version.newBuilder().setMajor(2).setMinor(1).build())
            .build();

    AltsClientOptions options =
        new AltsClientOptions.Builder()
            .setTargetName(targetName)
            .addTargetServiceAccount(serviceAccount1)
            .addTargetServiceAccount(serviceAccount2)
            .setRpcProtocolVersions(rpcVersions)
            .build();

    assertThat(options.getTargetName()).isEqualTo(targetName);
    assertThat(options.getTargetServiceAccounts()).containsAllOf(serviceAccount1, serviceAccount2);
    assertThat(options.getRpcProtocolVersions()).isEqualTo(rpcVersions);
  }
}
