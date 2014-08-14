package com.google.net.stubby;

import com.google.net.stubby.Server.MethodDefinition;
import com.google.net.stubby.Server.ServiceDefinition;

import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;

import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/** Mutable registry implementation of services and their methods for dispatching incoming calls. */
@ThreadSafe
public final class MutableHandlerRegistryImpl extends MutableHandlerRegistry {
  private final ConcurrentMap<String, ServiceDefinition> services
      = new ConcurrentHashMap<String, ServiceDefinition>();

  @Override
  @Nullable
  public ServiceDefinition addService(ServiceDefinition service) {
    return services.put(service.getName(), service);
  }

  @Override
  public boolean removeService(ServiceDefinition service) {
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
    ServiceDefinition service = services.get(methodName.substring(0, index));
    if (service == null) {
      return null;
    }
    MethodDefinition method = service.getMethod(methodName.substring(index + 1));
    if (method == null) {
      return null;
    }
    return new Method(service, method);
  }
}
