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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.Objects;
import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.List;
import javax.annotation.concurrent.Immutable;

/**
 * A group of {@link ResolvedServerInfo}s that is returned from a {@link NameResolver}.
 *
 * @deprecated This class will be removed. Use {@link EquivalentAddressGroup} instead.
 */
@Deprecated
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1770")
@Immutable
public final class ResolvedServerInfoGroup {
  private final List<ResolvedServerInfo> resolvedServerInfoList;
  private final Attributes attributes;

  /**
   * Constructs a new resolved server info group from {@link ResolvedServerInfo} list,
   * with custom {@link Attributes} attached to it.
   *
   * @param resolvedServerInfoList list of resolved server info objects.
   * @param attributes custom attributes for a given group.
   */
  private ResolvedServerInfoGroup(List<ResolvedServerInfo> resolvedServerInfoList,
      Attributes attributes) {
    checkArgument(!resolvedServerInfoList.isEmpty(), "empty server list");
    this.resolvedServerInfoList =
        Collections.unmodifiableList(new ArrayList<ResolvedServerInfo>(resolvedServerInfoList));
    this.attributes = checkNotNull(attributes, "attributes");
  }

  /**
   * Returns immutable list of {@link ResolvedServerInfo} objects for this group.
   */
  public List<ResolvedServerInfo> getResolvedServerInfoList() {
    return resolvedServerInfoList;
  }

  /**
   * Returns {@link Attributes} for this group.
   */
  public Attributes getAttributes() {
    return attributes;
  }

  /**
   * Converts this group to {@link EquivalentAddressGroup} object.
   */
  public EquivalentAddressGroup toEquivalentAddressGroup() {
    List<SocketAddress> addrs = new ArrayList<SocketAddress>(resolvedServerInfoList.size());
    for (ResolvedServerInfo resolvedServerInfo : resolvedServerInfoList) {
      addrs.add(resolvedServerInfo.getAddress());
    }
    return new EquivalentAddressGroup(addrs, attributes);
  }

  /**
   * Creates a new builder.
   */
  public static Builder builder() {
    return new Builder();
  }

  /**
   * Creates a new builder for a group with extra attributes.
   */
  public static Builder builder(Attributes attributes) {
    return new Builder(attributes);
  }

  /**
   * Returns true if the given object is also a {@link ResolvedServerInfoGroup} with an equal
   * attributes and list of {@link ResolvedServerInfo} objects.
   *
   * @param o an object.
   * @return true if the given object is a {@link ResolvedServerInfoGroup} with an equal attributes
   *     and server info list.
   */
  @Override
  public boolean equals(Object o) {
    if (this == o) {
      return true;
    }
    if (o == null || getClass() != o.getClass()) {
      return false;
    }
    ResolvedServerInfoGroup that = (ResolvedServerInfoGroup) o;
    return Objects.equal(resolvedServerInfoList, that.resolvedServerInfoList)
        && Objects.equal(attributes, that.attributes);
  }

  /**
   * Returns a hash code for the resolved server info group.
   *
   * <p>Note that if a resolver includes mutable values in the attributes, this object's hash code
   * could change over time. So care must be used when putting these objects into a set or using
   * them as keys for a map.
   *
   * @return a hash code for the server info group.
   */
  @Override
  public int hashCode() {
    return Objects.hashCode(resolvedServerInfoList, attributes);
  }

  @Override
  public String toString() {
    return "[servers=" + resolvedServerInfoList + ", attrs=" + attributes + "]";
  }

  /**
   * Builder for a {@link ResolvedServerInfo}.
   */
  @Deprecated
  public static final class Builder {
    private final List<ResolvedServerInfo> group = new ArrayList<ResolvedServerInfo>();
    private final Attributes attributes;

    public Builder(Attributes attributes) {
      this.attributes = attributes;
    }

    public Builder() {
      this(Attributes.EMPTY);
    }

    public Builder add(ResolvedServerInfo resolvedServerInfo) {
      group.add(resolvedServerInfo);
      return this;
    }

    public Builder addAll(Collection<ResolvedServerInfo> resolvedServerInfo) {
      group.addAll(resolvedServerInfo);
      return this;
    }

    public ResolvedServerInfoGroup build() {
      return new ResolvedServerInfoGroup(group, attributes);
    }
  }
}
