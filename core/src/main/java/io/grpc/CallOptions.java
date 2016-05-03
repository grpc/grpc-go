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

import com.google.common.base.MoreObjects;
import com.google.common.base.Preconditions;

import java.util.concurrent.Executor;
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
  private Deadline deadline;
  private Executor executor;

  @Nullable
  private String authority;

  private Attributes affinity = Attributes.EMPTY;

  @Nullable
  private String compressorName;

  /**
   * Override the HTTP/2 authority the channel claims to be connecting to. <em>This is not
   * generally safe.</em> Overriding allows advanced users to re-use a single Channel for multiple
   * services, even if those services are hosted on different domain names. That assumes the
   * server is virtually hosting multiple domains and is guaranteed to continue doing so. It is
   * rare for a service provider to make such a guarantee. <em>At this time, there is no security
   * verification of the overridden value, such as making sure the authority matches the server's
   * TLS certificate.</em>
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1767")
  public CallOptions withAuthority(@Nullable String authority) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.authority = authority;
    return newOptions;
  }

  /**
   * Sets the compression to use for the call.  The compressor must be a valid name known in the
   * {@link CompressorRegistry}.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public CallOptions withCompression(@Nullable String compressorName) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.compressorName = compressorName;
    return newOptions;
  }

  /**
   * Returns a new {@code CallOptions} with the given absolute deadline.
   *
   * <p>This is mostly used for propagating an existing deadline. {@link #withDeadlineAfter} is the
   * recommended way of setting a new deadline,
   *
   * @param deadline the deadline or {@code null} for unsetting the deadline.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1706")
  public CallOptions withDeadline(@Nullable Deadline deadline) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.deadline = deadline;
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
   * @deprecated  Use {@link #withDeadline(Deadline)} instead.
   */
  @Deprecated
  public CallOptions withDeadlineNanoTime(@Nullable Long deadlineNanoTime) {
    Deadline deadline = deadlineNanoTime != null
        ? Deadline.after(deadlineNanoTime - System.nanoTime(), TimeUnit.NANOSECONDS)
        : null;
    return withDeadline(deadline);
  }

  /**
   * Returns a new {@code CallOptions} with a deadline that is after the given {@code duration} from
   * now.
   */
  public CallOptions withDeadlineAfter(long duration, TimeUnit unit) {
    return withDeadline(Deadline.after(duration, unit));
  }

  /**
   * Returns the deadline in nanoseconds in the clock as per {@link System#nanoTime()}. {@code null}
   * if the deadline is not set.
   *
   * @deprecated  Use {@link #getDeadline()} instead.
   */
  @Deprecated
  public Long getDeadlineNanoTime() {
    if (getDeadline() == null) {
      return null;
    }
    return System.nanoTime() + getDeadline().timeRemaining(TimeUnit.NANOSECONDS);
  }

  /**
   * Returns the deadline or {@code null} if the deadline is not set.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1706")
  @Nullable
  public Deadline getDeadline() {
    return deadline;
  }

  /**
   * Returns a new {@code CallOptions} with attributes for affinity-based routing.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1766")
  public CallOptions withAffinity(Attributes affinity) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.affinity = Preconditions.checkNotNull(affinity);
    return newOptions;
  }

  /**
   * Returns the attributes for affinity-based routing.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1766")
  public Attributes getAffinity() {
    return affinity;
  }

  /**
   * Returns the compressor's name.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  @Nullable
  public String getCompressor() {
    return compressorName;
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
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1767")
  public String getAuthority() {
    return authority;
  }

  /**
   * Returns a new {@code CallOptions} with {@code executor} to be used instead of the default
   * executor specified with {@link ManagedChannelBuilder#executor}.
   */
  public CallOptions withExecutor(Executor executor) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.executor = executor;
    return newOptions;
  }

  @Nullable
  public Executor getExecutor() {
    return executor;
  }

  private CallOptions() {
  }

  /**
   * Copy constructor.
   */
  private CallOptions(CallOptions other) {
    deadline = other.deadline;
    authority = other.authority;
    affinity = other.affinity;
    executor = other.executor;
    compressorName = other.compressorName;
  }

  @Override
  public String toString() {
    MoreObjects.ToStringHelper toStringHelper = MoreObjects.toStringHelper(this);
    toStringHelper.add("deadline", deadline);
    toStringHelper.add("authority", authority);
    toStringHelper.add("affinity", affinity);
    toStringHelper.add("executor", executor != null ? executor.getClass() : null);
    toStringHelper.add("compressorName", compressorName);

    return toStringHelper.toString();
  }
}
