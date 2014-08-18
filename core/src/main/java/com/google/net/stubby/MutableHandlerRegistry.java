package com.google.net.stubby;

import com.google.net.stubby.ServerServiceDefinition;

import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/** Mutable registry of services and their methods for dispatching incoming calls. */
@ThreadSafe
public abstract class MutableHandlerRegistry extends HandlerRegistry {
  /**
   * Returns {@code null}, or previous service if {@code service} replaced an existing service.
   */
  @Nullable
  public abstract ServerServiceDefinition addService(ServerServiceDefinition service);

  /** Returns {@code false} if {@code service} was not registered. */
  @Nullable
  public abstract boolean removeService(ServerServiceDefinition service);
}
