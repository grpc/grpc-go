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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import io.grpc.alts.internal.RpcProtocolVersions.Version;
import io.grpc.alts.internal.RpcProtocolVersionsUtil.RpcVersionsCheckResult;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link RpcProtocolVersionsUtil}. */
@RunWith(JUnit4.class)
public final class RpcProtocolVersionsUtilTest {

  @Test
  public void compareVersions() throws Exception {
    assertTrue(
        RpcProtocolVersionsUtil.isGreaterThanOrEqualTo(
            Version.newBuilder().setMajor(3).setMinor(2).build(),
            Version.newBuilder().setMajor(2).setMinor(1).build()));
    assertTrue(
        RpcProtocolVersionsUtil.isGreaterThanOrEqualTo(
            Version.newBuilder().setMajor(3).setMinor(2).build(),
            Version.newBuilder().setMajor(2).setMinor(1).build()));
    assertTrue(
        RpcProtocolVersionsUtil.isGreaterThanOrEqualTo(
            Version.newBuilder().setMajor(3).setMinor(2).build(),
            Version.newBuilder().setMajor(3).setMinor(2).build()));
    assertFalse(
        RpcProtocolVersionsUtil.isGreaterThanOrEqualTo(
            Version.newBuilder().setMajor(2).setMinor(3).build(),
            Version.newBuilder().setMajor(3).setMinor(2).build()));
    assertFalse(
        RpcProtocolVersionsUtil.isGreaterThanOrEqualTo(
            Version.newBuilder().setMajor(3).setMinor(1).build(),
            Version.newBuilder().setMajor(3).setMinor(2).build()));
  }

  @Test
  public void checkRpcVersions() throws Exception {
    // local.max > peer.max and local.min > peer.min
    RpcVersionsCheckResult checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(3).setMinor(2).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .build());
    assertTrue(checkResult.getResult());
    assertEquals(
        Version.newBuilder().setMajor(2).setMinor(1).build(),
        checkResult.getHighestCommonVersion());
    // local.max > peer.max and local.min < peer.min
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(3).setMinor(2).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build());
    assertTrue(checkResult.getResult());
    assertEquals(
        Version.newBuilder().setMajor(2).setMinor(1).build(),
        checkResult.getHighestCommonVersion());
    // local.max > peer.max and local.min = peer.min
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(3).setMinor(2).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build());
    assertTrue(checkResult.getResult());
    assertEquals(
        Version.newBuilder().setMajor(2).setMinor(1).build(),
        checkResult.getHighestCommonVersion());
    // local.max < peer.max and local.min > peer.min
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(3).setMinor(2).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .build());
    assertTrue(checkResult.getResult());
    assertEquals(
        Version.newBuilder().setMajor(2).setMinor(1).build(),
        checkResult.getHighestCommonVersion());
    // local.max = peer.max and local.min > peer.min
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .build());
    assertTrue(checkResult.getResult());
    assertEquals(
        Version.newBuilder().setMajor(2).setMinor(1).build(),
        checkResult.getHighestCommonVersion());
    // local.max < peer.max and local.min < peer.min
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(3).setMinor(2).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build());
    assertTrue(checkResult.getResult());
    assertEquals(
        Version.newBuilder().setMajor(2).setMinor(1).build(),
        checkResult.getHighestCommonVersion());
    // local.max < peer.max and local.min = peer.min
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(3).setMinor(2).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .build());
    assertTrue(checkResult.getResult());
    assertEquals(
        Version.newBuilder().setMajor(2).setMinor(1).build(),
        checkResult.getHighestCommonVersion());
    // local.max = peer.max and local.min < peer.min
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .build());
    assertTrue(checkResult.getResult());
    assertEquals(
        Version.newBuilder().setMajor(2).setMinor(1).build(),
        checkResult.getHighestCommonVersion());
    // all equal
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build());
    assertTrue(checkResult.getResult());
    assertEquals(
        Version.newBuilder().setMajor(2).setMinor(1).build(),
        checkResult.getHighestCommonVersion());
    // max is smaller than min
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(1).setMinor(2).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build());
    assertFalse(checkResult.getResult());
    assertEquals(null, checkResult.getHighestCommonVersion());
    // no overlap, local > peer
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(6).setMinor(5).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(4).setMinor(3).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(0).setMinor(0).build())
                .build());
    assertFalse(checkResult.getResult());
    assertEquals(null, checkResult.getHighestCommonVersion());
    // no overlap, local < peer
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(1).setMinor(0).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(6).setMinor(5).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(4).setMinor(3).build())
                .build());
    assertFalse(checkResult.getResult());
    assertEquals(null, checkResult.getHighestCommonVersion());
    // no overlap, max < min
    checkResult =
        RpcProtocolVersionsUtil.checkRpcProtocolVersions(
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(4).setMinor(3).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(6).setMinor(5).build())
                .build(),
            RpcProtocolVersions.newBuilder()
                .setMaxRpcVersion(Version.newBuilder().setMajor(1).setMinor(0).build())
                .setMinRpcVersion(Version.newBuilder().setMajor(2).setMinor(1).build())
                .build());
    assertFalse(checkResult.getResult());
    assertEquals(null, checkResult.getHighestCommonVersion());
  }
}
