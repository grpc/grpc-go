package com.google.net.stubby;

import com.google.net.stubby.Server.MethodDefinition;
import com.google.net.stubby.Server.ServiceDefinition;

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
    private final ServiceDefinition serviceDef;
    private final MethodDefinition methodDef;

    public Method(ServiceDefinition serviceDef, MethodDefinition methodDef) {
      this.serviceDef = serviceDef;
      this.methodDef = methodDef;
    }

    public ServiceDefinition getServiceDefinition() {
      return serviceDef;
    }

    public MethodDefinition getMethodDefinition() {
      return methodDef;
    }
  }
}
