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
import com.google.common.base.Preconditions;
import java.util.Collections;
import java.util.HashMap;
import java.util.Set;
import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;

/**
 * An immutable type-safe container of attributes.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1764")
@Immutable
public final class Attributes {

  private final HashMap<Key<?>, Object> data = new HashMap<Key<?>, Object>();

  public static final Attributes EMPTY = new Attributes();

  private Attributes() {
  }

  /**
   * Gets the value for the key, or {@code null} if it's not present.
   */
  @SuppressWarnings("unchecked")
  @Nullable
  public <T> T get(Key<T> key) {
    return (T) data.get(key);
  }

  /**
   * Returns set of keys stored in container.
   *
   * @return Set of Key objects.
   */
  public Set<Key<?>> keys() {
    return Collections.unmodifiableSet(data.keySet());
  }

  /**
   * Create a new builder that is pre-populated with the content from a given container.
   */
  public static Builder newBuilder(Attributes base) {
    return newBuilder().setAll(base);
  }

  /**
   * Create a new builder.
   */
  public static Builder newBuilder() {
    return new Builder();
  }

  /**
   * Key for an key-value pair.
   * @param <T> type of the value in the key-value pair
   */
  @Immutable
  public static final class Key<T> {
    private final String name;

    private Key(String name) {
      this.name = name;
    }

    @Override
    public String toString() {
      return name;
    }

    /**
     * Factory method for creating instances of {@link Key}.
     *
     * @param name the name of Key. Name collision, won't cause key collision.
     * @param <T> Key type
     * @return Key object
     */
    public static <T> Key<T> of(String name) {
      return new Key<T>(name);
    }
  }

  @Override
  public String toString() {
    return data.toString();
  }

  /**
   * Returns true if the given object is also a {@link Attributes} with an equal attribute values.
   *
   * <p>Note that if a stored values are mutable, it is possible for two objects to be considered
   * equal at one point in time and not equal at another (due to concurrent mutation of attribute
   * values).
   *
   * @param o an object.
   * @return true if the given object is a {@link Attributes} equal attributes.
   */
  @Override
  public boolean equals(Object o) {
    if (this == o) {
      return true;
    }
    if (o == null || getClass() != o.getClass()) {
      return false;
    }
    Attributes that = (Attributes) o;
    return Objects.equal(data, that.data);
  }

  /**
   * Returns a hash code for the attributes.
   *
   * <p>Note that if a stored values are mutable, it is possible for two objects to be considered
   * equal at one point in time and not equal at another (due to concurrent mutation of attribute
   * values).
   *
   * @return a hash code for the attributes map.
   */
  @Override
  public int hashCode() {
    return data.hashCode();
  }

  /**
   * The helper class to build an Attributes instance.
   */
  public static final class Builder {
    private Attributes product;

    private Builder() {
      this.product = new Attributes();
    }

    public <T> Builder set(Key<T> key, T value) {
      product.data.put(key, value);
      return this;
    }

    public <T> Builder setAll(Attributes other) {
      product.data.putAll(other.data);
      return this;
    }

    /**
     * Build the attributes. Can only be called once.
     */
    public Attributes build() {
      Preconditions.checkState(product != null, "Already built");
      Attributes result = product;
      product = null;
      return result;
    }
  }
}
