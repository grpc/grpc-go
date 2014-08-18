package com.google.net.stubby;

import com.google.net.stubby.ServerMethodDefinition;
import com.google.net.stubby.ServerServiceDefinition;

import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;

import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/** Mutable registry implementation of services and their methods for dispatching incoming calls. */
@ThreadSafe
public final class MutableHandlerRegistryImpl extends MutableHandlerRegistry {
  private final ConcurrentMap<String, ServerServiceDefinition> services
      = new ConcurrentHashMap<String, ServerServiceDefinition>();

  @Override
  @Nullable
  public ServerServiceDefinition addService(ServerServiceDefinition service) {
    return services.put(service.getName(), service);
  }

  @Override
  public boolean removeService(ServerServiceDefinition service) {
    return services.remove(service.getName(), service);
  }

  @Override
  @Nullable
  public Method lookupMethod(String methodName) {
    methodName = methodName.replace('.', '/');
    if (!methodName.startsWith("/")) {
      return null;
    }
    methodName = methodName.substring(1);
    int index = methodName.lastIndexOf("/");
    if (index == -1) {
      return null;
    }
    ServerServiceDefinition service = services.get(methodName.substring(0, index));
    if (service == null) {
      return null;
    }
    ServerMethodDefinition method = service.getMethod(methodName.substring(index + 1));
    if (method == null) {
      return null;
    }
    return new Method(service, method);
  }
}
