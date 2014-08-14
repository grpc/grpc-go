package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;
import com.google.common.collect.ImmutableMap;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.Service;

import java.io.InputStream;
import java.util.HashMap;
import java.util.Map;

import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Server for listening for and dispatching incoming calls. Although Server is an interface, it is
 * not expected to be implemented by application code or interceptors.
 */
@ThreadSafe
public interface Server extends Service {
  /** Definition of a service to be exposed via a Server. */
  final class ServiceDefinition {
    public static ServiceDefinition.Builder builder(String serviceName) {
      return new ServiceDefinition.Builder(serviceName);
    }

    private final String name;
    private final ImmutableList<MethodDefinition> methods;
    private final ImmutableMap<String, MethodDefinition> methodLookup;

    private ServiceDefinition(String name, ImmutableList<MethodDefinition> methods,
        Map<String, MethodDefinition> methodLookup) {
      this.name = name;
      this.methods = methods;
      this.methodLookup = ImmutableMap.copyOf(methodLookup);
    }

    /** Simple name of the service. It is not an absolute path. */
    public String getName() {
      return name;
    }

    public ImmutableList<MethodDefinition> getMethods() {
      return methods;
    }

    public MethodDefinition getMethod(String name) {
      return methodLookup.get(name);
    }

    /** Builder for constructing Service instances. */
    public static final class Builder {
      private final String serviceName;
      private final ImmutableList.Builder<MethodDefinition> methods = ImmutableList.builder();
      private final Map<String, MethodDefinition> methodLookup
          = new HashMap<String, MethodDefinition>();

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
          Marshaller<RespT> responseMarshaller, CallHandler<ReqT, RespT> handler) {
        Preconditions.checkNotNull(name, "name must not be null");
        if (methodLookup.containsKey(name)) {
          throw new IllegalStateException("Method by same name already registered");
        }
        MethodDefinition def = new MethodDefinition<ReqT, RespT>(name,
            Preconditions.checkNotNull(requestMarshaller, "requestMarshaller must not be null"),
            Preconditions.checkNotNull(responseMarshaller, "responseMarshaller must not be null"),
            Preconditions.checkNotNull(handler, "handler must not be null"));
        methodLookup.put(name, def);
        methods.add(def);
        return this;
      }

      /** Construct new ServiceDefinition. */
      public ServiceDefinition build() {
        return new ServiceDefinition(serviceName, methods.build(), methodLookup);
      }
    }
  }

  /** Definition of a method supported by a service. */
  final class MethodDefinition<RequestT, ResponseT> {
    private final String name;
    private final Marshaller<RequestT> requestMarshaller;
    private final Marshaller<ResponseT> responseMarshaller;
    private final CallHandler<RequestT, ResponseT> handler;

    // MethodDefinition has no form of public construction. It is only created within the context of
    // a ServiceDefinition.Builder.
    private MethodDefinition(String name, Marshaller<RequestT> requestMarshaller,
        Marshaller<ResponseT> responseMarshaller, CallHandler<RequestT, ResponseT> handler) {
      this.name = name;
      this.requestMarshaller = requestMarshaller;
      this.responseMarshaller = responseMarshaller;
      this.handler = handler;
    }

    /** The simple name of the method. It is not an absolute path. */
    public String getName() {
      return name;
    }

    /** Marshaller for deserializing incoming requests. */
    public Marshaller<RequestT> getRequestMarshaller() {
      return requestMarshaller;
    }

    /** Marshaller for serializing outgoing responses. */
    public Marshaller<ResponseT> getResponseMarshaller() {
      return responseMarshaller;
    }

    /** Handler for incoming calls. */
    public CallHandler<RequestT, ResponseT> getCallHandler() {
      return handler;
    }
  }

  /**
   * Interface for intercepting incoming RPCs before the handler receives them.
   */
  @ThreadSafe
  interface Interceptor {
    /**
     * Intercept a new call. General semantics of {@link Server.CallHandler#startCall} apply. {@code
     * next} may only be called once. Returned listener must not be {@code null}.
     *
     * <p>If the implementation throws an exception, {@code call} will be closed with an error.
     * Implementations must not throw an exception if they started processing that may use {@code
     * call} on another thread.
     *
     * @param method metadata concerning the call
     * @param call object for responding
     * @param next next processor in the interceptor chain
     * @return listener for processing incoming messages for {@code call}
     */
    <ReqT, RespT> Server.Call.Listener<ReqT> interceptCall(MethodDescriptor<ReqT, RespT> method,
        Server.Call<ReqT, RespT> call, CallHandler<ReqT, RespT> next);
  }

  /**
   * Interface to begin processing incoming RPCs. Advanced applications and generated code implement
   * this interface to implement service methods.
   */
  @ThreadSafe
  interface CallHandler<ReqT, RespT> {
    /**
     * Produce a non-{@code null} listener for the incoming call. Implementations are free to call
     * methods on {@code call} before this method has returned.
     *
     * <p>If the implementation throws an exception, {@code call} will be closed with an error.
     * Implementations must not throw an exception if they started processing that may use {@code
     * call} on another thread.
     *
     * @param method metadata concerning the call
     * @param call object for responding
     * @return listener for processing incoming messages for {@code call}
     */
    Server.Call.Listener<ReqT> startCall(MethodDescriptor<ReqT, RespT> method,
        Server.Call<ReqT, RespT> call);
  }

  /**
   * Low-level method for communicating with a remote client during a single RPC. Unlike normal
   * RPCs, calls may stream any number of requests and responses, although a single request and
   * single response is most common. This API is generally intended for use generated handlers, but
   * advanced applications may have need for it.
   *
   * <p>Any contexts must be sent before any payloads, which must be sent before closing.
   *
   * <p>No generic method for determining message receipt or providing acknowlegement is provided.
   * Applications are expected to utilize normal payload messages for such signals, as a response
   * natually acknowledges its request.
   *
   * <p>Methods are guaranteed to be non-blocking. Implementations are not required to be
   * thread-safe.
   */
  abstract class Call<RequestT, ResponseT> {
    /**
     * Callbacks for consuming incoming RPC messages.
     *
     * <p>Any contexts are guaranteed to arrive before any payloads, which are guaranteed before
     * half close, which is guaranteed before completion.
     *
     * <p>Implementations are free to block for extended periods of time. Implementations are not
     * required to be thread-safe.
     */
    // TODO(user): We need to decide what to do in the case of server closing with non-cancellation
    // before client half closes. It may be that we treat such a case as an error. If we permit such
    // a case then we either get to generate a half close or purposefully omit it.
    public abstract static class Listener<T> {
      /**
       * A request context has been received. Any context messages will precede payload messages.
       *
       * <p>The {@code value} {@link InputStream} will be closed when the returned future completes.
       * If no future is returned, the value will be closed immediately after returning from this
       * method.
       */
      @Nullable
      public abstract ListenableFuture<Void> onContext(String name, InputStream value);

      /**
       * A request payload has been receiveed. For streaming calls, there may be zero payload
       * messages.
       */
      @Nullable
      public abstract ListenableFuture<Void> onPayload(T payload);

      /**
       * The client completed all message sending. However, the call may still be cancelled.
       */
      public abstract void onHalfClose();

      /**
       * The call was cancelled and the server is encouraged to abort processing to save resources,
       * since the client will not process any further messages. Cancellations can be caused by
       * timeouts, explicit cancel by client, network errors, and similar.
       *
       * <p>There will be no further callbacks for the call.
       */
      public abstract void onCancel();

      /**
       * The call is considered complete and {@link #onCancel} is guaranteed not to be called.
       * However, the client is not guaranteed to have received all messages.
       *
       * <p>There will be no further callbacks for the call.
       */
      public abstract void onCompleted();
    }

    /**
     * Close the call with the provided status. No further sending or receiving will occur. If
     * {@code status} is not equal to {@link Status#OK}, then the call is said to have failed.
     *
     * <p>If {@code status} is not {@link Status#CANCELLED} and no errors or cancellations are known
     * to have occured, then a {@link Listener#onCompleted} notification should be expected.
     * Otherwise {@link Listener#onCancel} has been or will be called.
     *
     * @throws IllegalStateException if call is already {@code close}d
     */
    public abstract void close(Status status);

    /**
     * Send a context message. Context messages are intended for side-channel information like
     * statistics and authentication.
     *
     * @param name key identifier of context
     * @param value context value bytes
     * @throws IllegalStateException if call is {@link #close}d, or after {@link #sendPayload}
     */
    public abstract void sendContext(String name, InputStream value);

    /**
     * Send a payload message. Payload messages are the primary form of communication associated
     * with RPCs. Multiple payload messages may exist for streaming calls.
     *
     * @param payload message
     * @throws IllegalStateException if call is {@link #close}d
     */
    public abstract void sendPayload(ResponseT payload);

    /**
     * Returns {@code true} when the call is cancelled and the server is encouraged to abort
     * processing to save resources, since the client will not be processing any further methods.
     * Cancellations can be caused by timeouts, explicit cancel by client, network errors, and
     * similar.
     *
     * <p>This method may safely be called concurrently from multiple threads.
     */
    public abstract boolean isCancelled();
  }
}
