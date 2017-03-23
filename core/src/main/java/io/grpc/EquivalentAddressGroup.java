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

import com.google.common.base.Preconditions;
import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * A group of {@link SocketAddress}es that are considered equivalent when channel makes connections.
 *
 * <p>Usually the addresses are addresses resolved from the same host name, and connecting to any of
 * them is equally sufficient. They do have order. An address appears earlier on the list is likely
 * to be tried earlier.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
public final class EquivalentAddressGroup {

  private final List<SocketAddress> addrs;
  private final Attributes attrs;

  /**
   * {@link SocketAddress} docs say that the addresses are immutable, so we cache the hashCode.
   */
  private final int hashCode;

  /**
   * List constructor without {@link Attributes}.
   */
  public EquivalentAddressGroup(List<SocketAddress> addrs) {
    this(addrs, Attributes.EMPTY);
  }

  /**
   * List constructor with {@link Attributes}.
   */
  public EquivalentAddressGroup(List<SocketAddress> addrs, Attributes attrs) {
    Preconditions.checkArgument(!addrs.isEmpty(), "addrs is empty");
    this.addrs = Collections.unmodifiableList(new ArrayList<SocketAddress>(addrs));
    this.attrs = Preconditions.checkNotNull(attrs, "attrs");
    // Attributes may contain mutable objects, which means Attributes' hashCode may change over
    // time, thus we don't cache Attributes' hashCode.
    hashCode = this.addrs.hashCode();
  }

  /**
   * Singleton constructor without Attributes.
   */
  public EquivalentAddressGroup(SocketAddress addr) {
    this(addr, Attributes.EMPTY);
  }

  /**
   * Singleton constructor with Attributes.
   */
  public EquivalentAddressGroup(SocketAddress addr, Attributes attrs) {
    this(Collections.singletonList(addr), attrs);
  }

  /**
   * Returns an immutable list of the addresses.
   */
  public List<SocketAddress> getAddresses() {
    return addrs;
  }

  /**
   * Returns the attributes.
   */
  public Attributes getAttributes() {
    return attrs;
  }

  @Override
  public String toString() {
    return "[addrs=" + addrs + ", attrs=" + attrs + "]";
  }

  @Override
  public int hashCode() {
    // Avoids creating an iterator on the underlying array list.
    return hashCode;
  }

  /**
   * Returns true if the given object is also an {@link EquivalentAddressGroup} with an equal
   * address list and equal attribute values.
   *
   * <p>Note that if the attributes include mutable values, it is possible for two objects to be
   * considered equal at one point in time and not equal at another (due to concurrent mutation of
   * attribute values).
   */
  @Override
  public boolean equals(Object other) {
    if (!(other instanceof EquivalentAddressGroup)) {
      return false;
    }
    EquivalentAddressGroup that = (EquivalentAddressGroup) other;
    if (addrs.size() != that.addrs.size()) {
      return false;
    }
    // Avoids creating an iterator on the underlying array list.
    for (int i = 0; i < addrs.size(); i++) {
      if (!addrs.get(i).equals(that.addrs.get(i))) {
        return false;
      }
    }
    if (!attrs.equals(that.attrs)) {
      return false;
    }
    return true;
  }
}
