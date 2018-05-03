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
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import com.google.common.base.Defaults;
import io.grpc.ManagedChannel;
import io.grpc.alts.AltsChannelBuilder.AltsChannel;
import io.grpc.alts.internal.AltsClientOptions;
import io.grpc.alts.internal.AltsProtocolNegotiator;
import io.grpc.alts.internal.TransportSecurityCommon.RpcProtocolVersions;
import io.grpc.netty.InternalNettyChannelBuilder.TransportCreationParamsFilterFactory;
import io.grpc.netty.ProtocolNegotiator;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.net.InetSocketAddress;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class AltsChannelBuilderTest {

  @Test
  public void buildsNettyChannel() throws Exception {
    AltsChannelBuilder builder =
        AltsChannelBuilder.forTarget("localhost:8080").enableUntrustedAltsForTesting();

    TransportCreationParamsFilterFactory tcpfFactory = builder.getTcpfFactoryForTest();
    AltsClientOptions altsClientOptions = builder.getAltsClientOptionsForTest();

    assertThat(tcpfFactory).isNull();
    assertThat(altsClientOptions).isNull();

    ManagedChannel channel = builder.build();
    assertThat(channel).isInstanceOf(AltsChannel.class);

    tcpfFactory = builder.getTcpfFactoryForTest();
    altsClientOptions = builder.getAltsClientOptionsForTest();

    assertThat(tcpfFactory).isNotNull();
    ProtocolNegotiator protocolNegotiator =
        tcpfFactory
            .create(new InetSocketAddress(8080), "fakeAuthority", "fakeUserAgent", null)
            .getProtocolNegotiator();
    assertThat(protocolNegotiator).isInstanceOf(AltsProtocolNegotiator.class);

    assertThat(altsClientOptions).isNotNull();
    RpcProtocolVersions expectedVersions =
        RpcProtocolVersions.newBuilder()
            .setMaxRpcVersion(
                RpcProtocolVersions.Version.newBuilder().setMajor(2).setMinor(1).build())
            .setMinRpcVersion(
                RpcProtocolVersions.Version.newBuilder().setMajor(2).setMinor(1).build())
            .build();
    assertThat(altsClientOptions.getRpcProtocolVersions()).isEqualTo(expectedVersions);
  }

  @Test
  public void allAltsChannelMethodsForward() throws Exception {
    ManagedChannel mockDelegate = mock(ManagedChannel.class);
    AltsChannel altsChannel = new AltsChannel(mockDelegate);

    for (Method method : ManagedChannel.class.getDeclaredMethods()) {
      if (Modifier.isStatic(method.getModifiers()) || Modifier.isPrivate(method.getModifiers())) {
        continue;
      }
      Class<?>[] argTypes = method.getParameterTypes();
      Object[] args = new Object[argTypes.length];
      for (int i = 0; i < argTypes.length; i++) {
        args[i] = Defaults.defaultValue(argTypes[i]);
      }

      method.invoke(altsChannel, args);
      method.invoke(verify(mockDelegate), args);
    }
  }
}
