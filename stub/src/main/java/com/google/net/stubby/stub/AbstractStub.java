/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package com.google.net.stubby.stub;

import com.google.common.collect.Maps;
import com.google.net.stubby.Channel;
import com.google.net.stubby.ClientInterceptor;
import com.google.net.stubby.ClientInterceptors;
import com.google.net.stubby.MethodDescriptor;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;

/**
 * Common base type for stub implementations. Allows for reconfiguration.
 *
 * @param <S> the concrete type of this stub.
 * @param <C> the service descriptor type
 */
// TODO(user): Move into 3rd party when tidy
// TODO(lryan/kevinb): Excessive parameterization can be a pain, try to eliminate once the generated
// code is more tangible.
public abstract class AbstractStub<S extends AbstractStub<?, ?>,
    C extends AbstractServiceDescriptor<C>> {
  protected final Channel channel;
  protected final C config;

  /**
   * Constructor for use by subclasses.
   */
  protected AbstractStub(Channel channel, C config) {
    this.channel = channel;
    this.config = config;
  }

  protected C getServiceDescriptor() {
    return config;
  }

  public StubConfigBuilder configureNewStub() {
    return new StubConfigBuilder();
  }

  public Channel getChannel() {
    return channel;
  }

  /**
   * Returns a new stub configuration for the provided method configurations.
   */
  protected abstract S build(Channel channel, C config);

  /**
   * Utility class for (re) configuring the operations in a stub.
   */
  public class StubConfigBuilder {

    private final Map<String, MethodDescriptor<?, ?>> methodMap;
    private final List<ClientInterceptor> interceptors = new ArrayList<ClientInterceptor>();
    private Channel stubChannel;

    private StubConfigBuilder() {
      this.stubChannel = AbstractStub.this.channel;
      methodMap = Maps.newHashMapWithExpectedSize(config.methods().size());
      for (MethodDescriptor<?, ?> method : AbstractStub.this.config.methods()) {
        methodMap.put(method.getName(), method);
      }
    }

    /**
     * Set a timeout for all methods in the stub.
     */
    public StubConfigBuilder setTimeout(long timeout, TimeUnit unit) {
      for (Map.Entry<String, MethodDescriptor<?, ?>> entry : methodMap.entrySet()) {
        entry.setValue(entry.getValue().withTimeout(timeout, unit));
      }
      return this;
    }

    /**
     * Set the channel to be used by the stub.
     */
    public StubConfigBuilder setChannel(Channel channel) {
      this.stubChannel = channel;
      return this;
    }

    /**
     * Adds a client interceptor to be attached to the channel.
     */
    public StubConfigBuilder addInterceptor(ClientInterceptor interceptor) {
      interceptors.add(interceptor);
      return this;
    }

    /**
     * Create a new stub configuration
     */
    public S build() {
      return AbstractStub.this.build(ClientInterceptors.intercept(stubChannel, interceptors),
          config.build(methodMap));
    }
  }
}
