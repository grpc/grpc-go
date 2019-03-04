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

package io.grpc.internal.testing;

import io.grpc.ChannelLogger;
import io.grpc.internal.ClientTransportFactory;
import java.net.InetSocketAddress;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public abstract class AbstractClientTransportFactoryTest {

  protected abstract ClientTransportFactory newClientTransportFactory();

  @Test
  public void multipleCallsToCloseShouldNotThrow() {
    ClientTransportFactory transportFactory = newClientTransportFactory();
    transportFactory.close();
    transportFactory.close();
    transportFactory.close();
  }

  @Test(expected = IllegalStateException.class)
  public void newClientTransportAfterCloseShouldThrow() {
    ClientTransportFactory transportFactory = newClientTransportFactory();
    transportFactory.close();
    transportFactory.newClientTransport(
        new InetSocketAddress("localhost", 12345),
        new ClientTransportFactory.ClientTransportOptions(),
        new ChannelLogger() {

          @Override
          public void log(ChannelLogLevel level, String message) {}

          @Override
          public void log(ChannelLogLevel level, String messageFormat, Object... args) {}
        }
    );
  }
}
