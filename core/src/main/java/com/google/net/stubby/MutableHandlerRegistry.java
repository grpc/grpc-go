package com.google.net.stubby;

import com.google.net.stubby.Server.ServiceDefinition;

import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/** Mutable registry of services and their methods for dispatching incoming calls. */
@ThreadSafe
public abstract class MutableHandlerRegistry extends HandlerRegistry {
  /**
   * Returns {@code null}, or previous service if {@code service} replaced an existing service.
   */
  @Nullable
  public abstract ServiceDefinition addService(ServiceDefinition service);

  /** Returns {@code false} if {@code service} was not registered. */
  @Nullable
  public abstract boolean removeService(ServiceDefinition service);
}
