package com.google.net.stubby;

import static com.google.common.base.Charsets.US_ASCII;
import static com.google.common.base.Charsets.UTF_8;

import com.google.common.base.Function;
import com.google.common.base.Preconditions;
import com.google.common.collect.Iterables;
import com.google.common.collect.Iterators;
import com.google.common.collect.LinkedListMultimap;
import com.google.common.collect.ListMultimap;
import com.google.common.collect.Lists;

import java.util.Iterator;
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
   * Interleave keys and values into a single iterator.
   */
  private static Iterator<String> fromMapEntries(Iterable<Map.Entry<String, String>> entries) {
    final Iterator<Map.Entry<String, String>> iterator = entries.iterator();
    return new Iterator<String>() {
      Map.Entry<String, String> last;
      @Override
      public boolean hasNext() {
        return last != null || iterator.hasNext();
      }

      @Override
      public String next() {
        if (last == null) {
          last = iterator.next();
          return last.getKey();
        } else {
          String val = last.getValue();
          last = null;
          return val;
        }
      }

      @Override
      public void remove() {
        throw new UnsupportedOperationException();
      }
    };
  }

  /**
   * Simple metadata marshaller that encodes strings as either UTF-8 or ASCII bytes.
   */
  public static final Marshaller<String> STRING_MARSHALLER =
      new Marshaller<String>() {

    @Override
    public byte[] toBytes(String value) {
      return value.getBytes(UTF_8);
    }

    @Override
    public String toAscii(String value) {
      return value;
    }

    @Override
    public String parseBytes(byte[] serialized) {
      return new String(serialized, UTF_8);
    }

    @Override
    public String parseAscii(String ascii) {
      return ascii;
    }
  };

  /**
   * Simple metadata marshaller that encodes an integer as a signed decimal string or as big endian
   * binary with four bytes.
   */
  public static final Marshaller<Integer> INTEGER_MARSHALLER = new Marshaller<Integer>() {
    @Override
    public byte[] toBytes(Integer value) {
      return new byte[] {
        (byte) (value >>> 24),
        (byte) (value >>> 16),
        (byte) (value >>> 8),
        (byte) (value >>> 0)};
    }

    @Override
    public String toAscii(Integer value) {
      return value.toString();
    }

    @Override
    public Integer parseBytes(byte[] serialized) {
      if (serialized.length != 4) {
        throw new IllegalArgumentException("Can only deserialize 4 bytes into an integer");
      }
      return (serialized[0] << 24)
          |  (serialized[1] << 16)
          |  (serialized[2] << 8)
          |   serialized[3];
    }

    @Override
    public Integer parseAscii(String ascii) {
      return Integer.valueOf(ascii);
    }
  };

  private final ListMultimap<String, MetadataEntry> store;
  private final boolean serializable;

  /**
   * Constructor called by the transport layer when it receives binary metadata.
   */
  // TODO(user): Convert to use ByteString so we can cache transformations
  private Metadata(byte[]... binaryValues) {
    store = LinkedListMultimap.create();
    for (int i = 0; i < binaryValues.length; i++) {
      String name = new String(binaryValues[i], US_ASCII);
      store.put(name, new MetadataEntry(binaryValues[++i]));
    }
    this.serializable = false;
  }

  /**
   * Constructor called by the transport layer when it receives ASCII metadata.
   */
  private Metadata(String... asciiValues) {
    store = LinkedListMultimap.create();
    for (int i = 0; i < asciiValues.length; i++) {
      store.put(asciiValues[i], new MetadataEntry(asciiValues[++i]));
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
  public <T> boolean containsKey(Key key) {
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
   * Serialize all the metadata entries
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
   * Serialize all the metadata entries
   */
  public String[] serializeAscii() {
    Preconditions.checkState(serializable, "Can't serialize received metadata");
    String[] serialized = new String[store.size() * 2];
    int i = 0;
    for (Map.Entry<String, MetadataEntry> entry : store.entries()) {
      serialized[i++] = entry.getValue().key.name();
      serialized[i++] = entry.getValue().getSerializedAscii();
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
  public void merge(Metadata other, Set<Key> keys) {
    Preconditions.checkNotNull(other);
    for (Key key : keys) {
      if (other.containsKey(key)) {
        Iterable values = other.getAll(key);
        for (Object value : values) {
          put(key, value);
        }
      }
    }
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
     * Called by the transport layer to create headers from their ASCII serialized values.
     */
    public Headers(String... asciiValues) {
      super(asciiValues);
    }

    /**
     * Called by the transport layer to create headers from their ASCII serialized values.
     */
    public Headers(Iterable<Map.Entry<String, String>> mapEntries) {
      super(Iterators.toArray(fromMapEntries(mapEntries), String.class));
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

    public void setAuthority(String authority) {
      this.authority = authority;
    }

    @Override
    public void merge(Metadata other) {
      super.merge(other);
      mergePathAndAuthority(other);
    }

    @Override
    public void merge(Metadata other, Set<Key> keys) {
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
     * Called by the transport layer to create trailers from their ASCII serialized values.
     */
    public Trailers(String... asciiValues) {
      super(asciiValues);
    }

    /**
     * Called by the transport layer to create headers from their ASCII serialized values.
     */
    public Trailers(Iterable<Map.Entry<String, String>> mapEntries) {
      super(Iterators.toArray(fromMapEntries(mapEntries), String.class));
    }

    /**
     * Called by the application layer to construct trailers prior to passing them to the
     * transport for serialization.
     */
    public Trailers() {
    }
  }


  /**
   * Marshaller for metadata values.
   */
  public static interface Marshaller<T> {
    /**
     * Serialize a metadata value to bytes.
     * @param value to serialize
     * @return serialized version of value, or null if value cannot be transmitted.
     */
    public byte[] toBytes(T value);

    /**
     * Serialize a metadata value to an ASCII string
     * @param value to serialize
     * @return serialized ascii version of value, or null if value cannot be transmitted.
     */
    public String toAscii(T value);

    /**
     * Parse a serialized metadata value from bytes.
     * @param serialized value of metadata to parse
     * @return a parsed instance of type T
     */
    public T parseBytes(byte[] serialized);

    /**
     * Parse a serialized metadata value from an ascii string.
     * @param ascii string value of metadata to parse
     * @return a parsed instance of type T
     */
    public T parseAscii(String ascii);
  }

  /**
   * Key for metadata entries. Allows for parsing and serialization of metadata.
   */
  public static class Key<T> {
    public static <T> Key<T> of(String name, Marshaller<T> marshaller) {
      return new Key<T>(name, marshaller);
    }

    private final String name;
    private final byte[] asciiName;
    private final Marshaller<T> marshaller;

    /**
     * Keys have a name and a marshaller used for serialization.
     */
    private Key(String name, Marshaller<T> marshaller) {
      this.name = Preconditions.checkNotNull(name, "name").toLowerCase().intern();
      this.asciiName = name.getBytes(US_ASCII);
      this.marshaller = Preconditions.checkNotNull(marshaller);
    }

    public String name() {
      return name;
    }

    // TODO (lryan): Migrate to ByteString
    public byte[] asciiName() {
      return asciiName;
    }

    public Marshaller<T> getMarshaller() {
      return marshaller;
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) return true;
      if (o == null || getClass() != o.getClass()) return false;
      Key key = (Key) o;
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
  }

  private static class MetadataEntry {
    Object parsed;
    Key key;
    byte[] serializedBinary;
    String serializedAscii;

    /**
     * Constructor used when application layer adds a parsed value.
     */
    private MetadataEntry(Key key, Object parsed) {
      this.parsed = Preconditions.checkNotNull(parsed);
      this.key = Preconditions.checkNotNull(key);
    }

    /**
     * Constructor used when reading a value from the transport.
     */
    private MetadataEntry(byte[] serialized) {
      Preconditions.checkNotNull(serialized);
      this.serializedBinary = serialized;
    }

    /**
     * Constructor used when reading a value from the transport.
     */
    private MetadataEntry(String serializedAscii) {
      this.serializedAscii = Preconditions.checkNotNull(serializedAscii);
    }

    public <T> T getParsed(Key<T> key) {
      @SuppressWarnings("unchecked")
      T value = (T) parsed;
      if (value != null) {
        if (this.key != key) {
          // Keys don't match so serialize using the old key
          serializedBinary = this.key.getMarshaller().toBytes(value);
        } else {
          return value;
        }
      }
      this.key = key;
      if (serializedBinary != null) {
        value = key.getMarshaller().parseBytes(serializedBinary);
      } else if (serializedAscii != null) {
        value = key.getMarshaller().parseAscii(serializedAscii);
      }
      parsed = value;
      return value;
    }

    @SuppressWarnings("unchecked")
    public byte[] getSerialized() {
      return serializedBinary =
          serializedBinary == null
              ? key.getMarshaller().toBytes(parsed) :
              serializedBinary;
    }

    @SuppressWarnings("unchecked")
    public String getSerializedAscii() {
      return serializedAscii =
          serializedAscii == null
              ? key.getMarshaller().toAscii(parsed) :
              serializedAscii;
    }
  }
}
