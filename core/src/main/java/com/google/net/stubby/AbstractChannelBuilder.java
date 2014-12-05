/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.Service;
import com.google.net.stubby.transport.ClientTransportFactory;

import java.util.concurrent.ExecutorService;

import javax.annotation.Nullable;

/**
 * The base class for channel builders.
 *
 * @param <BuilderT> The concrete type of this builder.
 */
public abstract class AbstractChannelBuilder<BuilderT extends AbstractChannelBuilder<BuilderT>>
    extends AbstractServiceBuilder<ChannelImpl, BuilderT> {

  @Override
  protected final ChannelImpl buildImpl(ExecutorService executor) {
    ChannelEssentials essentials = buildEssentials();
    ChannelImpl channel = new ChannelImpl(essentials.transportFactory, executor);
    if (essentials.listener != null) {
      channel.addListener(essentials.listener, MoreExecutors.directExecutor());
    }
    return channel;
  }

  /**
   * The essentials required for creating a channel.
   */
  protected static class ChannelEssentials {
    final ClientTransportFactory transportFactory;
    @Nullable final Service.Listener listener;

    /**
     * @param transportFactory the created channel uses this factory to create transports
     * @param listener will be called at the channel's life-cycle events
     */
    public ChannelEssentials(ClientTransportFactory transportFactory,
        @Nullable Service.Listener listener) {
      this.transportFactory = Preconditions.checkNotNull(transportFactory);
      this.listener = listener;
    }
  }

  protected abstract ChannelEssentials buildEssentials();
}
