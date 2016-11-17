/*
 * Copyright 2016, Google Inc. All rights reserved.
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
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.fail;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.net.InetSocketAddress;

@RunWith(JUnit4.class)
public class ResolvedServerInfoTest {
  private static final Attributes.Key<String> FOO = Attributes.Key.of("foo");
  private static final Attributes ATTRS = Attributes.newBuilder().set(FOO, "bar").build();

  @Test
  public void accessors() {
    InetSocketAddress addr = InetSocketAddress.createUnresolved("foo", 123);

    ResolvedServerInfo server = new ResolvedServerInfo(addr, ATTRS);
    assertEquals(addr, server.getAddress());
    assertEquals(ATTRS, server.getAttributes());

    // unspecified attributes treated as empty
    server = new ResolvedServerInfo(addr);
    assertEquals(addr, server.getAddress());
    assertEquals(Attributes.EMPTY, server.getAttributes());
  }

  @Test public void cannotUseNullAddress() {
    try {
      new ResolvedServerInfo(null, ATTRS);
      fail("Should not have been allowd to create info with null address");
    } catch (NullPointerException e) {
      // expected
    }
  }

  @Test public void equals_true() {
    ResolvedServerInfo server1 = new ResolvedServerInfo(
        InetSocketAddress.createUnresolved("foo", 123),
        Attributes.newBuilder().set(FOO, "bar").build());
    ResolvedServerInfo server2 = new ResolvedServerInfo(
        InetSocketAddress.createUnresolved("foo", 123),
        Attributes.newBuilder().set(FOO, "bar").build());

    // sanity checks that they're not same instances
    assertNotSame(server1.getAddress(), server2.getAddress());
    assertNotSame(server1.getAttributes(), server2.getAttributes());

    assertEquals(server1, server2);
    assertEquals(server1.hashCode(), server2.hashCode()); // hash code must be consistent

    // empty attributes
    server1 = new ResolvedServerInfo(InetSocketAddress.createUnresolved("foo", 123));
    server2 = new ResolvedServerInfo(
        InetSocketAddress.createUnresolved("foo", 123), Attributes.EMPTY);
    assertEquals(server1, server2);
    assertEquals(server1.hashCode(), server2.hashCode());
  }

  @Test public void equals_falseDifferentAddresses() {
    ResolvedServerInfo server1 = new ResolvedServerInfo(
        InetSocketAddress.createUnresolved("foo", 123),
        Attributes.newBuilder().set(FOO, "bar").build());
    ResolvedServerInfo server2 = new ResolvedServerInfo(
        InetSocketAddress.createUnresolved("foo", 456),
        Attributes.newBuilder().set(FOO, "bar").build());

    assertNotEquals(server1, server2);
    // hash code could collide, but this assertion is safe because, in this example, they do not
    assertNotEquals(server1.hashCode(), server2.hashCode());
  }

  @Test public void equals_falseDifferentAttributes() {
    ResolvedServerInfo server1 = new ResolvedServerInfo(
        InetSocketAddress.createUnresolved("foo", 123),
        Attributes.newBuilder().set(FOO, "bar").build());
    ResolvedServerInfo server2 = new ResolvedServerInfo(
        InetSocketAddress.createUnresolved("foo", 123),
        Attributes.newBuilder().set(FOO, "baz").build());

    assertNotEquals(server1, server2);
    // hash code could collide, but these assertions are safe because, in these examples, they don't
    assertNotEquals(server1.hashCode(), server2.hashCode());

    // same values but extra key? still not equal
    server2 = new ResolvedServerInfo(
        InetSocketAddress.createUnresolved("foo", 456),
        Attributes.newBuilder()
            .set(FOO, "bar")
            .set(Attributes.Key.of("fiz"), "buz")
            .build());

    assertNotEquals(server1, server2);
    assertNotEquals(server1.hashCode(), server2.hashCode());
  }
}
