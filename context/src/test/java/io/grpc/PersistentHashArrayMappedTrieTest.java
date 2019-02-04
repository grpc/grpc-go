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

import static junit.framework.TestCase.assertTrue;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;

import io.grpc.PersistentHashArrayMappedTrie.CollisionLeaf;
import io.grpc.PersistentHashArrayMappedTrie.CompressedIndex;
import io.grpc.PersistentHashArrayMappedTrie.Leaf;
import io.grpc.PersistentHashArrayMappedTrie.Node;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class PersistentHashArrayMappedTrieTest {
  @Test
  public void leaf_replace() {
    Key key = new Key(0);
    Object value1 = new Object();
    Object value2 = new Object();
    Leaf<Key, Object> leaf = new Leaf<>(key, value1);
    Node<Key, Object> ret = leaf.put(key, value2, key.hashCode(), 0);
    assertTrue(ret instanceof Leaf);
    assertSame(value2, ret.get(key, key.hashCode(), 0));

    assertSame(value1, leaf.get(key, key.hashCode(), 0));

    assertEquals(1, leaf.size());
    assertEquals(1, ret.size());
  }

  @Test
  public void leaf_collision() {
    Key key1 = new Key(0);
    Key key2 = new Key(0);
    Object value1 = new Object();
    Object value2 = new Object();
    Leaf<Key, Object> leaf = new Leaf<>(key1, value1);
    Node<Key, Object> ret = leaf.put(key2, value2, key2.hashCode(), 0);
    assertTrue(ret instanceof CollisionLeaf);
    assertSame(value1, ret.get(key1, key1.hashCode(), 0));
    assertSame(value2, ret.get(key2, key2.hashCode(), 0));

    assertSame(value1, leaf.get(key1, key1.hashCode(), 0));
    assertSame(null, leaf.get(key2, key2.hashCode(), 0));

    assertEquals(1, leaf.size());
    assertEquals(2, ret.size());
  }

  @Test
  public void leaf_insert() {
    Key key1 = new Key(0);
    Key key2 = new Key(1);
    Object value1 = new Object();
    Object value2 = new Object();
    Leaf<Key, Object> leaf = new Leaf<>(key1, value1);
    Node<Key, Object> ret = leaf.put(key2, value2, key2.hashCode(), 0);
    assertTrue(ret instanceof CompressedIndex);
    assertSame(value1, ret.get(key1, key1.hashCode(), 0));
    assertSame(value2, ret.get(key2, key2.hashCode(), 0));

    assertSame(value1, leaf.get(key1, key1.hashCode(), 0));
    assertSame(null, leaf.get(key2, key2.hashCode(), 0));

    assertEquals(1, leaf.size());
    assertEquals(2, ret.size());
  }

  @Test(expected = AssertionError.class)
  public void collisionLeaf_assertKeysDifferent() {
    Key key1 = new Key(0);
    new CollisionLeaf<>(key1, new Object(), key1, new Object());
  }

  @Test(expected = AssertionError.class)
  public void collisionLeaf_assertHashesSame() {
    new CollisionLeaf<>(new Key(0), new Object(), new Key(1), new Object());
  }

  @Test
  public void collisionLeaf_insert() {
    Key key1 = new Key(0);
    Key key2 = new Key(key1.hashCode());
    Key insertKey = new Key(1);
    Object value1 = new Object();
    Object value2 = new Object();
    Object insertValue = new Object();
    CollisionLeaf<Key, Object> leaf =
        new CollisionLeaf<>(key1, value1, key2, value2);

    Node<Key, Object> ret = leaf.put(insertKey, insertValue, insertKey.hashCode(), 0);
    assertTrue(ret instanceof CompressedIndex);
    assertSame(value1, ret.get(key1, key1.hashCode(), 0));
    assertSame(value2, ret.get(key2, key2.hashCode(), 0));
    assertSame(insertValue, ret.get(insertKey, insertKey.hashCode(), 0));

    assertSame(value1, leaf.get(key1, key1.hashCode(), 0));
    assertSame(value2, leaf.get(key2, key2.hashCode(), 0));
    assertSame(null, leaf.get(insertKey, insertKey.hashCode(), 0));

    assertEquals(2, leaf.size());
    assertEquals(3, ret.size());
  }

  @Test
  public void collisionLeaf_replace() {
    Key replaceKey = new Key(0);
    Object originalValue = new Object();
    Key key = new Key(replaceKey.hashCode());
    Object value = new Object();
    CollisionLeaf<Key, Object> leaf =
        new CollisionLeaf<>(replaceKey, originalValue, key, value);
    Object replaceValue = new Object();
    Node<Key, Object> ret = leaf.put(replaceKey, replaceValue, replaceKey.hashCode(), 0);
    assertTrue(ret instanceof CollisionLeaf);
    assertSame(replaceValue, ret.get(replaceKey, replaceKey.hashCode(), 0));
    assertSame(value, ret.get(key, key.hashCode(), 0));

    assertSame(value, leaf.get(key, key.hashCode(), 0));
    assertSame(originalValue, leaf.get(replaceKey, replaceKey.hashCode(), 0));

    assertEquals(2, leaf.size());
    assertEquals(2, ret.size());
  }

  @Test
  public void collisionLeaf_collision() {
    Key key1 = new Key(0);
    Key key2 = new Key(key1.hashCode());
    Key key3 = new Key(key1.hashCode());
    Object value1 = new Object();
    Object value2 = new Object();
    Object value3 = new Object();
    CollisionLeaf<Key, Object> leaf =
        new CollisionLeaf<>(key1, value1, key2, value2);

    Node<Key, Object> ret = leaf.put(key3, value3, key3.hashCode(), 0);
    assertTrue(ret instanceof CollisionLeaf);
    assertSame(value1, ret.get(key1, key1.hashCode(), 0));
    assertSame(value2, ret.get(key2, key2.hashCode(), 0));
    assertSame(value3, ret.get(key3, key3.hashCode(), 0));

    assertSame(value1, leaf.get(key1, key1.hashCode(), 0));
    assertSame(value2, leaf.get(key2, key2.hashCode(), 0));
    assertSame(null, leaf.get(key3, key3.hashCode(), 0));

    assertEquals(2, leaf.size());
    assertEquals(3, ret.size());
  }

  @Test
  public void compressedIndex_combine_differentIndexBit() {
    final Key key1 = new Key(7);
    final Key key2 = new Key(19);
    final Object value1 = new Object();
    final Object value2 = new Object();
    Leaf<Key, Object> leaf1 = new Leaf<>(key1, value1);
    Leaf<Key, Object> leaf2 = new Leaf<>(key2, value2);
    class Verifier {
      private void verify(Node<Key, Object> ret) {
        CompressedIndex<Key, Object> collisionLeaf = (CompressedIndex<Key, Object>) ret;
        assertEquals((1 << 7) | (1 << 19), collisionLeaf.bitmap);
        assertEquals(2, collisionLeaf.values.length);
        assertSame(value1, collisionLeaf.values[0].get(key1, key1.hashCode(), 0));
        assertSame(value2, collisionLeaf.values[1].get(key2, key2.hashCode(), 0));

        assertSame(value1, ret.get(key1, key1.hashCode(), 0));
        assertSame(value2, ret.get(key2, key2.hashCode(), 0));

        assertEquals(2, ret.size());
      }
    }

    Verifier verifier = new Verifier();
    verifier.verify(CompressedIndex.combine(leaf1, key1.hashCode(), leaf2, key2.hashCode(), 0));
    verifier.verify(CompressedIndex.combine(leaf2, key2.hashCode(), leaf1, key1.hashCode(), 0));

    assertEquals(1, leaf1.size());
    assertEquals(1, leaf2.size());
  }

  @Test
  public void compressedIndex_combine_sameIndexBit() {
    final Key key1 = new Key(17 << 5 | 1); // 5 bit regions: (17, 1)
    final Key key2 = new Key(31 << 5 | 1); // 5 bit regions: (31, 1)
    final Object value1 = new Object();
    final Object value2 = new Object();
    Leaf<Key, Object> leaf1 = new Leaf<>(key1, value1);
    Leaf<Key, Object> leaf2 = new Leaf<>(key2, value2);
    class Verifier {
      private void verify(Node<Key, Object> ret) {
        CompressedIndex<Key, Object> collisionInternal = (CompressedIndex<Key, Object>) ret;
        assertEquals(1 << 1, collisionInternal.bitmap);
        assertEquals(1, collisionInternal.values.length);
        CompressedIndex<Key, Object> collisionLeaf =
            (CompressedIndex<Key, Object>) collisionInternal.values[0];
        assertEquals((1 << 31) | (1 << 17), collisionLeaf.bitmap);
        assertSame(value1, ret.get(key1, key1.hashCode(), 0));
        assertSame(value2, ret.get(key2, key2.hashCode(), 0));

        assertEquals(2, ret.size());
      }
    }

    Verifier verifier = new Verifier();
    verifier.verify(CompressedIndex.combine(leaf1, key1.hashCode(), leaf2, key2.hashCode, 0));
    verifier.verify(CompressedIndex.combine(leaf2, key2.hashCode(), leaf1, key1.hashCode, 0));

    assertEquals(1, leaf1.size());
    assertEquals(1, leaf2.size());
  }

  /**
   * A key with a settable hashcode.
   */
  static final class Key {
    private final int hashCode;

    Key(int hashCode) {
      this.hashCode = hashCode;
    }

    @Override
    public int hashCode() {
      return hashCode;
    }

    @Override
    public String toString() {
      return String.format("Key(hashCode=%x)", hashCode);
    }
  }
}
