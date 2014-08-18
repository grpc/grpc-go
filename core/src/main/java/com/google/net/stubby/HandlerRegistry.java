package com.google.net.stubby;

import com.google.net.stubby.ServerMethodDefinition;
import com.google.net.stubby.ServerServiceDefinition;

import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/** Registry of services and their methods for dispatching incoming calls. */
@ThreadSafe
public abstract class HandlerRegistry {
  /** Lookup full method name, starting with '/'. Returns {@code null} if method not found. */
  @Nullable
  public abstract Method lookupMethod(String methodName);

  /** A method definition and its parent's service definition. */
  public static final class Method {
    private final ServerServiceDefinition serviceDef;
    private final ServerMethodDefinition methodDef;

    public Method(ServerServiceDefinition serviceDef, ServerMethodDefinition methodDef) {
      this.serviceDef = serviceDef;
      this.methodDef = methodDef;
    }

    public ServerServiceDefinition getServiceDefinition() {
      return serviceDef;
    }

    public ServerMethodDefinition getMethodDefinition() {
      return methodDef;
    }
  }
}
