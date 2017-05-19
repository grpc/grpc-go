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

import static com.google.common.base.Preconditions.checkArgument;

import com.google.common.base.MoreObjects;
import com.google.common.base.Preconditions;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
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

  @Nullable
  private CallCredentials credentials;

  @Nullable
  private String compressorName;

  private Object[][] customOptions = new Object[0][2];

  // Unmodifiable list
  private List<ClientStreamTracer.Factory> streamTracerFactories = Collections.emptyList();

  /**
   * Opposite to fail fast.
   */
  private boolean waitForReady;

  @Nullable
  private Integer maxInboundMessageSize;
  @Nullable
  private Integer maxOutboundMessageSize;


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
   * Returns a new {@code CallOptions} with the given call credentials.
   */
  @ExperimentalApi("https//github.com/grpc/grpc-java/issues/1914")
  public CallOptions withCallCredentials(@Nullable CallCredentials credentials) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.credentials = credentials;
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
  public CallOptions withDeadline(@Nullable Deadline deadline) {
    CallOptions newOptions = new CallOptions(this);
    newOptions.deadline = deadline;
    return newOptions;
  }

  /**
   * Returns a new {@code CallOptions} with a deadline that is after the given {@code duration} from
   * now.
   */
  public CallOptions withDeadlineAfter(long duration, TimeUnit unit) {
    return withDeadline(Deadline.after(duration, unit));
  }

  /**
   * Returns the deadline or {@code null} if the deadline is not set.
   */
  @Nullable
  public Deadline getDeadline() {
    return deadline;
  }

  /**
   * Enables <a href="https://github.com/grpc/grpc/blob/master/doc/wait-for-ready.md">
   * 'wait for ready'</a> feature for the call. 'Fail fast' is the default option for gRPC calls
   * and 'wait for ready' is the opposite to it.
   */
  public CallOptions withWaitForReady() {
    CallOptions newOptions = new CallOptions(this);
    newOptions.waitForReady = true;
    return newOptions;
  }

  /**
   * Disables 'wait for ready' feature for the call.
   * This method should be rarely used because the default is without 'wait for ready'.
   */
  public CallOptions withoutWaitForReady() {
    CallOptions newOptions = new CallOptions(this);
    newOptions.waitForReady = false;
    return newOptions;
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
   * Returns the call credentials.
   */
  @ExperimentalApi("https//github.com/grpc/grpc-java/issues/1914")
  @Nullable
  public CallCredentials getCredentials() {
    return credentials;
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

  /**
   * Returns a new {@code CallOptions} with a {@code ClientStreamTracerFactory} in addition to
   * the existing factories.
   *
   * <p>This method doesn't replace existing factories, or try to de-duplicate factories.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2861")
  public CallOptions withStreamTracerFactory(ClientStreamTracer.Factory factory) {
    CallOptions newOptions = new CallOptions(this);
    ArrayList<ClientStreamTracer.Factory> newList =
        new ArrayList<ClientStreamTracer.Factory>(streamTracerFactories.size() + 1);
    newList.addAll(streamTracerFactories);
    newList.add(factory);
    newOptions.streamTracerFactories = Collections.unmodifiableList(newList);
    return newOptions;
  }

  /**
   * Returns an immutable list of {@code ClientStreamTracerFactory}s.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2861")
  public List<ClientStreamTracer.Factory> getStreamTracerFactories() {
    return streamTracerFactories;
  }

  /**
   * Key for a key-value pair. Uses reference equality.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1869")
  public static final class Key<T> {
    private final String name;
    private final T defaultValue;

    private Key(String name, T defaultValue) {
      this.name = name;
      this.defaultValue = defaultValue;
    }

    /**
     * Returns the user supplied default value for this key.
     */
    public T getDefault() {
      return defaultValue;
    }

    @Override
    public String toString() {
      return name;
    }

    /**
     * Factory method for creating instances of {@link Key}.
     *
     * @param name the name of Key.
     * @param defaultValue default value to return when value for key not set
     * @param <T> Key type
     * @return Key object
     */
    public static <T> Key<T> of(String name, T defaultValue) {
      Preconditions.checkNotNull(name, "name");
      return new Key<T>(name, defaultValue);
    }
  }

  /**
   * Sets a custom option. Any existing value for the key is overwritten.
   *
   * @param key The option key
   * @param value The option value.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1869")
  public <T> CallOptions withOption(Key<T> key, T value) {
    Preconditions.checkNotNull(key, "key");
    Preconditions.checkNotNull(value, "value");

    CallOptions newOptions = new CallOptions(this);
    int existingIdx = -1;
    for (int i = 0; i < customOptions.length; i++) {
      if (key.equals(customOptions[i][0])) {
        existingIdx = i;
        break;
      }
    }

    newOptions.customOptions = new Object[customOptions.length + (existingIdx == -1 ? 1 : 0)][2];
    System.arraycopy(customOptions, 0, newOptions.customOptions, 0, customOptions.length);

    if (existingIdx == -1) {
      // Add a new option
      newOptions.customOptions[customOptions.length] = new Object[] {key, value};
    } else {
      // Replace an existing option
      newOptions.customOptions[existingIdx][1] = value;
    }

    return newOptions;
  }

  /**
   * Get the value for a custom option or its inherent default.
   * @param key Key identifying option
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1869")
  @SuppressWarnings("unchecked")
  public <T> T getOption(Key<T> key) {
    Preconditions.checkNotNull(key, "key");
    for (int i = 0; i < customOptions.length; i++) {
      if (key.equals(customOptions[i][0])) {
        return (T) customOptions[i][1];
      }
    }
    return key.defaultValue;
  }

  @Nullable
  public Executor getExecutor() {
    return executor;
  }

  private CallOptions() {
  }

  /**
   * Returns whether <a href="https://github.com/grpc/grpc/blob/master/doc/wait-for-ready.md">
   * 'wait for ready'</a> option is enabled for the call. 'Fail fast' is the default option for gRPC
   * calls and 'wait for ready' is the opposite to it.
   */
  public boolean isWaitForReady() {
    return waitForReady;
  }

  /**
   * Sets the maximum allowed message size acceptable from the remote peer.  If unset, this will
   * default to the value set on the {@link ManagedChannelBuilder#maxInboundMessageSize(int)}.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2563")
  public CallOptions withMaxInboundMessageSize(int maxSize) {
    checkArgument(maxSize >= 0, "invalid maxsize %s", maxSize);
    CallOptions newOptions = new CallOptions(this);
    newOptions.maxInboundMessageSize = maxSize;
    return newOptions;
  }

  /**
   * Sets the maximum allowed message size acceptable sent to the remote peer.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2563")
  public CallOptions withMaxOutboundMessageSize(int maxSize) {
    checkArgument(maxSize >= 0, "invalid maxsize %s", maxSize);
    CallOptions newOptions = new CallOptions(this);
    newOptions.maxOutboundMessageSize = maxSize;
    return newOptions;
  }

  /**
   * Gets the maximum allowed message size acceptable from the remote peer.
   */
  @Nullable
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2563")
  public Integer getMaxInboundMessageSize() {
    return maxInboundMessageSize;
  }

  /**
   * Gets the maximum allowed message size acceptable to send the remote peer.
   */
  @Nullable
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2563")
  public Integer getMaxOutboundMessageSize() {
    return maxOutboundMessageSize;
  }

  /**
   * Copy constructor.
   */
  private CallOptions(CallOptions other) {
    deadline = other.deadline;
    authority = other.authority;
    credentials = other.credentials;
    executor = other.executor;
    compressorName = other.compressorName;
    customOptions = other.customOptions;
    waitForReady = other.waitForReady;
    maxInboundMessageSize = other.maxInboundMessageSize;
    maxOutboundMessageSize = other.maxOutboundMessageSize;
    streamTracerFactories = other.streamTracerFactories;
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("deadline", deadline)
        .add("authority", authority)
        .add("callCredentials", credentials)
        .add("executor", executor != null ? executor.getClass() : null)
        .add("compressorName", compressorName)
        .add("customOptions", Arrays.deepToString(customOptions))
        .add("waitForReady", isWaitForReady())
        .add("maxInboundMessageSize", maxInboundMessageSize)
        .add("maxOutboundMessageSize", maxOutboundMessageSize)
        .add("streamTracerFactories", streamTracerFactories)
        .toString();
  }
}
