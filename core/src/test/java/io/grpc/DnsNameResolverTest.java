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

package io.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.fail;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.net.URI;

/** Unit tests for {@link DnsNameResolver}. */
@RunWith(JUnit4.class)
public class DnsNameResolverTest {

  private DnsNameResolverFactory factory = DnsNameResolverFactory.getInstance();

  private static final int DEFAULT_PORT = 887;
  private static final Attributes NAME_RESOLVER_PARAMS =
      Attributes.newBuilder().set(NameResolver.Factory.PARAMS_DEFAULT_PORT, DEFAULT_PORT).build();

  @Test
  public void invalidDnsName() throws Exception {
    testInvalidUri(new URI("dns", null, "/[invalid]", null));
  }

  @Test
  public void validIpv6() throws Exception {
    testValidUri(new URI("dns", null, "/[::1]", null), "[::1]", DEFAULT_PORT);
  }

  @Test
  public void validDnsNameWithoutPort() throws Exception {
    testValidUri(new URI("dns", null, "/foo.googleapis.com", null),
        "foo.googleapis.com", DEFAULT_PORT);
  }

  @Test
  public void validDnsNameWithPort() throws Exception {
    testValidUri(new URI("dns", null, "/foo.googleapis.com:456", null),
        "foo.googleapis.com:456", 456);
  }

  private void testInvalidUri(URI uri) {
    try {
      factory.newNameResolver(uri, NAME_RESOLVER_PARAMS);
      fail("Should have failed");
    } catch (IllegalArgumentException e) {
      // expected
    }
  }

  private void testValidUri(URI uri, String exportedAuthority, int expectedPort) {
    DnsNameResolver resolver = (DnsNameResolver) factory.newNameResolver(uri, NAME_RESOLVER_PARAMS);
    assertNotNull(resolver);
    assertEquals(expectedPort, resolver.getPort());
    assertEquals(exportedAuthority, resolver.getServiceAuthority());
  }
}
