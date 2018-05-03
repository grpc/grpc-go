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

package io.grpc.okhttp;

import static io.grpc.okhttp.OkHttpTlsUpgrader.canonicalizeHost;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import io.grpc.okhttp.internal.Protocol;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link io.grpc.okhttp.OkHttpTlsUpgrader}. */
@RunWith(JUnit4.class)
public class OkHttpTlsUpgraderTest {
  @Test public void upgrade_grpcExp() {
    assertTrue(
        OkHttpTlsUpgrader.TLS_PROTOCOLS.indexOf(Protocol.GRPC_EXP) == -1
            || OkHttpTlsUpgrader.TLS_PROTOCOLS.indexOf(Protocol.GRPC_EXP)
                < OkHttpTlsUpgrader.TLS_PROTOCOLS.indexOf(Protocol.HTTP_2));
  }

  @Test public void canonicalizeHosts() {
    assertEquals("::1", canonicalizeHost("::1"));
    assertEquals("::1", canonicalizeHost("[::1]"));
    assertEquals("127.0.0.1", canonicalizeHost("127.0.0.1"));
    assertEquals("some.long.url.com", canonicalizeHost("some.long.url.com"));

    // Extra square brackets in a malformed URI are retained
    assertEquals("[::1]", canonicalizeHost("[[::1]]"));
  }
}
