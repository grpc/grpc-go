/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts.internal;

import java.math.BigInteger;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import javax.annotation.Nonnull;

/** A set of peer properties. */
public final class TsiPeer {
  private final List<Property<?>> properties;

  public TsiPeer(List<Property<?>> properties) {
    this.properties = Collections.unmodifiableList(properties);
  }

  public List<Property<?>> getProperties() {
    return properties;
  }

  /** Get peer property. */
  public Property<?> getProperty(String name) {
    for (Property<?> property : properties) {
      if (property.getName().equals(name)) {
        return property;
      }
    }
    return null;
  }

  @Override
  public String toString() {
    return new ArrayList<>(properties).toString();
  }

  /** A peer property. */
  public abstract static class Property<T> {
    private final String name;
    private final T value;

    public Property(@Nonnull String name, @Nonnull T value) {
      this.name = name;
      this.value = value;
    }

    public final T getValue() {
      return value;
    }

    public final String getName() {
      return name;
    }

    @Override
    public String toString() {
      return String.format("%s=%s", name, value);
    }
  }

  /** A peer property corresponding to a signed 64-bit integer. */
  public static final class SignedInt64Property extends Property<Long> {
    public SignedInt64Property(@Nonnull String name, @Nonnull Long value) {
      super(name, value);
    }
  }

  /** A peer property corresponding to an unsigned 64-bit integer. */
  public static final class UnsignedInt64Property extends Property<BigInteger> {
    public UnsignedInt64Property(@Nonnull String name, @Nonnull BigInteger value) {
      super(name, value);
    }
  }

  /** A peer property corresponding to a double. */
  public static final class DoubleProperty extends Property<Double> {
    public DoubleProperty(@Nonnull String name, @Nonnull Double value) {
      super(name, value);
    }
  }

  /** A peer property corresponding to a string. */
  public static final class StringProperty extends Property<String> {
    public StringProperty(@Nonnull String name, @Nonnull String value) {
      super(name, value);
    }
  }

  /** A peer property corresponding to a list of peer properties. */
  public static final class PropertyList extends Property<List<Property<?>>> {
    public PropertyList(@Nonnull String name, @Nonnull List<Property<?>> value) {
      super(name, value);
    }
  }
}
