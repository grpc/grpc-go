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

import com.google.common.annotations.VisibleForTesting;

import io.grpc.EquivalentAddressGroup;
import io.grpc.Status;
import io.grpc.TransportManager;

import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.Iterator;
import java.util.List;
import java.util.NoSuchElementException;

import javax.annotation.Nullable;
import javax.annotation.concurrent.NotThreadSafe;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Manages a list of server addresses to round-robin on.
 */
@ThreadSafe
public class RoundRobinServerList<T> {
  private final TransportManager<T> tm;
  private final List<EquivalentAddressGroup> list;
  private final Iterator<EquivalentAddressGroup> cyclingIter;
  private final T requestDroppingTransport;

  private RoundRobinServerList(TransportManager<T> tm, List<EquivalentAddressGroup> list) {
    this.tm = tm;
    this.list = list;
    this.cyclingIter = new CycleIterator<EquivalentAddressGroup>(list);
    this.requestDroppingTransport =
      tm.createFailingTransport(Status.UNAVAILABLE.withDescription("Throttled by LB"));
  }

  /**
   * Returns the next transport in the list of servers.
   *
   * @return the next transport
   */
  public T getTransportForNextServer() {
    EquivalentAddressGroup currentServer;
    synchronized (cyclingIter) {
      // TODO(zhangkun83): receive transportShutdown and transportReady events, then skip addresses
      // that have been failing.
      currentServer = cyclingIter.next();
    }
    if (currentServer == null) {
      return requestDroppingTransport;
    }
    return tm.getTransport(currentServer);
  }

  @VisibleForTesting
  public List<EquivalentAddressGroup> getList() {
    return list;
  }

  public int size() {
    return list.size();
  }

  @NotThreadSafe
  public static class Builder<T> {
    private final List<EquivalentAddressGroup> list = new ArrayList<EquivalentAddressGroup>();
    private final TransportManager<T> tm;

    public Builder(TransportManager<T> tm) {
      this.tm = tm;
    }

    /**
     * Adds a server to the list, or {@code null} for a drop entry.
     */
    public Builder<T> addSocketAddress(@Nullable SocketAddress address) {
      list.add(new EquivalentAddressGroup(address));
      return this;
    }

    /**
     * Adds a address group to the list.
     *
     * @param addresses the addresses to add
     */
    public Builder<T> add(EquivalentAddressGroup addresses) {
      list.add(addresses);
      return this;
    }

    /**
     * Adds a list of address groups.
     *
     * @param addresses the list of addresses group.
     */
    public Builder<T> addAll(Collection<EquivalentAddressGroup> addresses) {
      list.addAll(addresses);
      return this;
    }

    public RoundRobinServerList<T> build() {
      return new RoundRobinServerList<T>(tm,
          Collections.unmodifiableList(new ArrayList<EquivalentAddressGroup>(list)));
    }
  }

  private static final class CycleIterator<T> implements Iterator<T> {
    private final List<T> list;
    private int index;

    public CycleIterator(List<T> list) {
      this.list = list;
    }

    @Override
    public boolean hasNext() {
      return !list.isEmpty();
    }

    @Override
    public T next() {
      if (!hasNext()) {
        throw new NoSuchElementException();
      }
      T val = list.get(index);
      index++;
      if (index >= list.size()) {
        index -= list.size();
      }
      return val;
    }

    @Override
    public void remove() {
      throw new UnsupportedOperationException();
    }
  }
}
