/*
 * Copyright 2015, Google Inc. All rights reserved.
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

package io.grpc;

import com.google.common.base.Objects;

import java.util.concurrent.TimeUnit;

import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;

/**
 * The collection of runtime options for a new RPC call.
 *
 * <p>A field that is not set is {@code null}.
 */
@Immutable
public final class CallOptions {
  /**
   * A blank {@code CallOptions} that all fields are not set.
   */
  public static final CallOptions DEFAULT = new CallOptions();

  // Although {@code CallOptions} is immutable, its fields are not final, so that we can initialize
  // them outside of constructor. Otherwise the constructor will have a potentially long list of
  // unnamed arguments, which is undesirable.
  private Long deadlineNanoTime;


  @Nullable
  private Compressor compressor;

  @Nullable
  private String authority;

  /**
   * Override the HTTP/2 authority the channel claims to be connecting to. <em>This is not
   * generally safe.</em> Overriding allows advanced users to re-use a single Channel for multiple
   * services, even if those services are hosted on different domain names. That assumes the
   * server is virtually hosting multiple domains and is guaranteed to continue doing so. It is
   * rare for a service provider to make such a guarantee. <em>At this time, there is no security
   * verification of the overridden value, such as making sure the authority matches the server's
   * TLS certificate.</em>
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/67")
  public CallOptions withAuthority(@Nullable String authority) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.authority = authority;
    return newOptions;
  }

  /**
   * Returns a new {@code CallOptions} with the given absolute deadline in nanoseconds in the clock
   * as per {@link System#nanoTime()}.
   *
   * <p>This is mostly used for propagating an existing deadline. {@link #withDeadlineAfter} is the
   * recommended way of setting a new deadline,
   *
   * @param deadlineNanoTime the deadline in the clock as per {@link System#nanoTime()}.
   *                         {@code null} for unsetting the deadline.
   */
  public CallOptions withDeadlineNanoTime(@Nullable Long deadlineNanoTime) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.deadlineNanoTime = deadlineNanoTime;
    return newOptions;
  }

  /**
   * Returns a new {@code CallOptions} with a deadline that is after the given {@code duration} from
   * now.
   */
  public CallOptions withDeadlineAfter(long duration, TimeUnit unit) {
    return withDeadlineNanoTime(System.nanoTime() + unit.toNanos(duration));
  }

  /**
   * Returns the deadline in nanoseconds in the clock as per {@link System#nanoTime()}. {@code null}
   * if the deadline is not set.
   */
  @Nullable
  public Long getDeadlineNanoTime() {
    return deadlineNanoTime;
  }

  /**
   * Returns the compressor, or {@code null} if none is set.
   */
  @Nullable
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/492")
  public Compressor getCompressor() {
    return compressor;
  }

  /**
   * Use the desired compression.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/492")
  public CallOptions withCompressor(@Nullable Compressor compressor) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.compressor = compressor;
    return newOptions;
  }

  /**
   * Override the HTTP/2 authority the channel claims to be connecting to. <em>This is not
   * generally safe.</em> Overriding allows advanced users to re-use a single Channel for multiple
   * services, even if those services are hosted on different domain names. That assumes the
   * server is virtually hosting multiple domains and is guaranteed to continue doing so. It is
   * rare for a service provider to make such a guarantee. <em>At this time, there is no security
   * verification of the overridden value, such as making sure the authority matches the server's
   * TLS certificate.</em>
   */
  @Nullable
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/67")
  public String getAuthority() {
    return authority;
  }

  private CallOptions() {
  }

  /**
   * Copy constructor.
   */
  private CallOptions(CallOptions other) {
    deadlineNanoTime = other.deadlineNanoTime;
    compressor = other.compressor;
    authority = other.authority;
  }

  @SuppressWarnings("deprecation") // guava 14.0
  @Override
  public String toString() {
    Objects.ToStringHelper toStringHelper = Objects.toStringHelper(this);
    toStringHelper.add("deadlineNanoTime", deadlineNanoTime);
    if (deadlineNanoTime != null) {
      long remainingNanos = deadlineNanoTime - System.nanoTime();
      toStringHelper.addValue(remainingNanos + " ns from now");
    }
    toStringHelper.add("compressor", compressor);

    return toStringHelper.toString();
  }
}
