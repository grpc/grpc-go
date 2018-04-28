/*
 * Copyright 2018, gRPC Authors All rights reserved.
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

package io.grpc.services;

import io.grpc.CallOptions;
import io.grpc.ClientInterceptor;
import io.grpc.ServerInterceptor;
import io.grpc.internal.BinaryLogProvider;
import java.util.concurrent.atomic.AtomicLong;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * The default implementation of a {@link BinaryLogProvider}.
 */
public class BinaryLogProviderImpl extends BinaryLogProvider {
  private static final Logger logger = Logger.getLogger(BinaryLogProviderImpl.class.getName());
  private final BinaryLog.Factory factory;
  private final AtomicLong counter = new AtomicLong();

  public BinaryLogProviderImpl() {
    this(BinaryLogSinkProvider.provider(), System.getenv("GRPC_BINARY_LOG_CONFIG"));
  }

  BinaryLogProviderImpl(BinaryLogSink sink, String configStr) {
    BinaryLog.Factory factory = null;
    try {
      factory = new BinaryLog.FactoryImpl(sink, configStr);
    } catch (RuntimeException e) {
      logger.log(Level.SEVERE, "Caught exception, binary log will be disabled", e);
    } catch (Error err) {
      logger.log(Level.SEVERE, "Caught exception, binary log will be disabled", err);
    }
    this.factory = factory;
  }

  @Nullable
  @Override
  public ServerInterceptor getServerInterceptor(String fullMethodName) {
    return factory.getLog(fullMethodName).getServerInterceptor(getServerCallId());
  }

  @Nullable
  @Override
  public ClientInterceptor getClientInterceptor(
      String fullMethodName, CallOptions callOptions) {
    return factory.getLog(fullMethodName).getClientInterceptor(getClientCallId(callOptions));
  }

  @Override
  protected int priority() {
    return 5;
  }

  @Override
  protected boolean isAvailable() {
    return factory != null;
  }

  protected CallId getServerCallId() {
    return new CallId(0, counter.getAndIncrement());
  }

  protected CallId getClientCallId(CallOptions options) {
    return new CallId(0, counter.getAndIncrement());
  }
}
