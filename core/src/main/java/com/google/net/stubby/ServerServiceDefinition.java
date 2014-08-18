package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;
import com.google.common.collect.ImmutableMap;

import java.util.HashMap;
import java.util.Map;

/** Definition of a service to be exposed via a Server. */
public final class ServerServiceDefinition {
  public static Builder builder(String serviceName) {
    return new Builder(serviceName);
  }

  private final String name;
  private final ImmutableList<ServerMethodDefinition> methods;
  private final ImmutableMap<String, ServerMethodDefinition> methodLookup;

  private ServerServiceDefinition(String name, ImmutableList<ServerMethodDefinition> methods,
      Map<String, ServerMethodDefinition> methodLookup) {
    this.name = name;
    this.methods = methods;
    this.methodLookup = ImmutableMap.copyOf(methodLookup);
  }

  /** Simple name of the service. It is not an absolute path. */
  public String getName() {
    return name;
  }

  public ImmutableList<ServerMethodDefinition> getMethods() {
    return methods;
  }

  public ServerMethodDefinition getMethod(String name) {
    return methodLookup.get(name);
  }

  /** Builder for constructing Service instances. */
  public static final class Builder {
    private final String serviceName;
    private final ImmutableList.Builder<ServerMethodDefinition> methods = ImmutableList.builder();
    private final Map<String, ServerMethodDefinition> methodLookup
        = new HashMap<String, ServerMethodDefinition>();

    private Builder(String serviceName) {
      this.serviceName = serviceName;
    }

    /**
     * Add a method to be supported by the service.
     *
     * @param name simple name of the method, without the service prefix
     * @param requestMarshaller marshaller for deserializing incoming requests
     * @param responseMarshaller marshaller for serializing outgoing responses
     * @param handler handler for incoming calls
     */
    public <ReqT, RespT> Builder addMethod(String name, Marshaller<ReqT> requestMarshaller,
        Marshaller<RespT> responseMarshaller, ServerCallHandler<ReqT, RespT> handler) {
      Preconditions.checkNotNull(name, "name must not be null");
      if (methodLookup.containsKey(name)) {
        throw new IllegalStateException("Method by same name already registered");
      }
      ServerMethodDefinition def = new ServerMethodDefinition<ReqT, RespT>(name,
          Preconditions.checkNotNull(requestMarshaller, "requestMarshaller must not be null"),
          Preconditions.checkNotNull(responseMarshaller, "responseMarshaller must not be null"),
          Preconditions.checkNotNull(handler, "handler must not be null"));
      methodLookup.put(name, def);
      methods.add(def);
      return this;
    }

    /** Construct new ServerServiceDefinition. */
    public ServerServiceDefinition build() {
      return new ServerServiceDefinition(serviceName, methods.build(), methodLookup);
    }
  }
}
