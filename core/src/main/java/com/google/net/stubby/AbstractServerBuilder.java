package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.Service;
import com.google.net.stubby.transport.ServerListener;

import java.util.concurrent.ExecutorService;

/**
 * The base class for server builders.
 *
 * @param <BuilderT> The concrete type for this builder.
 */
public abstract class AbstractServerBuilder<BuilderT extends AbstractServerBuilder<?>>
    extends AbstractServiceBuilder<ServerImpl, BuilderT> {

  private final HandlerRegistry registry;

  /**
   * Constructs using a given handler registry.
   */
  protected AbstractServerBuilder(HandlerRegistry registry) {
    this.registry = Preconditions.checkNotNull(registry);
  }

  /**
   * Constructs with a MutableHandlerRegistry created internally.
   */
  protected AbstractServerBuilder() {
    this.registry = new MutableHandlerRegistryImpl();
  }

  /**
   * Adds a service implementation to the handler registry.
   *
   * <p>This is supported only if the user didn't provide a handler registry, or the provided one is
   * a {@link MutableHandlerRegistry}. Otherwise it throws an UnsupportedOperationException.
   */
  @SuppressWarnings("unchecked")
  public final BuilderT addService(ServerServiceDefinition service) {
    if (registry instanceof MutableHandlerRegistry) {
      ((MutableHandlerRegistry) registry).addService(service);
      return (BuilderT) this;
    }
    throw new UnsupportedOperationException("Underlying HandlerRegistry is not mutable");
  }

  @Override
  protected final ServerImpl buildImpl(ExecutorService executor) {
    ServerImpl server = new ServerImpl(executor, registry);
    server.setTransportServer(buildTransportServer(server.serverListener()));
    return server;
  }

  protected abstract Service buildTransportServer(ServerListener serverListener);
}
