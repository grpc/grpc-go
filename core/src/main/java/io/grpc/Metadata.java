/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc;

import static com.google.common.base.Charsets.US_ASCII;
import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.io.BaseEncoding;
import java.nio.ByteBuffer;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.BitSet;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Iterator;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.NoSuchElementException;
import java.util.Set;
import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;
import javax.annotation.concurrent.NotThreadSafe;

/**
 * Provides access to read and write metadata values to be exchanged during a call.
 *
 * <p>Keys are allowed to be associated with more than one value.
 *
 * <p>This class is not thread safe, implementations should ensure that header reads and writes do
 * not occur in multiple threads concurrently.
 */
@NotThreadSafe
public final class Metadata {

  /**
   * All binary headers should have this suffix in their names. Vice versa.
   *
   * <p>Its value is {@code "-bin"}. An ASCII header's name must not end with this.
   */
  public static final String BINARY_HEADER_SUFFIX = "-bin";

  /**
   * Simple metadata marshaller that encodes bytes as is.
   *
   * <p>This should be used when raw bytes are favored over un-serialized version of object. Can be
   * helpful in situations where more processing to bytes is needed on application side, avoids
   * double encoding/decoding.
   *
   * <p>Both {@link BinaryMarshaller#toBytes} and {@link BinaryMarshaller#parseBytes} methods do not
   * return a copy of the byte array. Do _not_ modify the byte arrays of either the arguments or
   * return values.
   */
  public static final BinaryMarshaller<byte[]> BINARY_BYTE_MARSHALLER =
      new BinaryMarshaller<byte[]>() {

        @Override
        public byte[] toBytes(byte[] value) {
          return value;
        }

        @Override
        public byte[] parseBytes(byte[] serialized) {
          return serialized;
        }
      };

  /**
   * Simple metadata marshaller that encodes strings as is.
   *
   * <p>This should be used with ASCII strings that only contain the characters listed in the class
   * comment of {@link AsciiMarshaller}. Otherwise the output may be considered invalid and
   * discarded by the transport, or the call may fail.
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
   * Constructor called by the transport layer when it receives binary metadata. Metadata will
   * mutate the passed in array.
   */
  Metadata(byte[]... binaryValues) {
    this(binaryValues.length / 2, binaryValues);
  }

  /**
   * Constructor called by the transport layer when it receives binary metadata. Metadata will
   * mutate the passed in array.
   *
   * @param usedNames the number of
   */
  Metadata(int usedNames, byte[]... binaryValues) {
    assert (binaryValues.length & 1) == 0 : "Odd number of key-value pairs " + binaryValues.length;
    size = usedNames;
    namesAndValues = binaryValues;
  }

  private byte[][] namesAndValues;
  // The unscaled number of headers present.
  private int size;

  private byte[] name(int i) {
    return namesAndValues[i * 2];
  }

  private void name(int i, byte[] name) {
    namesAndValues[i * 2] = name;
  }

  private byte[] value(int i) {
    return namesAndValues[i * 2 + 1];
  }

  private void value(int i, byte[] value) {
    namesAndValues[i * 2 + 1] = value;
  }

  private int cap() {
    return namesAndValues != null ? namesAndValues.length : 0;
  }

  // The scaled version of size.
  private int len() {
    return size * 2;
  }

  private boolean isEmpty() {
    /** checks when {@link #namesAndValues} is null or has no elements */
    return size == 0;
  }

  /** Constructor called by the application layer when it wants to send metadata. */
  public Metadata() {}

  /** Returns the total number of key-value headers in this metadata, including duplicates. */
  int headerCount() {
    return size;
  }

  /**
   * Returns true if a value is defined for the given key.
   *
   * <p>This is done by linear search, so if it is followed by {@link #get} or {@link #getAll},
   * prefer calling them directly and checking the return value against {@code null}.
   */
  public boolean containsKey(Key<?> key) {
    for (int i = 0; i < size; i++) {
      if (bytesEqual(key.asciiName(), name(i))) {
        return true;
      }
    }
    return false;
  }

  /**
   * Returns the last metadata entry added with the name 'name' parsed as T.
   *
   * @return the parsed metadata entry or null if there are none.
   */
  @Nullable
  public <T> T get(Key<T> key) {
    for (int i = size - 1; i >= 0; i--) {
      if (bytesEqual(key.asciiName(), name(i))) {
        return key.parseBytes(value(i));
      }
    }
    return null;
  }

  private final class IterableAt<T> implements Iterable<T> {
    private final Key<T> key;
    private int startIdx;

    private IterableAt(Key<T> key, int startIdx) {
      this.key = key;
      this.startIdx = startIdx;
    }

    @Override
    public Iterator<T> iterator() {
      return new Iterator<T>() {
        private boolean hasNext = true;
        private int idx = startIdx;

        @Override
        public boolean hasNext() {
          if (hasNext) {
            return true;
          }
          for (; idx < size; idx++) {
            if (bytesEqual(key.asciiName(), name(idx))) {
              hasNext = true;
              return hasNext;
            }
          }
          return false;
        }

        @Override
        public T next() {
          if (hasNext()) {
            hasNext = false;
            return key.parseBytes(value(idx++));
          }
          throw new NoSuchElementException();
        }

        @Override
        public void remove() {
          throw new UnsupportedOperationException();
        }
      };
    }
  }

  /**
   * Returns all the metadata entries named 'name', in the order they were received, parsed as T, or
   * null if there are none. The iterator is not guaranteed to be "live." It may or may not be
   * accurate if Metadata is mutated.
   */
  @Nullable
  public <T> Iterable<T> getAll(final Key<T> key) {
    for (int i = 0; i < size; i++) {
      if (bytesEqual(key.asciiName(), name(i))) {
        return new IterableAt<T>(key, i);
      }
    }
    return null;
  }

  /**
   * Returns set of all keys in store.
   *
   * @return unmodifiable Set of keys
   */
  @SuppressWarnings("deprecation") // The String ctor is deprecated, but fast.
  public Set<String> keys() {
    if (isEmpty()) {
      return Collections.emptySet();
    }
    Set<String> ks = new HashSet<String>(size);
    for (int i = 0; i < size; i++) {
      ks.add(new String(name(i), 0 /* hibyte */));
    }
    // immutable in case we decide to change the implementation later.
    return Collections.unmodifiableSet(ks);
  }

  /**
   * Adds the {@code key, value} pair. If {@code key} already has values, {@code value} is added to
   * the end. Duplicate values for the same key are permitted.
   *
   * @throws NullPointerException if key or value is null
   */
  public <T> void put(Key<T> key, T value) {
    Preconditions.checkNotNull(key, "key");
    Preconditions.checkNotNull(value, "value");
    maybeExpand();
    name(size, key.asciiName());
    value(size, key.toBytes(value));
    size++;
  }

  private void maybeExpand() {
    if (len() == 0 || len() == cap()) {
      expand(Math.max(len() * 2, 8));
    }
  }

  // Expands to exactly the desired capacity.
  private void expand(int newCapacity) {
    byte[][] newNamesAndValues = new byte[newCapacity][];
    if (!isEmpty()) {
      System.arraycopy(namesAndValues, 0, newNamesAndValues, 0, len());
    }
    namesAndValues = newNamesAndValues;
  }

  /**
   * Removes the first occurrence of {@code value} for {@code key}.
   *
   * @param key key for value
   * @param value value
   * @return {@code true} if {@code value} removed; {@code false} if {@code value} was not present
   * @throws NullPointerException if {@code key} or {@code value} is null
   */
  public <T> boolean remove(Key<T> key, T value) {
    Preconditions.checkNotNull(key, "key");
    Preconditions.checkNotNull(value, "value");
    for (int i = 0; i < size; i++) {
      if (!bytesEqual(key.asciiName(), name(i))) {
        continue;
      }
      @SuppressWarnings("unchecked")
      T stored = key.parseBytes(value(i));
      if (!value.equals(stored)) {
        continue;
      }
      int writeIdx = i * 2;
      int readIdx = (i + 1) * 2;
      int readLen = len() - readIdx;
      System.arraycopy(namesAndValues, readIdx, namesAndValues, writeIdx, readLen);
      size -= 1;
      name(size, null);
      value(size, null);
      return true;
    }
    return false;
  }

  /** Remove all values for the given key. If there were no values, {@code null} is returned. */
  public <T> Iterable<T> removeAll(Key<T> key) {
    if (isEmpty()) {
      return null;
    }
    int writeIdx = 0;
    int readIdx = 0;
    List<T> ret = null;
    for (; readIdx < size; readIdx++) {
      if (bytesEqual(key.asciiName(), name(readIdx))) {
        ret = ret != null ? ret : new ArrayList<T>();
        ret.add(key.parseBytes(value(readIdx)));
        continue;
      }
      name(writeIdx, name(readIdx));
      value(writeIdx, value(readIdx));
      writeIdx++;
    }
    int newSize = writeIdx;
    // Multiply by two since namesAndValues is interleaved.
    Arrays.fill(namesAndValues, writeIdx * 2, len(), null);
    size = newSize;
    return ret;
  }

  /**
   * Remove all values for the given key without returning them. This is a minor performance
   * optimization if you do not need the previous values.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4691")
  public <T> void discardAll(Key<T> key) {
    if (isEmpty()) {
      return;
    }
    int writeIdx = 0;
    int readIdx = 0;
    for (; readIdx < size; readIdx++) {
      if (bytesEqual(key.asciiName(), name(readIdx))) {
        continue;
      }
      name(writeIdx, name(readIdx));
      value(writeIdx, value(readIdx));
      writeIdx++;
    }
    int newSize = writeIdx;
    // Multiply by two since namesAndValues is interleaved.
    Arrays.fill(namesAndValues, writeIdx * 2, len(), null);
    size = newSize;
  }

  /**
   * Serialize all the metadata entries.
   *
   * <p>It produces serialized names and values interleaved. result[i*2] are names, while
   * result[i*2+1] are values.
   *
   * <p>Names are ASCII string bytes that contains only the characters listed in the class comment
   * of {@link Key}. If the name ends with {@code "-bin"}, the value can be raw binary. Otherwise,
   * the value must contain only characters listed in the class comments of {@link AsciiMarshaller}
   *
   * <p>The returned individual byte arrays <em>must not</em> be modified. However, the top level
   * array may be modified.
   *
   * <p>This method is intended for transport use only.
   */
  @Nullable
  byte[][] serialize() {
    if (len() == cap()) {
      return namesAndValues;
    }
    byte[][] serialized = new byte[len()][];
    System.arraycopy(namesAndValues, 0, serialized, 0, len());
    return serialized;
  }

  /**
   * Perform a simple merge of two sets of metadata.
   *
   * <p>This is a purely additive operation, because a single key can be associated with multiple
   * values.
   */
  public void merge(Metadata other) {
    if (other.isEmpty()) {
      return;
    }
    int remaining = cap() - len();
    if (isEmpty() || remaining < other.len()) {
      expand(len() + other.len());
    }
    System.arraycopy(other.namesAndValues, 0, namesAndValues, len(), other.len());
    size += other.size;
  }

  /**
   * Merge values from the given set of keys into this set of metadata. If a key is present in keys,
   * then all of the associated values will be copied over.
   *
   * @param other The source of the new key values.
   * @param keys The subset of matching key we want to copy, if they exist in the source.
   */
  public void merge(Metadata other, Set<Key<?>> keys) {
    Preconditions.checkNotNull(other, "other");
    // Use ByteBuffer for equals and hashCode.
    Map<ByteBuffer, Key<?>> asciiKeys = new HashMap<ByteBuffer, Key<?>>(keys.size());
    for (Key<?> key : keys) {
      asciiKeys.put(ByteBuffer.wrap(key.asciiName()), key);
    }
    for (int i = 0; i < other.size; i++) {
      ByteBuffer wrappedNamed = ByteBuffer.wrap(other.name(i));
      if (asciiKeys.containsKey(wrappedNamed)) {
        maybeExpand();
        name(size, other.name(i));
        value(size, other.value(i));
        size++;
      }
    }
  }

  @SuppressWarnings("BetaApi") // BaseEncoding is stable in Guava 20.0
  @Override
  public String toString() {
    StringBuilder sb = new StringBuilder("Metadata(");
    for (int i = 0; i < size; i++) {
      if (i != 0) {
        sb.append(',');
      }
      String headerName = new String(name(i), US_ASCII);
      sb.append(headerName).append('=');
      if (headerName.endsWith(BINARY_HEADER_SUFFIX)) {
        sb.append(BaseEncoding.base64().encode(value(i)));
      } else {
        String headerValue = new String(value(i), US_ASCII);
        sb.append(headerValue);
      }
    }
    return sb.append(')').toString();
  }

  private boolean bytesEqual(byte[] left, byte[] right) {
    return Arrays.equals(left, right);
  }

  /** Marshaller for metadata values that are serialized into raw binary. */
  public interface BinaryMarshaller<T> {
    /**
     * Serialize a metadata value to bytes.
     *
     * @param value to serialize
     * @return serialized version of value
     */
    byte[] toBytes(T value);

    /**
     * Parse a serialized metadata value from bytes.
     *
     * @param serialized value of metadata to parse
     * @return a parsed instance of type T
     */
    T parseBytes(byte[] serialized);
  }

  /**
   * Marshaller for metadata values that are serialized into ASCII strings. The strings contain only
   * following characters:
   *
   * <ul>
   * <li>Space: {@code 0x20}, but must not be at the beginning or at the end of the value. Leading
   *     or trailing whitespace may not be preserved.
   * <li>ASCII visible characters ({@code 0x21-0x7E}).
   * </ul>
   *
   * <p>Note this has to be the subset of valid characters in {@code field-content} from RFC 7230
   * Section 3.2.
   */
  public interface AsciiMarshaller<T> {
    /**
     * Serialize a metadata value to a ASCII string that contains only the characters listed in the
     * class comment of {@link AsciiMarshaller}. Otherwise the output may be considered invalid and
     * discarded by the transport, or the call may fail.
     *
     * @param value to serialize
     * @return serialized version of value, or null if value cannot be transmitted.
     */
    String toAsciiString(T value);

    /**
     * Parse a serialized metadata value from an ASCII string.
     *
     * @param serialized value of metadata to parse
     * @return a parsed instance of type T
     */
    T parseAsciiString(String serialized);
  }

  /**
   * Key for metadata entries. Allows for parsing and serialization of metadata.
   *
   * <h3>Valid characters in key names</h3>
   *
   * <p>Only the following ASCII characters are allowed in the names of keys:
   *
   * <ul>
   * <li>digits: {@code 0-9}
   * <li>uppercase letters: {@code A-Z} (normalized to lower)
   * <li>lowercase letters: {@code a-z}
   * <li>special characters: {@code -_.}
   * </ul>
   *
   * <p>This is a strict subset of the HTTP field-name rules. Applications may not send or receive
   * metadata with invalid key names. However, the gRPC library may preserve any metadata received
   * even if it does not conform to the above limitations. Additionally, if metadata contains non
   * conforming field names, they will still be sent. In this way, unknown metadata fields are
   * parsed, serialized and preserved, but never interpreted. They are similar to protobuf unknown
   * fields.
   *
   * <p>Note this has to be the subset of valid HTTP/2 token characters as defined in RFC7230
   * Section 3.2.6 and RFC5234 Section B.1
   *
   * <p>Note that a key is immutable but it may not be deeply immutable, because the key depends on
   * its marshaller, and the marshaller can be mutable though not recommended.
   *
   * @see <a href="https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md">Wire Spec</a>
   * @see <a href="https://tools.ietf.org/html/rfc7230#section-3.2.6">RFC7230</a>
   * @see <a href="https://tools.ietf.org/html/rfc5234#appendix-B.1">RFC5234</a>
   */
  @Immutable
  public abstract static class Key<T> {

    /** Valid characters for field names as defined in RFC7230 and RFC5234. */
    private static final BitSet VALID_T_CHARS = generateValidTChars();

    /**
     * Creates a key for a binary header.
     *
     * @param name Must contain only the valid key characters as defined in the class comment. Must
     *     end with {@link #BINARY_HEADER_SUFFIX}.
     */
    public static <T> Key<T> of(String name, BinaryMarshaller<T> marshaller) {
      return new BinaryKey<T>(name, marshaller);
    }

    /**
     * Creates a key for an ASCII header.
     *
     * @param name Must contain only the valid key characters as defined in the class comment. Must
     *     <b>not</b> end with {@link #BINARY_HEADER_SUFFIX}
     */
    public static <T> Key<T> of(String name, AsciiMarshaller<T> marshaller) {
      return of(name, false, marshaller);
    }

    static <T> Key<T> of(String name, boolean pseudo, AsciiMarshaller<T> marshaller) {
      return new AsciiKey<T>(name, pseudo, marshaller);
    }

    static <T> Key<T> of(String name, boolean pseudo, TrustedAsciiMarshaller<T> marshaller) {
      return new TrustedAsciiKey<T>(name, pseudo, marshaller);
    }

    private final String originalName;

    private final String name;
    private final byte[] nameBytes;

    private static BitSet generateValidTChars() {
      BitSet valid = new BitSet(0x7f);
      valid.set('-');
      valid.set('_');
      valid.set('.');
      for (char c = '0'; c <= '9'; c++) {
        valid.set(c);
      }
      // Only validates after normalization, so we exclude uppercase.
      for (char c = 'a'; c <= 'z'; c++) {
        valid.set(c);
      }
      return valid;
    }

    private static String validateName(String n, boolean pseudo) {
      checkNotNull(n, "name");
      checkArgument(!n.isEmpty(), "token must have at least 1 tchar");
      for (int i = 0; i < n.length(); i++) {
        char tChar = n.charAt(i);
        if (pseudo && tChar == ':' && i == 0) {
          continue;
        }

        checkArgument(
            VALID_T_CHARS.get(tChar), "Invalid character '%s' in key name '%s'", tChar, n);
      }
      return n;
    }

    private Key(String name, boolean pseudo) {
      this.originalName = checkNotNull(name, "name");
      this.name = validateName(this.originalName.toLowerCase(Locale.ROOT), pseudo);
      this.nameBytes = this.name.getBytes(US_ASCII);
    }

    /**
     * @return The original name used to create this key.
     */
    public final String originalName() {
      return originalName;
    }

    /**
     * @return The normalized name for this key.
     */
    public final String name() {
      return name;
    }

    /**
     * Get the name as bytes using ASCII-encoding.
     *
     * <p>The returned byte arrays <em>must not</em> be modified.
     *
     * <p>This method is intended for transport use only.
     */
    // TODO (louiscryan): Migrate to ByteString
    @VisibleForTesting
    byte[] asciiName() {
      return nameBytes;
    }

    /**
     * Returns true if the two objects are both Keys, and their names match (case insensitive).
     */
    @Override
    public boolean equals(Object o) {
      if (this == o) {
        return true;
      }
      if (o == null || getClass() != o.getClass()) {
        return false;
      }
      Key<?> key = (Key<?>) o;
      return name.equals(key.name);
    }

    @Override
    public int hashCode() {
      return name.hashCode();
    }

    @Override
    public String toString() {
      return "Key{name='" + name + "'}";
    }

    /**
     * Serialize a metadata value to bytes.
     *
     * @param value to serialize
     * @return serialized version of value
     */
    abstract byte[] toBytes(T value);

    /**
     * Parse a serialized metadata value from bytes.
     *
     * @param serialized value of metadata to parse
     * @return a parsed instance of type T
     */
    abstract T parseBytes(byte[] serialized);
  }

  private static class BinaryKey<T> extends Key<T> {
    private final BinaryMarshaller<T> marshaller;

    /** Keys have a name and a binary marshaller used for serialization. */
    private BinaryKey(String name, BinaryMarshaller<T> marshaller) {
      super(name, false /* not pseudo */);
      checkArgument(
          name.endsWith(BINARY_HEADER_SUFFIX),
          "Binary header is named %s. It must end with %s",
          name,
          BINARY_HEADER_SUFFIX);
      checkArgument(name.length() > BINARY_HEADER_SUFFIX.length(), "empty key name");
      this.marshaller = checkNotNull(marshaller, "marshaller is null");
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

    /** Keys have a name and an ASCII marshaller used for serialization. */
    private AsciiKey(String name, boolean pseudo, AsciiMarshaller<T> marshaller) {
      super(name, pseudo);
      Preconditions.checkArgument(
          !name.endsWith(BINARY_HEADER_SUFFIX),
          "ASCII header is named %s.  Only binary headers may end with %s",
          name,
          BINARY_HEADER_SUFFIX);
      this.marshaller = Preconditions.checkNotNull(marshaller, "marshaller");
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

  private static final class TrustedAsciiKey<T> extends Key<T> {
    private final TrustedAsciiMarshaller<T> marshaller;

    /** Keys have a name and an ASCII marshaller used for serialization. */
    private TrustedAsciiKey(String name, boolean pseudo, TrustedAsciiMarshaller<T> marshaller) {
      super(name, pseudo);
      Preconditions.checkArgument(
          !name.endsWith(BINARY_HEADER_SUFFIX),
          "ASCII header is named %s.  Only binary headers may end with %s",
          name,
          BINARY_HEADER_SUFFIX);
      this.marshaller = Preconditions.checkNotNull(marshaller, "marshaller");
    }

    @Override
    byte[] toBytes(T value) {
      return marshaller.toAsciiString(value);
    }

    @Override
    T parseBytes(byte[] serialized) {
      return marshaller.parseAsciiString(serialized);
    }
  }

  /**
   * A specialized plain ASCII marshaller. Both input and output are assumed to be valid header
   * ASCII.
   */
  @Immutable
  interface TrustedAsciiMarshaller<T> {
    /**
     * Serialize a metadata value to a ASCII string that contains only the characters listed in the
     * class comment of {@link io.grpc.Metadata.AsciiMarshaller}. Otherwise the output may be
     * considered invalid and discarded by the transport, or the call may fail.
     *
     * @param value to serialize
     * @return serialized version of value, or null if value cannot be transmitted.
     */
    byte[] toAsciiString(T value);

    /**
     * Parse a serialized metadata value from an ASCII string.
     *
     * @param serialized value of metadata to parse
     * @return a parsed instance of type T
     */
    T parseAsciiString(byte[] serialized);
  }
}
