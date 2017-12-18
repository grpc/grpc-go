/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

package io.grpc.internal;

import static org.junit.Assert.assertSame;

import io.grpc.ClientInterceptor;
import io.grpc.ReplacingClassLoader;
import io.grpc.ServerInterceptor;
import javax.annotation.Nullable;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link BinaryLogProvider}. */
@RunWith(JUnit4.class)
public class BinaryLogProviderTest {
  private final String serviceFile = "META-INF/services/io.grpc.internal.BinaryLogProvider";

  @Test
  public void noProvider() {
    assertSame(BinaryLogProvider.NullProvider.class, BinaryLogProvider.provider().getClass());
  }

  @Test
  public void multipleProvider() {
    ClassLoader cl = new ReplacingClassLoader(getClass().getClassLoader(), serviceFile,
        "io/grpc/internal/BinaryLogProviderTest-multipleProvider.txt");
    assertSame(Provider7.class, BinaryLogProvider.load(cl).getClass());
  }

  @Test
  public void unavailableProvider() {
    ClassLoader cl = new ReplacingClassLoader(getClass().getClassLoader(), serviceFile,
        "io/grpc/internal/BinaryLogProviderTest-unavailableProvider.txt");
    assertSame(BinaryLogProvider.NullProvider.class, BinaryLogProvider.load(cl).getClass());
  }

  public static final class Provider0 extends BaseProvider {
    public Provider0() {
      super(0);
    }
  }

  public static final class Provider5 extends BaseProvider {
    public Provider5() {
      super(5);
    }
  }

  public static final class Provider7 extends BaseProvider {
    public Provider7() {
      super(7);
    }
  }

  public static class BaseProvider extends BinaryLogProvider {
    private int priority;

    BaseProvider(int priority) {
      this.priority = priority;
    }

    @Nullable
    @Override
    public ServerInterceptor getServerInterceptor(String fullMethodName) {
      throw new UnsupportedOperationException();
    }

    @Nullable
    @Override
    public ClientInterceptor getClientInterceptor(String fullMethodName) {
      throw new UnsupportedOperationException();
    }

    @Override
    protected int priority() {
      return priority;
    }
  }
}
