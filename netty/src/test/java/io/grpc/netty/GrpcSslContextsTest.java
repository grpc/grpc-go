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

package io.grpc.netty;

import static org.junit.Assert.assertTrue;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link io.grpc.netty.GrpcSslContexts}. */
@RunWith(JUnit4.class)
public class GrpcSslContextsTest {
  @Test public void selectApplicationProtocolConfig_grpcExp() {
    assertTrue(
        GrpcSslContexts.NEXT_PROTOCOL_VERSIONS.indexOf("grpc-exp") == -1
            || GrpcSslContexts.NEXT_PROTOCOL_VERSIONS.indexOf("grpc-exp")
                < GrpcSslContexts.NEXT_PROTOCOL_VERSIONS.indexOf("h2"));
  }
}
