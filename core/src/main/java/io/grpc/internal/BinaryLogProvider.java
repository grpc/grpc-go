/*
 * Copyright 2017, gRPC Authors All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.internal;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors;
import io.grpc.Context;
import io.grpc.Internal;
import io.grpc.InternalClientInterceptors;
import io.grpc.InternalServerInterceptors;
import io.grpc.InternalServiceProviders;
import io.grpc.InternalServiceProviders.PriorityAccessor;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerMethodDefinition;
import io.grpc.ServerStreamTracer;
import io.opencensus.trace.Span;
import io.opencensus.trace.Tracing;
import java.io.ByteArrayInputStream;
import java.io.Closeable;
import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;
import java.util.Collections;
import java.util.logging.Logger;
import javax.annotation.Nullable;

public abstract class BinaryLogProvider implements Closeable {
  // TODO(zpencer): move to services and make package private
  @Internal
  public static final Context.Key<CallId> SERVER_CALL_ID_CONTEXT_KEY
      = Context.key("binarylog-context-key");
  // TODO(zpencer): move to services and make package private when this class is moved
  @Internal
  public static final CallOptions.Key<CallId> CLIENT_CALL_ID_CALLOPTION_KEY
      = CallOptions.Key.of("binarylog-calloptions-key", null);
  @VisibleForTesting
  public static final Marshaller<byte[]> BYTEARRAY_MARSHALLER = new ByteArrayMarshaller();

  private static final Logger logger = Logger.getLogger(BinaryLogProvider.class.getName());
  private static final BinaryLogProvider PROVIDER = InternalServiceProviders.load(
      BinaryLogProvider.class,
      Collections.<Class<?>>emptyList(),
      BinaryLogProvider.class.getClassLoader(),
      new PriorityAccessor<BinaryLogProvider>() {
        @Override
        public boolean isAvailable(BinaryLogProvider provider) {
          return provider.isAvailable();
        }

        @Override
        public int getPriority(BinaryLogProvider provider) {
          return provider.priority();
        }
      });

  private final ClientInterceptor binaryLogShim = new BinaryLogShim();

  /**
   * Returns a {@code BinaryLogProvider}, or {@code null} if there is no provider.
   */
  @Nullable
  public static BinaryLogProvider provider() {
    return PROVIDER;
  }

  /**
   * Wraps a channel to provide binary logging on {@link ClientCall}s as needed.
   */
  public final Channel wrapChannel(Channel channel) {
    return ClientInterceptors.intercept(channel, binaryLogShim);
  }

  private static MethodDescriptor<byte[], byte[]> toByteBufferMethod(
      MethodDescriptor<?, ?> method) {
    return method.toBuilder(BYTEARRAY_MARSHALLER, BYTEARRAY_MARSHALLER).build();
  }

  /**
   * Wraps a {@link ServerMethodDefinition} such that it performs binary logging if needed.
   */
  public final <ReqT, RespT> ServerMethodDefinition<?, ?> wrapMethodDefinition(
      ServerMethodDefinition<ReqT, RespT> oMethodDef) {
    ServerInterceptor binlogInterceptor =
        getServerInterceptor(oMethodDef.getMethodDescriptor().getFullMethodName());
    if (binlogInterceptor == null) {
      return oMethodDef;
    }
    MethodDescriptor<byte[], byte[]> binMethod =
        BinaryLogProvider.toByteBufferMethod(oMethodDef.getMethodDescriptor());
    ServerMethodDefinition<byte[], byte[]> binDef = InternalServerInterceptors
        .wrapMethod(oMethodDef, binMethod);
    ServerCallHandler<byte[], byte[]> binlogHandler = InternalServerInterceptors
        .interceptCallHandler(binlogInterceptor, binDef.getServerCallHandler());
    return ServerMethodDefinition.create(binMethod, binlogHandler);
  }


  /**
   * Returns a {@link ServerInterceptor} for binary logging. gRPC is free to cache the interceptor,
   * so the interceptor must be reusable across calls. At runtime, the request and response
   * marshallers are always {@code Marshaller<InputStream>}.
   * Returns {@code null} if this method is not binary logged.
   */
  // TODO(zpencer): ensure the interceptor properly handles retries and hedging
  @Nullable
  protected abstract ServerInterceptor getServerInterceptor(String fullMethodName);

  /**
   * Returns a {@link ClientInterceptor} for binary logging. gRPC is free to cache the interceptor,
   * so the interceptor must be reusable across calls. At runtime, the request and response
   * marshallers are always {@code Marshaller<InputStream>}.
   * Returns {@code null} if this method is not binary logged.
   */
  // TODO(zpencer): ensure the interceptor properly handles retries and hedging
  @Nullable
  protected abstract ClientInterceptor getClientInterceptor(String fullMethodName);

  @Override
  public void close() throws IOException {
    // default impl: noop
    // TODO(zpencer): make BinaryLogProvider provide a BinaryLog, and this method belongs there
  }

  private static final ServerStreamTracer SERVER_CALLID_SETTER = new ServerStreamTracer() {
    @Override
    public Context filterContext(Context context) {
      Context toRestore = context.attach();
      try {
        Span span = Tracing.getTracer().getCurrentSpan();
        if (span == null) {
          return context;
        }

        return context.withValue(SERVER_CALL_ID_CONTEXT_KEY, CallId.fromCensusSpan(span));
      } finally {
        context.detach(toRestore);
      }
    }
  };

  private static final ServerStreamTracer.Factory SERVER_CALLID_SETTER_FACTORY
      = new ServerStreamTracer.Factory() {
          @Override
          public ServerStreamTracer newServerStreamTracer(String fullMethodName, Metadata headers) {
            return SERVER_CALLID_SETTER;
          }
      };

  /**
   * Returns a {@link ServerStreamTracer.Factory} that copies the call ID to the {@link Context}
   * as {@code SERVER_CALL_ID_CONTEXT_KEY}.
   */
  public ServerStreamTracer.Factory getServerCallIdSetter() {
    return SERVER_CALLID_SETTER_FACTORY;
  }

  private static final ClientInterceptor CLIENT_CALLID_SETTER = new ClientInterceptor() {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      Span span = Tracing.getTracer().getCurrentSpan();
      if (span == null) {
        return next.newCall(method, callOptions);
      }

      return next.newCall(
          method,
          callOptions.withOption(CLIENT_CALL_ID_CALLOPTION_KEY, CallId.fromCensusSpan(span)));
    }
  };

  /**
   * Returns a {@link ClientInterceptor} that copies the call ID to the {@link CallOptions}
   * as {@code CALL_CLIENT_CALL_ID_CALLOPTION_KEY}.
   */
  public ClientInterceptor getClientCallIdSetter() {
    return CLIENT_CALLID_SETTER;
  }

  /**
   * A priority, from 0 to 10 that this provider should be used, taking the current environment into
   * consideration. 5 should be considered the default, and then tweaked based on environment
   * detection. A priority of 0 does not imply that the provider wouldn't work; just that it should
   * be last in line.
   */
  protected abstract int priority();

  /**
   * Whether this provider is available for use, taking the current environment into consideration.
   * If {@code false}, no other methods are safe to be called.
   */
  protected abstract boolean isAvailable();

  // Creating a named class makes debugging easier
  private static final class ByteArrayMarshaller implements Marshaller<byte[]> {
    @Override
    public InputStream stream(byte[] value) {
      return new ByteArrayInputStream(value);
    }

    @Override
    public byte[] parse(InputStream stream) {
      try {
        return parseHelper(stream);
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
    }

    private byte[] parseHelper(InputStream stream) throws IOException {
      try {
        return IoUtils.toByteArray(stream);
      } finally {
        stream.close();
      }
    }
  }

  /**
   * The pipeline of interceptors is hard coded when the {@link ManagedChannelImpl} is created.
   * This shim interceptor should always be installed as a placeholder. When a call starts,
   * this interceptor checks with the {@link BinaryLogProvider} to see if logging should happen
   * for this particular {@link ClientCall}'s method.
   */
  private final class BinaryLogShim implements ClientInterceptor {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method,
        CallOptions callOptions,
        Channel next) {
      ClientInterceptor binlogInterceptor = getClientInterceptor(method.getFullMethodName());
      if (binlogInterceptor == null) {
        return next.newCall(method, callOptions);
      } else {
        return InternalClientInterceptors
            .wrapClientInterceptor(
                binlogInterceptor,
                BYTEARRAY_MARSHALLER,
                BYTEARRAY_MARSHALLER)
            .interceptCall(method, callOptions, next);
      }
    }
  }

  /**
   * A CallId is two byte[] arrays both of size 8 that uniquely identifies the RPC. Users are
   * free to use the byte arrays however they see fit.
   */
  public static final class CallId {
    public final long hi;
    public final long lo;

    /**
     * Creates an instance.
     */
    public CallId(long hi, long lo) {
      this.hi = hi;
      this.lo = lo;
    }

    static CallId fromCensusSpan(Span span) {
      return new CallId(0, ByteBuffer.wrap(span.getContext().getSpanId().getBytes()).getLong());
    }
  }
}
