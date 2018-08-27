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

package io.grpc.alts;

import static com.google.common.truth.Truth.assertThat;

import io.grpc.alts.internal.GoogleDefaultProtocolNegotiator;
import io.grpc.netty.InternalNettyChannelBuilder.TransportCreationParamsFilterFactory;
import io.grpc.netty.ProtocolNegotiator;
import java.net.InetSocketAddress;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class GoogleDefaultChannelBuilderTest {

  @Test
  public void buildsNettyChannel() throws Exception {
    GoogleDefaultChannelBuilder builder = GoogleDefaultChannelBuilder.forTarget("localhost:8080");

    TransportCreationParamsFilterFactory tcpfFactory = builder.getTcpfFactoryForTest();
    assertThat(tcpfFactory).isNotNull();
    ProtocolNegotiator protocolNegotiator =
        tcpfFactory
            .create(new InetSocketAddress(8080), "fakeAuthority", "fakeUserAgent", null)
            .getProtocolNegotiator();
    assertThat(protocolNegotiator).isInstanceOf(GoogleDefaultProtocolNegotiator.class);
  }
}
