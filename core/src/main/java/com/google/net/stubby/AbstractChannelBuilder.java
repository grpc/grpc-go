package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.Service;
import com.google.net.stubby.newtransport.ClientTransportFactory;

import java.util.concurrent.ExecutorService;

import javax.annotation.Nullable;

/**
 * The base class for channel builders.
 *
 * @param <BuilderT> The concrete type of this builder.
 */
public abstract class AbstractChannelBuilder<BuilderT extends AbstractChannelBuilder<?>>
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
