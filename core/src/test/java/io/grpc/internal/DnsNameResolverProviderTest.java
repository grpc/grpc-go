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

package io.grpc.internal;

import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import io.grpc.NameResolver;
import io.grpc.ProxyDetector;
import io.grpc.SynchronizationContext;
import java.net.URI;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link DnsNameResolverProvider}. */
@RunWith(JUnit4.class)
public class DnsNameResolverProviderTest {
  private final SynchronizationContext syncContext = new SynchronizationContext(
      new Thread.UncaughtExceptionHandler() {
        @Override
        public void uncaughtException(Thread t, Throwable e) {
          throw new AssertionError(e);
        }
      });
  private final NameResolver.Helper helper = new NameResolver.Helper() {
      @Override
      public int getDefaultPort() {
        throw new UnsupportedOperationException("Should not be called");
      }

      @Override
      public ProxyDetector getProxyDetector() {
        return GrpcUtil.getDefaultProxyDetector();
      }

      @Override
      public SynchronizationContext getSynchronizationContext() {
        return syncContext;
      }
    };

  private DnsNameResolverProvider provider = new DnsNameResolverProvider();

  @Test
  public void isAvailable() {
    assertTrue(provider.isAvailable());
  }

  @Test
  public void newNameResolver() {
    assertSame(DnsNameResolver.class,
        provider.newNameResolver(URI.create("dns:///localhost:443"), helper).getClass());
    assertNull(
        provider.newNameResolver(URI.create("notdns:///localhost:443"), helper));
  }
}
