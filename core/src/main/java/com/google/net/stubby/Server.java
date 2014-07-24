package com.google.net.stubby;

import com.google.common.collect.ImmutableList;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.Service;

import java.io.InputStream;

import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Server for listening for and dispatching incoming calls. Although Server is an interface, it is
 * not expected to be implemented by application code or interceptors.
 */
@ThreadSafe
public interface Server extends Service {
  /** Builder that Servers are expected to provide for constructing new instances. */
  abstract class Builder {
    public abstract Builder addService(ServiceDef service);
    public abstract Server build();
  }

  /** Definition of a service to be exposed via a Server. */
  final class ServiceDef {
    public static ServiceDef.Builder builder(String serviceName) {
      return new ServiceDef.Builder(serviceName);
    }

    private final String name;
    private final ImmutableList<MethodDef> methods;

    private ServiceDef(String name, ImmutableList<MethodDef> methods) {
      this.name = name;
      this.methods = methods;
    }

    /** Simple name of the service. It is not an absolute path. */
    public String getName() {
      return name;
    }

    public ImmutableList<MethodDef> getMethods() {
      return methods;
    }

    /** Builder for constructing Service instances. */
    public static final class Builder {
      private final String serviceName;
      private final ImmutableList.Builder<MethodDef> methods = ImmutableList.builder();

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
        methods.add(
            new MethodDef<ReqT, RespT>(name, requestMarshaller, responseMarshaller, handler));
        return this;
      }

      /** Construct new ServiceDef. */
      public ServiceDef build() {
        return new ServiceDef(serviceName, methods.build());
      }
    }
  }

  /** Definition of a method supported by a service. */
  final class MethodDef<RequestT, ResponseT> {
    private final String name;
    private final Marshaller<RequestT> requestMarshaller;
    private final Marshaller<ResponseT> responseMarshaller;
    private final CallHandler<RequestT, ResponseT> handler;

    // MethodDef has no way of public creation, because all parameters are required. A builder
    // wouldn't have any methods other than build(). addMethod() can be overriden if we ever need to
    // extend what MethodDef contains or if we add a Builder or similar.
    private MethodDef(String name, Marshaller<RequestT> requestMarshaller,
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
   * Class to begin processing incoming RPCs. Advanced applications and generated code implement
   * this interface to implement service methods.
   */
  @ThreadSafe
  interface CallHandler<ReqT, RespT> {
    /**
     * Produce a listener for the incoming call. Implementations are free to call methods on {@code
     * call} before this method has returned.
     *
     * <p>If the implementation throws an exception or returns {@code null}, {@code call} will be
     * closed with an error.
     *
     * @param call object for responding
     * @param method metadata concerning the call
     * @return listener for processing incoming messages for {@code call}
     */
    Server.Call.Listener<ReqT> startCall(Server.Call<ReqT, RespT> call,
        MethodDescriptor<ReqT, RespT> method);
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
