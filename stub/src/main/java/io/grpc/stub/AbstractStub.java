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

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors;
import io.grpc.Deadline;
import io.grpc.ExperimentalApi;

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
public abstract class AbstractStub<S extends AbstractStub<S>> {
  private final Channel channel;
  private final CallOptions callOptions;

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
    this.channel = checkNotNull(channel);
    this.callOptions = checkNotNull(callOptions);
  }

  /**
   * The underlying channel of the stub.
   */
  public final Channel getChannel() {
    return channel;
  }

  /**
   * The {@code CallOptions} of the stub.
   */
  public final CallOptions getCallOptions() {
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
   * Returns a new stub with an absolute deadline.
   *
   * <p>This is mostly used for propagating an existing deadline. {@link #withDeadlineAfter} is the
   * recommended way of setting a new deadline,
   *
   * @param deadline the deadline or {@code null} for unsetting the deadline.
   */
  public final S withDeadline(@Nullable Deadline deadline) {
    return build(channel, callOptions.withDeadline(deadline));
  }

  /**
   * Returns a new stub with an absolute deadline in nanoseconds in the clock as per {@link
   * System#nanoTime()}.
   *
   * <p>This is mostly used for propagating an existing deadline. {@link #withDeadlineAfter} is the
   * recommended way of setting a new deadline,
   *
   * @param deadlineNanoTime nanoseconds in the clock as per {@link System#nanoTime()}
   * @deprecated  Use {@link #withDeadline(Deadline)} instead.
   */
  @Deprecated
  public final S withDeadlineNanoTime(@Nullable Long deadlineNanoTime) {
    return build(channel, callOptions.withDeadlineNanoTime(deadlineNanoTime));
  }

  /**
   * Returns a new stub with a deadline that is after the given {@code duration} from now.
   *
   * @see CallOptions#withDeadlineAfter
   */
  public final S withDeadlineAfter(long duration, TimeUnit unit) {
    return build(channel, callOptions.withDeadlineAfter(duration, unit));
  }

  /**
   *  Set's the compressor name to use for the call.  It is the responsibility of the application
   *  to make sure the server supports decoding the compressor picked by the client.  To be clear,
   *  this is the compressor used by the stub to compress messages to the server.  To get
   *  compressed responses from the server, set the appropriate {@link io.grpc.DecompressorRegistry}
   *  on the {@link io.grpc.ManagedChannelBuilder}.
   *
   * @param compressorName the name (e.g. "gzip") of the compressor to use.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public final S withCompression(String compressorName) {
    return build(channel, callOptions.withCompression(compressorName));
  }

  /**
   * Returns a new stub that uses the given channel.
   */
  public final S withChannel(Channel newChannel) {
    return build(newChannel, callOptions);
  }
  
  /**
   * Sets a custom option to be passed to client interceptors on the channel
   * {@link io.grpc.ClientInterceptor} via the CallOptions parameter.
   * @param key the option being set
   * @param value the value for the key
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1869")
  public final <T> S withOption(CallOptions.Key<T> key, T value) {
    return build(channel, callOptions.withOption(key, value));
  }

  /**
   * Returns a new stub that has the given interceptors attached to the underlying channel.
   */
  public final S withInterceptors(ClientInterceptor... interceptors) {
    return build(ClientInterceptors.intercept(channel, interceptors), callOptions);
  }
}
