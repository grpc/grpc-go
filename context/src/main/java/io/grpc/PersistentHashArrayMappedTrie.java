/*
 * Copyright 2017 The gRPC Authors
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

import java.util.Arrays;

/**
 * A persistent (copy-on-write) hash tree/trie. Collisions are handled
 * linearly. Delete is not supported, but replacement is. The implementation
 * favors simplicity and low memory allocation during insertion. Although the
 * asymptotics are good, it is optimized for small sizes like less than 20;
 * "unbelievably large" would be 100.
 *
 * <p>Inspired by popcnt-based compression seen in Ideal Hash Trees, Phil
 * Bagwell (2000). The rest of the implementation is ignorant of/ignores the
 * paper.
 */
final class PersistentHashArrayMappedTrie<K,V> {
  private final Node<K,V> root;

  PersistentHashArrayMappedTrie() {
    this(null);
  }

  private PersistentHashArrayMappedTrie(Node<K,V> root) {
    this.root = root;
  }

  public int size() {
    if (root == null) {
      return 0;
    }
    return root.size();
  }

  /**
   * Returns the value with the specified key, or {@code null} if it does not exist.
   */
  public V get(K key) {
    if (root == null) {
      return null;
    }
    return root.get(key, key.hashCode(), 0);
  }

  /**
   * Returns a new trie where the key is set to the specified value.
   */
  public PersistentHashArrayMappedTrie<K,V> put(K key, V value) {
    if (root == null) {
      return new PersistentHashArrayMappedTrie<>(new Leaf<>(key, value));
    } else {
      return new PersistentHashArrayMappedTrie<>(root.put(key, value, key.hashCode(), 0));
    }
  }

  // Not actually annotated to avoid depending on guava
  // @VisibleForTesting
  static final class Leaf<K,V> implements Node<K,V> {
    private final K key;
    private final V value;

    public Leaf(K key, V value) {
      this.key = key;
      this.value = value;
    }

    @Override
    public int size() {
      return 1;
    }

    @Override
    public V get(K key, int hash, int bitsConsumed) {
      if (this.key == key) {
        return value;
      } else {
        return null;
      }
    }

    @Override
    public Node<K,V> put(K key, V value, int hash, int bitsConsumed) {
      int thisHash = this.key.hashCode();
      if (thisHash != hash) {
        // Insert
        return CompressedIndex.combine(
            new Leaf<>(key, value), hash, this, thisHash, bitsConsumed);
      } else if (this.key == key) {
        // Replace
        return new Leaf<>(key, value);
      } else {
        // Hash collision
        return new CollisionLeaf<>(this.key, this.value, key, value);
      }
    }

    @Override
    public String toString() {
      return String.format("Leaf(key=%s value=%s)", key, value);
    }
  }

  // Not actually annotated to avoid depending on guava
  // @VisibleForTesting
  static final class CollisionLeaf<K,V> implements Node<K,V> {
    // All keys must have same hash, but not have the same reference
    private final K[] keys;
    private final V[] values;

    // Not actually annotated to avoid depending on guava
    // @VisibleForTesting
    @SuppressWarnings("unchecked")
    CollisionLeaf(K key1, V value1, K key2, V value2) {
      this((K[]) new Object[] {key1, key2}, (V[]) new Object[] {value1, value2});
      assert key1 != key2;
      assert key1.hashCode() == key2.hashCode();
    }

    private CollisionLeaf(K[] keys, V[] values) {
      this.keys = keys;
      this.values = values;
    }

    @Override
    public int size() {
      return values.length;
    }

    @Override
    public V get(K key, int hash, int bitsConsumed) {
      for (int i = 0; i < keys.length; i++) {
        if (keys[i] == key) {
          return values[i];
        }
      }
      return null;
    }

    @Override
    public Node<K,V> put(K key, V value, int hash, int bitsConsumed) {
      int thisHash = keys[0].hashCode();
      int keyIndex;
      if (thisHash != hash) {
        // Insert
        return CompressedIndex.combine(
            new Leaf<>(key, value), hash, this, thisHash, bitsConsumed);
      } else if ((keyIndex = indexOfKey(key)) != -1) {
        // Replace
        K[] newKeys = Arrays.copyOf(keys, keys.length);
        V[] newValues = Arrays.copyOf(values, keys.length);
        newKeys[keyIndex] = key;
        newValues[keyIndex] = value;
        return new CollisionLeaf<>(newKeys, newValues);
      } else {
        // Yet another hash collision
        K[] newKeys = Arrays.copyOf(keys, keys.length + 1);
        V[] newValues = Arrays.copyOf(values, keys.length + 1);
        newKeys[keys.length] = key;
        newValues[keys.length] = value;
        return new CollisionLeaf<>(newKeys, newValues);
      }
    }

    // -1 if not found
    private int indexOfKey(K key) {
      for (int i = 0; i < keys.length; i++) {
        if (keys[i] == key) {
          return i;
        }
      }
      return -1;
    }

    @Override
    public String toString() {
      StringBuilder valuesSb = new StringBuilder();
      valuesSb.append("CollisionLeaf(");
      for (int i = 0; i < values.length; i++) {
        valuesSb.append("(key=").append(keys[i]).append(" value=").append(values[i]).append(") ");
      }
      return valuesSb.append(")").toString();
    }
  }

  // Not actually annotated to avoid depending on guava
  // @VisibleForTesting
  static final class CompressedIndex<K,V> implements Node<K,V> {
    private static final int BITS = 5;
    private static final int BITS_MASK = 0x1F;

    final int bitmap;
    final Node<K,V>[] values;
    private final int size;

    private CompressedIndex(int bitmap, Node<K,V>[] values, int size) {
      this.bitmap = bitmap;
      this.values = values;
      this.size = size;
    }

    @Override
    public int size() {
      return size;
    }

    @Override
    public V get(K key, int hash, int bitsConsumed) {
      int indexBit = indexBit(hash, bitsConsumed);
      if ((bitmap & indexBit) == 0) {
        return null;
      }
      int compressedIndex = compressedIndex(indexBit);
      return values[compressedIndex].get(key, hash, bitsConsumed + BITS);
    }

    @Override
    public Node<K,V> put(K key, V value, int hash, int bitsConsumed) {
      int indexBit = indexBit(hash, bitsConsumed);
      int compressedIndex = compressedIndex(indexBit);
      if ((bitmap & indexBit) == 0) {
        // Insert
        int newBitmap = bitmap | indexBit;
        @SuppressWarnings("unchecked")
        Node<K,V>[] newValues = (Node<K,V>[]) new Node<?,?>[values.length + 1];
        System.arraycopy(values, 0, newValues, 0, compressedIndex);
        newValues[compressedIndex] = new Leaf<>(key, value);
        System.arraycopy(
            values,
            compressedIndex,
            newValues,
            compressedIndex + 1,
            values.length - compressedIndex);
        return new CompressedIndex<>(newBitmap, newValues, size() + 1);
      } else {
        // Replace
        Node<K,V>[] newValues = Arrays.copyOf(values, values.length);
        newValues[compressedIndex] =
            values[compressedIndex].put(key, value, hash, bitsConsumed + BITS);
        int newSize = size();
        newSize += newValues[compressedIndex].size();
        newSize -= values[compressedIndex].size();
        return new CompressedIndex<>(bitmap, newValues, newSize);
      }
    }

    static <K,V> Node<K,V> combine(
        Node<K,V> node1, int hash1, Node<K,V> node2, int hash2, int bitsConsumed) {
      assert hash1 != hash2;
      int indexBit1 = indexBit(hash1, bitsConsumed);
      int indexBit2 = indexBit(hash2, bitsConsumed);
      if (indexBit1 == indexBit2) {
        Node<K,V> node = combine(node1, hash1, node2, hash2, bitsConsumed + BITS);
        @SuppressWarnings("unchecked")
        Node<K,V>[] values = (Node<K,V>[]) new Node<?,?>[] {node};
        return new CompressedIndex<>(indexBit1, values, node.size());
      } else {
        // Make node1 the smallest
        if (uncompressedIndex(hash1, bitsConsumed) > uncompressedIndex(hash2, bitsConsumed)) {
          Node<K,V> nodeCopy = node1;
          node1 = node2;
          node2 = nodeCopy;
        }
        @SuppressWarnings("unchecked")
        Node<K,V>[] values = (Node<K,V>[]) new Node<?,?>[] {node1, node2};
        return new CompressedIndex<>(indexBit1 | indexBit2, values, node1.size() + node2.size());
      }
    }

    @Override
    public String toString() {
      StringBuilder valuesSb = new StringBuilder();
      valuesSb.append("CompressedIndex(")
          .append(String.format("bitmap=%s ", Integer.toBinaryString(bitmap)));
      for (Node<K, V> value : values) {
        valuesSb.append(value).append(" ");
      }
      return valuesSb.append(")").toString();
    }

    private int compressedIndex(int indexBit) {
      return Integer.bitCount(bitmap & (indexBit - 1));
    }

    private static int uncompressedIndex(int hash, int bitsConsumed) {
      return (hash >>> bitsConsumed) & BITS_MASK;
    }

    private static int indexBit(int hash, int bitsConsumed) {
      int uncompressedIndex = uncompressedIndex(hash, bitsConsumed);
      return 1 << uncompressedIndex;
    }
  }

  interface Node<K,V> {
    V get(K key, int hash, int bitsConsumed);

    Node<K,V> put(K key, V value, int hash, int bitsConsumed);

    int size();
  }
}
