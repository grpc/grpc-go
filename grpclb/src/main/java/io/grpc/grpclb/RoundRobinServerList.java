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

package io.grpc.grpclb;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;
import com.google.common.collect.Iterables;
import com.google.common.util.concurrent.ListenableFuture;

import io.grpc.EquivalentAddressGroup;
import io.grpc.TransportManager;

import java.net.InetSocketAddress;
import java.util.Iterator;
import java.util.List;

import javax.annotation.Nullable;
import javax.annotation.concurrent.NotThreadSafe;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Manages a list of server addresses to round-robin on.
 */
// TODO(zhangkun83): possibly move it to io.grpc.internal, as it can also be used by the round-robin
// LoadBalancer.
@ThreadSafe
class RoundRobinServerList<T> {
  private final TransportManager<T> tm;
  private final List<EquivalentAddressGroup> list;
  private final Iterator<EquivalentAddressGroup> cyclingIter;

  private RoundRobinServerList(TransportManager<T> tm, List<EquivalentAddressGroup> list) {
    this.tm = tm;
    this.list = list;
    this.cyclingIter = Iterables.cycle(list).iterator();
  }

  ListenableFuture<T> getTransportForNextServer() {
    EquivalentAddressGroup currentServer;
    synchronized (cyclingIter) {
      // TODO(zhangkun83): receive transportShutdown and transportReady events, then skip addresses
      // that have been failing.
      currentServer = cyclingIter.next();
    }
    if (currentServer == null) {
      // TODO(zhangkun83): drop the request by returnning a fake transport that would fail
      // streams.
      throw new UnsupportedOperationException("server dropping not implemented yet");
    }
    return Preconditions.checkNotNull(tm.getTransport(currentServer),
        "TransportManager returned null for %s", currentServer);
  }

  @VisibleForTesting
  List<EquivalentAddressGroup> getList() {
    return list;
  }

  int size() {
    return list.size();
  }

  @NotThreadSafe
  static class Builder<T> {
    private final ImmutableList.Builder<EquivalentAddressGroup> listBuilder =
        ImmutableList.builder();
    private final TransportManager<T> tm;

    Builder(TransportManager<T> tm) {
      this.tm = tm;
    }

    /**
     * Adds a server to the list, or {@code null} for a drop entry.
     */
    void add(@Nullable InetSocketAddress addr) {
      listBuilder.add(new EquivalentAddressGroup(addr));
    }

    RoundRobinServerList<T> build() {
      return new RoundRobinServerList<T>(tm, listBuilder.build());
    }
  }
}
