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
import io.grpc.InternalClientInterceptors;
import io.grpc.InternalServerInterceptors;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerMethodDefinition;
import java.io.Closeable;
import java.io.IOException;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.Iterator;
import java.util.List;
import java.util.ServiceConfigurationError;
import java.util.ServiceLoader;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

public abstract class BinaryLogProvider implements Closeable {
  private static final Logger logger = Logger.getLogger(BinaryLogProvider.class.getName());
  private static final BinaryLogProvider PROVIDER = load(BinaryLogProvider.class.getClassLoader());
  @VisibleForTesting
  static final Marshaller<InputStream> IDENTITY_MARSHALLER = new IdentityMarshaller();

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
  Channel wrapChannel(Channel channel) {
    return ClientInterceptors.intercept(channel, binaryLogShim);
  }

  private static MethodDescriptor<InputStream, InputStream> toInputStreamMethod(
      MethodDescriptor<?, ?> method) {
    return method.toBuilder(IDENTITY_MARSHALLER, IDENTITY_MARSHALLER).build();
  }

  /**
   * Wraps a {@link ServerMethodDefinition} such that it performs binary logging if needed.
   */
  final <ReqT, RespT> ServerMethodDefinition<?, ?> wrapMethodDefinition(
      ServerMethodDefinition<ReqT, RespT> oMethodDef) {
    ServerInterceptor binlogInterceptor =
        getServerInterceptor(oMethodDef.getMethodDescriptor().getFullMethodName());
    if (binlogInterceptor == null) {
      return oMethodDef;
    }
    MethodDescriptor<InputStream, InputStream> binMethod =
        BinaryLogProvider.toInputStreamMethod(oMethodDef.getMethodDescriptor());
    ServerMethodDefinition<InputStream, InputStream> binDef = InternalServerInterceptors
        .wrapMethod(oMethodDef, binMethod);
    ServerCallHandler<InputStream, InputStream> binlogHandler = InternalServerInterceptors
        .interceptCallHandler(binlogInterceptor, binDef.getServerCallHandler());
    return ServerMethodDefinition.create(binMethod, binlogHandler);
  }


  @VisibleForTesting
  static BinaryLogProvider load(ClassLoader classLoader) {
    try {
      return loadHelper(classLoader);
    } catch (Throwable t) {
      logger.log(
          Level.SEVERE, "caught exception loading BinaryLogProvider, will disable binary log", t);
      return null;
    }
  }

  private static BinaryLogProvider loadHelper(ClassLoader classLoader) {
    if (isAndroid()) {
      return null;
    }

    Iterator<BinaryLogProvider> iter = getCandidatesViaServiceLoader(classLoader).iterator();
    List<BinaryLogProvider> list = new ArrayList<BinaryLogProvider>();
    while (iter.hasNext()) {
      // The iterator comes from ServiceLoader and may throw when next() is called
      try {
        list.add(iter.next());
      } catch (ServiceConfigurationError e) {
        logger.log(Level.SEVERE, "caught exception creating an instance of BinaryLogProvider", e);
      }
    }
    if (list.isEmpty()) {
      return null;
    } else {
      return Collections.max(list, new Comparator<BinaryLogProvider>() {
        @Override
        public int compare(BinaryLogProvider f1, BinaryLogProvider f2) {
          return f1.priority() - f2.priority();
        }
      });
    }
  }

  private static ServiceLoader<BinaryLogProvider> getCandidatesViaServiceLoader(
      ClassLoader classLoader) {
    ServiceLoader<BinaryLogProvider> i = ServiceLoader.load(BinaryLogProvider.class, classLoader);
    // Attempt to load using the context class loader and ServiceLoader.
    // This allows frameworks like http://aries.apache.org/modules/spi-fly.html to plug in.
    if (!i.iterator().hasNext()) {
      i = ServiceLoader.load(BinaryLogProvider.class);
    }
    return i;
  }

  /**
   * Returns a {@link ServerInterceptor} for binary logging. gRPC is free to cache the interceptor,
   * so the interceptor must be reusable across calls. At runtime, the request and response
   * marshallers are always {@code Marshaller<InputStream>}.
   * Returns {@code null} if this method is not binary logged.
   */
  // TODO(zpencer): ensure the interceptor properly handles retries and hedging
  @Nullable
  public abstract ServerInterceptor getServerInterceptor(String fullMethodName);

  /**
   * Returns a {@link ClientInterceptor} for binary logging. gRPC is free to cache the interceptor,
   * so the interceptor must be reusable across calls. At runtime, the request and response
   * marshallers are always {@code Marshaller<InputStream>}.
   * Returns {@code null} if this method is not binary logged.
   */
  // TODO(zpencer): ensure the interceptor properly handles retries and hedging
  @Nullable
  public abstract ClientInterceptor getClientInterceptor(String fullMethodName);

  @Override
  public void close() throws IOException {
    // default impl: noop
    // TODO(zpencer): make BinaryLogProvider provide a BinaryLog, and this method belongs there
  }

  /**
   * A priority, from 0 to 10 that this provider should be used, taking the current environment into
   * consideration. 5 should be considered the default, and then tweaked based on environment
   * detection. A priority of 0 does not imply that the provider wouldn't work; just that it should
   * be last in line.
   */
  protected abstract int priority();

  /**
   * A provider that always returns null interceptors.
   */
  @VisibleForTesting
  static final class NullProvider extends BinaryLogProvider {
    @Nullable
    @Override
    public ServerInterceptor getServerInterceptor(String fullMethodName) {
      return null;
    }

    @Override
    public ClientInterceptor getClientInterceptor(String fullMethodName) {
      return null;
    }

    @Override
    protected int priority() {
      return 0;
    }
  }

  /**
   * Returns whether current platform is Android.
   */
  protected static boolean isAndroid() {
    try {
      // Specify a class loader instead of null because we may be running under Robolectric
      Class.forName("android.app.Application", /*initialize=*/ false,
          BinaryLogProvider.class.getClassLoader());
      return true;
    } catch (Exception e) {
      // If Application isn't loaded, it might as well not be Android.
      return false;
    }
  }

  // Creating a named class makes debugging easier
  private static final class IdentityMarshaller implements Marshaller<InputStream> {
    @Override
    public InputStream stream(InputStream value) {
      return value;
    }

    @Override
    public InputStream parse(InputStream stream) {
      return stream;
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
                IDENTITY_MARSHALLER,
                IDENTITY_MARSHALLER)
            .interceptCall(method, callOptions, next);
      }
    }
  }
}
