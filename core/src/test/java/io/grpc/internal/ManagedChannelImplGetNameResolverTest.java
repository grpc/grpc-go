/*
 * Copyright 2015, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.fail;

import io.grpc.Attributes;
import io.grpc.NameResolver;
import io.grpc.NameResolver.Factory;
import java.net.URI;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ManagedChannelImpl#getNameResolver}. */
@RunWith(JUnit4.class)
public class ManagedChannelImplGetNameResolverTest {
  private static final Attributes NAME_RESOLVER_PARAMS =
      Attributes.newBuilder().set(NameResolver.Factory.PARAMS_DEFAULT_PORT, 447).build();

  @Test
  public void invalidUriTarget() {
    testInvalidTarget("defaultscheme:///[invalid]");
  }

  @Test
  public void validTargetWithInvalidDnsName() throws Exception {
    testValidTarget("[valid]", "defaultscheme:///%5Bvalid%5D",
        new URI("defaultscheme", "", "/[valid]", null));
  }

  @Test
  public void validAuthorityTarget() throws Exception {
    testValidTarget("foo.googleapis.com:8080", "defaultscheme:///foo.googleapis.com:8080",
        new URI("defaultscheme", "", "/foo.googleapis.com:8080", null));
  }

  @Test
  public void validUriTarget() throws Exception {
    testValidTarget("scheme:///foo.googleapis.com:8080", "scheme:///foo.googleapis.com:8080",
        new URI("scheme", "", "/foo.googleapis.com:8080", null));
  }

  @Test
  public void validIpv4AuthorityTarget() throws Exception {
    testValidTarget("127.0.0.1:1234", "defaultscheme:///127.0.0.1:1234",
        new URI("defaultscheme", "", "/127.0.0.1:1234", null));
  }

  @Test
  public void validIpv4UriTarget() throws Exception {
    testValidTarget("dns:///127.0.0.1:1234", "dns:///127.0.0.1:1234",
        new URI("dns", "", "/127.0.0.1:1234", null));
  }

  @Test
  public void validIpv6AuthorityTarget() throws Exception {
    testValidTarget("[::1]:1234", "defaultscheme:///%5B::1%5D:1234",
        new URI("defaultscheme", "", "/[::1]:1234", null));
  }

  @Test
  public void invalidIpv6UriTarget() throws Exception {
    testInvalidTarget("dns:///[::1]:1234");
  }

  @Test
  public void validIpv6UriTarget() throws Exception {
    testValidTarget("dns:///%5B::1%5D:1234", "dns:///%5B::1%5D:1234",
        new URI("dns", "", "/[::1]:1234", null));
  }

  @Test
  public void validTargetStartingWithSlash() throws Exception {
    testValidTarget("/target", "defaultscheme:////target",
        new URI("defaultscheme", "", "//target", null));
  }

  @Test
  public void validTargetNoResovler() {
    Factory nameResolverFactory = new NameResolver.Factory() {
      @Override
      public NameResolver newNameResolver(URI targetUri, Attributes params) {
        return null;
      }

      @Override
      public String getDefaultScheme() {
        return "defaultscheme";
      }
    };
    try {
      ManagedChannelImpl.getNameResolver(
          "foo.googleapis.com:8080", nameResolverFactory, NAME_RESOLVER_PARAMS);
      fail("Should fail");
    } catch (IllegalArgumentException e) {
      // expected
    }
  }

  private void testValidTarget(String target, String expectedUriString, URI expectedUri) {
    Factory nameResolverFactory = new FakeNameResolverFactory(expectedUri.getScheme());
    FakeNameResolver nameResolver = (FakeNameResolver) ManagedChannelImpl.getNameResolver(
        target, nameResolverFactory, NAME_RESOLVER_PARAMS);
    assertNotNull(nameResolver);
    assertEquals(expectedUri, nameResolver.uri);
    assertEquals(expectedUriString, nameResolver.uri.toString());
  }

  private void testInvalidTarget(String target) {
    Factory nameResolverFactory = new FakeNameResolverFactory("dns");

    try {
      FakeNameResolver nameResolver = (FakeNameResolver) ManagedChannelImpl.getNameResolver(
          target, nameResolverFactory, NAME_RESOLVER_PARAMS);
      fail("Should have failed, but got resolver with " + nameResolver.uri);
    } catch (IllegalArgumentException e) {
      // expected
    }
  }

  private static class FakeNameResolverFactory extends NameResolver.Factory {
    final String expectedScheme;

    FakeNameResolverFactory(String expectedScheme) {
      this.expectedScheme = expectedScheme;
    }

    @Override
    public NameResolver newNameResolver(URI targetUri, Attributes params) {
      if (expectedScheme.equals(targetUri.getScheme())) {
        return new FakeNameResolver(targetUri);
      }
      return null;
    }

    @Override
    public String getDefaultScheme() {
      return expectedScheme;
    }
  }

  private static class FakeNameResolver extends NameResolver {
    final URI uri;

    FakeNameResolver(URI uri) {
      this.uri = uri;
    }

    @Override public String getServiceAuthority() {
      return uri.getAuthority();
    }

    @Override public void start(final Listener listener) {}

    @Override public void shutdown() {}
  }
}
