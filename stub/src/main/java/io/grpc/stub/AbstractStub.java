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

package io.grpc.stub;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

import javax.annotation.Nullable;

/**
 * Common base type for stub implementations.
 *
 * <p>This is the base class of the stub classes from the generated code. It allows for
 * reconfiguration, e.g., attaching interceptors to the stub.
 *
 * @param <S> the concrete type of this stub.
 */
public abstract class AbstractStub<S extends AbstractStub<?>> {
  protected final Channel channel;
  protected final CallOptions callOptions;

  /**
   * Constructor for use by subclasses, with the default {@code CallOptions}.
   *
   * @param channel the channel that this stub will use to do communications
   */
  protected AbstractStub(Channel channel) {
    this(channel, CallOptions.DEFAULT);
  }

  /**
   * Constructor for use by subclasses, with the default {@code CallOptions}.
   *
   * @param channel the channel that this stub will use to do communications
   * @param callOptions the runtime call options to be applied to every call on this stub
   */
  protected AbstractStub(Channel channel, CallOptions callOptions) {
    this.channel = channel;
    this.callOptions = callOptions;
  }

  /**
   * Creates a builder for reconfiguring the stub.
   */
  public StubConfigBuilder configureNewStub() {
    return new StubConfigBuilder();
  }

  /**
   * The underlying channel of the stub.
   */
  public Channel getChannel() {
    return channel;
  }

  /**
   * The {@code CallOptions} of the stub.
   */
  public CallOptions getCallOptions() {
    return callOptions;
  }

  /**
   * Returns a new stub with the given channel for the provided method configurations.
   *
   * @param channel the channel that this stub will use to do communications
   * @param callOptions the runtime call options to be applied to every call on this stub
   */
  protected abstract S build(Channel channel, CallOptions callOptions);

  /**
   * Utility class for (re) configuring the operations in a stub.
   */
  public class StubConfigBuilder {

    private final List<ClientInterceptor> interceptors = new ArrayList<ClientInterceptor>();
    private CallOptions callOptions = AbstractStub.this.callOptions;
    private Channel stubChannel;

    private StubConfigBuilder() {
      this.stubChannel = AbstractStub.this.channel;
    }

    /**
     * Sets an absolute deadline in nanoseconds in the clock as per {@link System#nanoTime()}.
     *
     * <p>This is mostly used for propagating an existing deadline. {@link #setDeadlineAfter} is the
     * recommended way of setting a new deadline,
     *
     * @param deadlineNanoTime nanoseconds in the clock as per {@link System#nanoTime()}
     */
    public StubConfigBuilder setDeadlineNanoTime(@Nullable Long deadlineNanoTime) {
      callOptions = callOptions.withDeadlineNanoTime(deadlineNanoTime);
      return this;
    }

    /**
     * Sets a deadline that is after the given {@code duration} from now.
     *
     * @see CallOptions#withDeadlineAfter
     */
    public StubConfigBuilder setDeadlineAfter(long duration, TimeUnit unit) {
      callOptions = callOptions.withDeadlineAfter(duration, unit);
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
     * Adds a client interceptor to be attached to the channel of the reconfigured stub.
     */
    public StubConfigBuilder addInterceptor(ClientInterceptor interceptor) {
      interceptors.add(interceptor);
      return this;
    }

    /**
     * Create a new stub with the configurations made on this builder.
     */
    public S build() {
      return AbstractStub.this.build(
          ClientInterceptors.intercept(stubChannel, interceptors), callOptions);
    }
  }
}
