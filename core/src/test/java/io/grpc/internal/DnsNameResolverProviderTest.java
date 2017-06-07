/*
 * Copyright 2016, gRPC Authors All rights reserved.
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
import static org.junit.Assert.fail;

import io.grpc.Attributes;
import io.grpc.NameResolverProvider;
import java.net.URI;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link DnsNameResolverProvider}. */
@RunWith(JUnit4.class)
public class DnsNameResolverProviderTest {
  private DnsNameResolverProvider provider = new DnsNameResolverProvider();

  @Test
  public void provided() {
    for (NameResolverProvider current
        : NameResolverProvider.getCandidatesViaServiceLoader(getClass().getClassLoader())) {
      if (current instanceof DnsNameResolverProvider) {
        return;
      }
    }
    fail("DnsNameResolverProvider not registered");
  }

  @Test
  public void providedHardCoded() {
    for (NameResolverProvider current
        : NameResolverProvider.getCandidatesViaHardCoded(getClass().getClassLoader())) {
      if (current instanceof DnsNameResolverProvider) {
        return;
      }
    }
    fail("DnsNameResolverProvider not registered");
  }

  @Test
  public void isAvailable() {
    assertTrue(provider.isAvailable());
  }

  @Test
  public void newNameResolver() {
    assertSame(DnsNameResolver.class,
        provider.newNameResolver(URI.create("dns:///localhost:443"), Attributes.EMPTY).getClass());
    assertNull(
        provider.newNameResolver(URI.create("notdns:///localhost:443"), Attributes.EMPTY));
  }
}
