/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

package io.grpc;

import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ServerProvider}. */
@RunWith(JUnit4.class)
public class ServerProviderTest {
  private final String serviceFile = "META-INF/services/io.grpc.ServerProvider";

  @Test(expected = ManagedChannelProvider.ProviderNotFoundException.class)
  public void noProvider() {
    ServerProvider.provider();
  }

  @Test
  public void multipleProvider() {
    ClassLoader cl = new ReplacingClassLoader(
        getClass().getClassLoader(), serviceFile,
        "io/grpc/ServerProviderTest-multipleProvider.txt");
    assertSame(Available7Provider.class, ServerProvider.load(cl).getClass());
  }

  @Test
  public void unavailableProvider() {
    ClassLoader cl = new ReplacingClassLoader(
        getClass().getClassLoader(), serviceFile,
        "io/grpc/ServerProviderTest-unavailableProvider.txt");
    assertNull(ServerProvider.load(cl));
  }

  @Test
  public void contextClassLoaderProvider() {
    ClassLoader ccl = Thread.currentThread().getContextClassLoader();
    try {
      ClassLoader cl = new ReplacingClassLoader(
              getClass().getClassLoader(), serviceFile,
              "io/grpc/ServerProviderTest-empty.txt");

      // test that the context classloader is used as fallback
      ClassLoader rcll = new ReplacingClassLoader(
          getClass().getClassLoader(), serviceFile,
          "io/grpc/ServerProviderTest-multipleProvider.txt");
      Thread.currentThread().setContextClassLoader(rcll);
      assertSame(Available7Provider.class, ServerProvider.load(cl).getClass());
    } finally {
      Thread.currentThread().setContextClassLoader(ccl);
    }
  }

  private static class BaseProvider extends ServerProvider {
    private final boolean isAvailable;
    private final int priority;

    public BaseProvider(boolean isAvailable, int priority) {
      this.isAvailable = isAvailable;
      this.priority = priority;
    }

    @Override
    protected boolean isAvailable() {
      return isAvailable;
    }

    @Override
    protected int priority() {
      return priority;
    }

    @Override
    protected ServerBuilder<?> builderForPort(int port) {
      throw new UnsupportedOperationException();
    }
  }

  public static class Available0Provider extends BaseProvider {
    public Available0Provider() {
      super(true, 0);
    }
  }

  public static class Available5Provider extends BaseProvider {
    public Available5Provider() {
      super(true, 5);
    }
  }

  public static class Available7Provider extends BaseProvider {
    public Available7Provider() {
      super(true, 7);
    }
  }

  public static class UnavailableProvider extends BaseProvider {
    public UnavailableProvider() {
      super(false, 10);
    }

    @Override
    protected int priority() {
      throw new RuntimeException("purposefully broken");
    }
  }
}
