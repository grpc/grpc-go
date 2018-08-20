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

package io.grpc;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import io.grpc.internal.DnsNameResolverProvider;
import java.net.URI;
import java.util.Collections;
import java.util.Iterator;
import java.util.List;
import java.util.concurrent.Callable;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link NameResolverProvider}. */
@RunWith(JUnit4.class)
public class NameResolverProviderTest {
  private final URI uri = URI.create("dns:///localhost");
  private final Attributes attributes = Attributes.EMPTY;

  @Test
  public void getDefaultScheme_noProvider() {
    List<NameResolverProvider> providers = Collections.<NameResolverProvider>emptyList();
    NameResolver.Factory factory = NameResolverProvider.asFactory(providers);
    try {
      factory.getDefaultScheme();
      fail("Expected exception");
    } catch (RuntimeException ex) {
      assertTrue(ex.toString(), ex.getMessage().contains("No NameResolverProviders found"));
    }
  }

  @Test
  public void newNameResolver_providerReturnsNull() {
    List<NameResolverProvider> providers = Collections.<NameResolverProvider>singletonList(
        new BaseProvider(true, 5) {
          @Override
          public NameResolver newNameResolver(URI passedUri, Attributes passedAttributes) {
            assertSame(uri, passedUri);
            assertSame(attributes, passedAttributes);
            return null;
          }
        });
    assertNull(NameResolverProvider.asFactory(providers).newNameResolver(uri, attributes));
  }

  @Test
  public void newNameResolver_noProvider() {
    List<NameResolverProvider> providers = Collections.<NameResolverProvider>emptyList();
    NameResolver.Factory factory = NameResolverProvider.asFactory(providers);
    try {
      factory.newNameResolver(uri, attributes);
      fail("Expected exception");
    } catch (RuntimeException ex) {
      assertTrue(ex.toString(), ex.getMessage().contains("No NameResolverProviders found"));
    }
  }

  @Test
  public void baseProviders() {
    List<NameResolverProvider> providers = NameResolverProvider.providers();
    assertEquals(1, providers.size());
    assertSame(DnsNameResolverProvider.class, providers.get(0).getClass());
    assertEquals("dns", NameResolverProvider.asFactory().getDefaultScheme());
  }

  @Test
  public void getClassesViaHardcoded_classesPresent() throws Exception {
    List<Class<?>> classes = NameResolverProvider.getHardCodedClasses();
    assertThat(classes).hasSize(1);
    assertThat(classes.get(0).getName()).isEqualTo("io.grpc.internal.DnsNameResolverProvider");
  }

  @Test
  public void provided() {
    for (NameResolverProvider current
        : InternalServiceProviders.getCandidatesViaServiceLoader(
        NameResolverProvider.class, getClass().getClassLoader())) {
      if (current instanceof DnsNameResolverProvider) {
        return;
      }
    }
    fail("DnsNameResolverProvider not registered");
  }

  @Test
  public void providedHardCoded() {
    for (NameResolverProvider current : InternalServiceProviders.getCandidatesViaHardCoded(
        NameResolverProvider.class, NameResolverProvider.HARDCODED_CLASSES)) {
      if (current instanceof DnsNameResolverProvider) {
        return;
      }
    }
    fail("DnsNameResolverProvider not registered");
  }

  public static final class HardcodedClassesCallable implements Callable<Iterator<Class<?>>> {
    @Override
    public Iterator<Class<?>> call() {
      return NameResolverProvider.getHardCodedClasses().iterator();
    }
  }

  private static class BaseProvider extends NameResolverProvider {
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
    public NameResolver newNameResolver(URI targetUri, Attributes params) {
      throw new UnsupportedOperationException();
    }

    @Override
    public String getDefaultScheme() {
      return "scheme" + getClass().getSimpleName();
    }
  }
}
