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

package io.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.mockito.Mockito.mock;

import io.grpc.internal.DnsNameResolverProvider;
import java.net.URI;
import java.util.Collections;
import java.util.List;
import java.util.ServiceConfigurationError;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link NameResolverProvider}. */
@RunWith(JUnit4.class)
public class NameResolverProviderTest {
  private final String serviceFile = "META-INF/services/io.grpc.NameResolverProvider";
  private final URI uri = URI.create("dns:///localhost");
  private final Attributes attributes = Attributes.EMPTY;

  @Test
  public void noProvider() {
    ClassLoader cl = new ReplacingClassLoader(
        getClass().getClassLoader(), serviceFile,
        "io/grpc/NameResolverProviderTest-doesNotExist.txt");
    List<NameResolverProvider> providers = NameResolverProvider.load(cl);
    assertEquals(Collections.<NameResolverProvider>emptyList(), providers);
  }

  @Test
  public void multipleProvider() {
    ClassLoader cl = new ReplacingClassLoader(
        getClass().getClassLoader(), serviceFile,
        "io/grpc/NameResolverProviderTest-multipleProvider.txt");
    List<NameResolverProvider> providers = NameResolverProvider.load(cl);
    assertEquals(3, providers.size());
    assertSame(Available7Provider.class, providers.get(0).getClass());
    assertSame(Available5Provider.class, providers.get(1).getClass());
    assertSame(Available0Provider.class, providers.get(2).getClass());
    assertEquals("schemeAvailable7Provider",
        NameResolverProvider.asFactory(providers).getDefaultScheme());
    assertSame(Available7Provider.nameResolver,
        NameResolverProvider.asFactory(providers).newNameResolver(uri, attributes));
  }

  @Test
  public void unavailableProvider() {
    ClassLoader cl = new ReplacingClassLoader(
        getClass().getClassLoader(), serviceFile,
        "io/grpc/NameResolverProviderTest-unavailableProvider.txt");
    assertEquals(Collections.<NameResolverProvider>emptyList(), NameResolverProvider.load(cl));
  }

  @Test
  public void getDefaultScheme_noProvider() {
    List<NameResolverProvider> providers = Collections.<NameResolverProvider>emptyList();
    NameResolver.Factory factory = NameResolverProvider.asFactory(providers);
    try {
      factory.getDefaultScheme();
      fail("Expected exception");
    } catch (IllegalStateException ex) {
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
    } catch (IllegalStateException ex) {
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
  public void getCandidatesViaHardCoded_usesProvidedClassLoader() {
    final RuntimeException toThrow = new RuntimeException();
    try {
      NameResolverProvider.getCandidatesViaHardCoded(new ClassLoader() {
        @Override
        public Class<?> loadClass(String name) {
          throw toThrow;
        }
      });
      fail("Expected exception");
    } catch (RuntimeException ex) {
      assertSame(toThrow, ex);
    }
  }

  @Test
  public void getCandidatesViaHardCoded_ignoresMissingClasses() {
    Iterable<NameResolverProvider> i =
        NameResolverProvider.getCandidatesViaHardCoded(new ClassLoader() {
          @Override
          public Class<?> loadClass(String name) throws ClassNotFoundException {
            throw new ClassNotFoundException();
          }
        });
    assertFalse("Iterator should be empty", i.iterator().hasNext());
  }

  @Test
  public void create_throwsErrorOnMisconfiguration() throws Exception {
    class PrivateClass {}

    try {
      NameResolverProvider.create(PrivateClass.class);
      fail("Expected exception");
    } catch (ServiceConfigurationError e) {
      assertTrue("Expected ClassCastException cause: " + e.getCause(),
          e.getCause() instanceof ClassCastException);
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
    public static final NameResolver nameResolver = mock(NameResolver.class);

    public Available7Provider() {
      super(true, 7);
    }

    @Override
    public NameResolver newNameResolver(URI targetUri, Attributes params) {
      return nameResolver;
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

