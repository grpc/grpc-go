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

package io.grpc;

import javax.annotation.concurrent.ThreadSafe;

/**
 * Listens to events on a stream to collect metrics.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2861")
@ThreadSafe
public abstract class ServerStreamTracer extends StreamTracer {
  /**
   * Called before the interceptors and the call handlers and make changes to the Context object
   * if needed.
   */
  public Context filterContext(Context context) {
    return context;
  }

  /**
   * Called when {@link ServerCall} is created.  This is for the tracer to access information about
   * the {@code ServerCall}.  Called after {@link #filterContext} and before the application call
   * handler.
   */
  public void serverCallStarted(ServerCall<?, ?> call) {
  }

  public abstract static class Factory {
    /**
     * Creates a {@link ServerStreamTracer} for a new server stream.
     *
     * <p>Called right before the stream is created
     *
     * @param fullMethodName the fully qualified method name
     * @param headers the received request headers.  It can be safely mutated within this method.
     *        It should not be saved because it is not safe for read or write after the method
     *        returns.
     */
    public abstract ServerStreamTracer newServerStreamTracer(
        String fullMethodName, Metadata headers);
  }
}
