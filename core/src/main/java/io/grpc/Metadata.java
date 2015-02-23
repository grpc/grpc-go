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

package io.grpc;

import static com.google.common.base.Charsets.US_ASCII;

import com.google.common.base.Function;
import com.google.common.base.Preconditions;
import com.google.common.collect.Iterables;
import com.google.common.collect.LinkedListMultimap;
import com.google.common.collect.ListMultimap;
import com.google.common.collect.Lists;

import java.util.Arrays;
import java.util.List;
import java.util.Map;
import java.util.Set;

import javax.annotation.concurrent.NotThreadSafe;

/**
 * Provides access to read and write metadata values to be exchanged during a call.
 * <p>
 * This class is not thread safe, implementations should ensure that header reads and writes
 * do not occur in multiple threads concurrently.
 * </p>
 */
@NotThreadSafe
public abstract class Metadata {

  /**
   * All binary headers should have this suffix in their names. Vice versa.
   */
  public static final String BINARY_HEADER_SUFFIX = "-bin";

  /**
   * Simple metadata marshaller that encodes strings as is.
   *
   * <p>This should be used with ASCII strings that only contain printable characters and space.
   * Otherwise the output may be considered invalid and discarded by the transport.
   */
  public static final AsciiMarshaller<String> ASCII_STRING_MARSHALLER =
      new AsciiMarshaller<String>() {

    @Override
    public String toAsciiString(String value) {
      return value;
    }

    @Override
    public String parseAsciiString(String serialized) {
      return serialized;
    }
  };

  /**
   * Simple metadata marshaller that encodes an integer as a signed decimal string.
   */
  public static final AsciiMarshaller<Integer> INTEGER_MARSHALLER = new AsciiMarshaller<Integer>() {

    @Override
    public String toAsciiString(Integer value) {
      return value.toString();
    }

    @Override
    public Integer parseAsciiString(String serialized) {
      return Integer.parseInt(serialized);
    }
  };

  private final ListMultimap<String, MetadataEntry> store;
  private final boolean serializable;

  /**
   * Constructor called by the transport layer when it receives binary metadata.
   */
  // TODO(louiscryan): Convert to use ByteString so we can cache transformations
  private Metadata(byte[]... binaryValues) {
    store = LinkedListMultimap.create();
    for (int i = 0; i < binaryValues.length; i++) {
      String name = new String(binaryValues[i], US_ASCII);
      store.put(name, new MetadataEntry(name.endsWith(BINARY_HEADER_SUFFIX), binaryValues[++i]));
    }
    this.serializable = false;
  }

  /**
   * Constructor called by the application layer when it wants to send metadata.
   */
  private Metadata() {
    store = LinkedListMultimap.create();
    this.serializable = true;
  }

  /**
   * Returns true if a value is defined for the given key.
   */
  public boolean containsKey(Key<?> key) {
    return store.containsKey(key.name);
  }

  /**
   * Returns the last metadata entry added with the name 'name' parsed as T.
   * @return the parsed metadata entry or null if there are none.
   */
  public <T> T get(Key<T> key) {
    if (containsKey(key)) {
      MetadataEntry metadataEntry = Iterables.getLast(store.get(key.name()));
      return metadataEntry.getParsed(key);
    }
    return null;
  }

  /**
   * Returns all the metadata entries named 'name', in the order they were received,
   * parsed as T or null if there are none.
   */
  public <T> Iterable<T> getAll(final Key<T> key) {
    if (containsKey(key)) {
      return Iterables.transform(
          store.get(key.name()),
          new Function<MetadataEntry, T>() {
            @Override
            public T apply(MetadataEntry entry) {
              return entry.getParsed(key);
            }
          });
    }
    return null;
  }

  public <T> void put(Key<T> key, T value) {
    store.put(key.name(), new MetadataEntry(key, value));
  }

  /**
   * Remove a specific value.
   */
  public <T> boolean remove(Key<T> key, T value) {
    return store.remove(key.name(), value);
  }

  /**
   * Remove all values for the given key.
   */
  public <T> List<T> removeAll(final Key<T> key) {
    return Lists.transform(store.removeAll(key.name()), new Function<MetadataEntry, T>() {
      @Override
      public T apply(MetadataEntry metadataEntry) {
        return metadataEntry.getParsed(key);
      }
    });
  }

  /**
   * Can this metadata be serialized. Metadata constructed from raw binary or ascii values
   * cannot be serialized without merging it into a serializable instance using
   * {@link #merge(Metadata, java.util.Set)}
   */
  public boolean isSerializable() {
    return serializable;
  }

  /**
   * Serialize all the metadata entries.
   *
   * <p>It produces serialized names and values interleaved. result[i*2] are names, while
   * result[i*2+1] are values.
   *
   * <p>Names are ASCII string bytes. If the name ends with "-bin", the value can be raw binary.
   * Otherwise, the value must be printable ASCII characters or space.
   */
  public byte[][] serialize() {
    Preconditions.checkState(serializable, "Can't serialize raw metadata");
    byte[][] serialized = new byte[store.size() * 2][];
    int i = 0;
    for (Map.Entry<String, MetadataEntry> entry : store.entries()) {
      serialized[i++] = entry.getValue().key.asciiName();
      serialized[i++] = entry.getValue().getSerialized();
    }
    return serialized;
  }

  /**
   * Perform a simple merge of two sets of metadata.
   * <p>
   * Note that we can't merge non-serializable metadata into serializable.
   * </p>
   */
  public void merge(Metadata other) {
    Preconditions.checkNotNull(other);
    if (this.serializable) {
      if (!other.serializable) {
        throw new IllegalArgumentException(
            "Cannot merge non-serializable metadata into serializable metadata without keys");
      }
    }
    store.putAll(other.store);
  }

  /**
   * Merge values for the given set of keys into this set of metadata.
   */
  @SuppressWarnings({"rawtypes", "unchecked"})
  public void merge(Metadata other, Set<Key<?>> keys) {
    Preconditions.checkNotNull(other);
    for (Key<?> key : keys) {
      if (other.containsKey(key)) {
        Iterable<?> values = other.getAll(key);
        for (Object value : values) {
          put((Key) key, value);
        }
      }
    }
  }

  private String toStringInternal() {
    return store.toString();
  }

  /**
   * Concrete instance for metadata attached to the start of a call.
   */
  public static class Headers extends Metadata {
    private String path;
    private String authority;

    /**
     * Called by the transport layer to create headers from their binary serialized values.
     */
    public Headers(byte[]... headers) {
      super(headers);
    }

    /**
     * Called by the application layer to construct headers prior to passing them to the
     * transport for serialization.
     */
    public Headers() {
    }

    /**
     * The path for the operation.
     */
    public String getPath() {
      return path;
    }

    public void setPath(String path) {
      this.path = path;
    }

    /**
     * The serving authority for the operation.
     */
    public String getAuthority() {
      return authority;
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
    public void setAuthority(String authority) {
      this.authority = authority;
    }

    @Override
    public void merge(Metadata other) {
      super.merge(other);
      mergePathAndAuthority(other);
    }

    @Override
    public void merge(Metadata other, Set<Key<?>> keys) {
      super.merge(other, keys);
      mergePathAndAuthority(other);
    }

    private void mergePathAndAuthority(Metadata other) {
      if (other instanceof Headers) {
        Headers otherHeaders = (Headers) other;
        path = otherHeaders.path != null ? otherHeaders.path : path;
        authority = otherHeaders.authority != null ? otherHeaders.authority : authority;
      }
    }

    @Override
    public String toString() {
      return "Headers(path=" + path
          + ",authority=" + authority
          + ",metadata=" + super.toStringInternal() + ")";
    }
  }

  /**
   * Concrete instance for metadata attached to the end of the call. Only provided by
   * servers.
   */
  public static class Trailers extends Metadata {
    /**
     * Called by the transport layer to create trailers from their binary serialized values.
     */
    public Trailers(byte[]... headers) {
      super(headers);
    }

    /**
     * Called by the application layer to construct trailers prior to passing them to the
     * transport for serialization.
     */
    public Trailers() {
    }

    @Override
    public String toString() {
      return "Trailers(" + super.toStringInternal() + ")";
    }
  }


  /**
   * Marshaller for metadata values that are serialized into raw binary.
   */
  public static interface BinaryMarshaller<T> {
    /**
     * Serialize a metadata value to bytes.
     * @param value to serialize
     * @return serialized version of value
     */
    public byte[] toBytes(T value);

    /**
     * Parse a serialized metadata value from bytes.
     * @param serialized value of metadata to parse
     * @return a parsed instance of type T
     */
    public T parseBytes(byte[] serialized);
  }

  /**
   * Marshaller for metadata values that are serialized into ASCII strings that contain only
   * printable characters and space.
   */
  public static interface AsciiMarshaller<T> {
    /**
     * Serialize a metadata value to a ASCII string that contains only printable characters and
     * space.
     *
     * @param value to serialize
     * @return serialized version of value, or null if value cannot be transmitted.
     */
    public String toAsciiString(T value);

    /**
     * Parse a serialized metadata value from an ASCII string.
     * @param serialized value of metadata to parse
     * @return a parsed instance of type T
     */
    public T parseAsciiString(String serialized);
  }

  /**
   * Key for metadata entries. Allows for parsing and serialization of metadata.
   */
  public abstract static class Key<T> {

    /**
     * Creates a key for a binary header.
     *
     * @param name must end with {@link #BINARY_HEADER_SUFFIX}
     */
    public static <T> Key<T> of(String name, BinaryMarshaller<T> marshaller) {
      return new BinaryKey<T>(name, marshaller);
    }

    /**
     * Creates a key for a ASCII header.
     *
     * @param name must not end with {@link #BINARY_HEADER_SUFFIX}
     */
    public static <T> Key<T> of(String name, AsciiMarshaller<T> marshaller) {
      return new AsciiKey<T>(name, marshaller);
    }

    private final String name;
    private final byte[] asciiName;

    private Key(String name) {
      this.name = Preconditions.checkNotNull(name, "name").toLowerCase().intern();
      this.asciiName = this.name.getBytes(US_ASCII);
    }

    public String name() {
      return name;
    }

    // TODO (louiscryan): Migrate to ByteString
    public byte[] asciiName() {
      return asciiName;
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) return true;
      if (o == null || getClass() != o.getClass()) return false;
      Key<?> key = (Key<?>) o;
      return !(name != null ? !name.equals(key.name) : key.name != null);
    }

    @Override
    public int hashCode() {
      return name != null ? name.hashCode() : 0;
    }

    @Override
    public String toString() {
      return "Key{name='" + name + "'}";
    }

    /**
     * Serialize a metadata value to bytes.
     * @param value to serialize
     * @return serialized version of value
     */
    abstract byte[] toBytes(T value);

    /**
     * Parse a serialized metadata value from bytes.
     * @param serialized value of metadata to parse
     * @return a parsed instance of type T
     */
    abstract T parseBytes(byte[] serialized);
  }

  private static class BinaryKey<T> extends Key<T> {
    private final BinaryMarshaller<T> marshaller;

    /**
     * Keys have a name and a binary marshaller used for serialization.
     */
    private BinaryKey(String name, BinaryMarshaller<T> marshaller) {
      super(name);
      Preconditions.checkArgument(name.endsWith(BINARY_HEADER_SUFFIX),
          "Binary header is named " + name + ". It must end with " + BINARY_HEADER_SUFFIX);
      this.marshaller = Preconditions.checkNotNull(marshaller);
    }

    @Override
    byte[] toBytes(T value) {
      return marshaller.toBytes(value);
    }

    @Override
    T parseBytes(byte[] serialized) {
      return marshaller.parseBytes(serialized);
    }
  }

  private static class AsciiKey<T> extends Key<T> {
    private final AsciiMarshaller<T> marshaller;

    /**
     * Keys have a name and an ASCII marshaller used for serialization.
     */
    private AsciiKey(String name, AsciiMarshaller<T> marshaller) {
      super(name);
      Preconditions.checkArgument(!name.endsWith(BINARY_HEADER_SUFFIX),
          "ASCII header is named " + name + ". It must not end with " + BINARY_HEADER_SUFFIX);
      this.marshaller = Preconditions.checkNotNull(marshaller);
    }

    @Override
    byte[] toBytes(T value) {
      return marshaller.toAsciiString(value).getBytes(US_ASCII);
    }

    @Override
    T parseBytes(byte[] serialized) {
      return marshaller.parseAsciiString(new String(serialized, US_ASCII));
    }
  }

  private static class MetadataEntry {
    Object parsed;

    @SuppressWarnings("rawtypes")
    Key key;
    boolean isBinary;
    byte[] serializedBinary;

    /**
     * Constructor used when application layer adds a parsed value.
     */
    private MetadataEntry(Key<?> key, Object parsed) {
      this.parsed = Preconditions.checkNotNull(parsed);
      this.key = Preconditions.checkNotNull(key);
      this.isBinary = key instanceof BinaryKey;
    }

    /**
     * Constructor used when reading a value from the transport.
     */
    private MetadataEntry(boolean isBinary, byte[] serialized) {
      Preconditions.checkNotNull(serialized);
      this.serializedBinary = serialized;
      this.isBinary = isBinary;
    }

    @SuppressWarnings("unchecked")
    public <T> T getParsed(Key<T> key) {
      T value = (T) parsed;
      if (value != null) {
        if (this.key != key) {
          // Keys don't match so serialize using the old key
          serializedBinary = this.key.toBytes(value);
        } else {
          return value;
        }
      }
      this.key = key;
      if (serializedBinary != null) {
        value = key.parseBytes(serializedBinary);
      }
      parsed = value;
      return value;
    }

    @SuppressWarnings("unchecked")
    public byte[] getSerialized() {
      return serializedBinary =
          serializedBinary == null
              ? key.toBytes(parsed) : serializedBinary;
    }

    @Override
    public String toString() {
      if (!isBinary) {
        return new String(getSerialized(), US_ASCII);
      } else {
        // Assume that the toString of an Object is better than a binary encoding.
        if (parsed != null) {
          return "" + parsed;
        } else {
          return Arrays.toString(serializedBinary);
        }
      }
    }
  }
}
