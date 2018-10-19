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

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link AltsHandshakerOptions}. */
@RunWith(JUnit4.class)
public final class AltsHandshakerOptionsTest {

  @Test
  public void setAndGet() throws Exception {
    RpcProtocolVersions rpcVersions =
        RpcProtocolVersions.newBuilder()
            .setMaxRpcVersion(
                RpcProtocolVersions.Version.newBuilder().setMajor(2).setMinor(1).build())
            .setMinRpcVersion(
                RpcProtocolVersions.Version.newBuilder().setMajor(2).setMinor(1).build())
            .build();

    AltsHandshakerOptions options = new AltsHandshakerOptions(rpcVersions);
    assertThat(options.getRpcProtocolVersions()).isEqualTo(rpcVersions);
  }
}
