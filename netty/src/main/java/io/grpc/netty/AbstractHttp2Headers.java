/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.netty;

import io.netty.handler.codec.Headers;
import io.netty.handler.codec.http2.Http2Headers;
import java.util.Iterator;
import java.util.List;
import java.util.Map.Entry;
import java.util.Set;

abstract class AbstractHttp2Headers implements Http2Headers {

  @Override
  public Iterator<CharSequence> valueIterator(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public int size() {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean isEmpty() {
    throw new UnsupportedOperationException();
  }

  @Override
  public Set<CharSequence> names() {
    throw new UnsupportedOperationException();
  }

  @Override
  public CharSequence get(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public CharSequence get(CharSequence name, CharSequence defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public CharSequence getAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public CharSequence getAndRemove(CharSequence name, CharSequence defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public List<CharSequence> getAll(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public List<CharSequence> getAllAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Boolean getBoolean(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean getBoolean(CharSequence name, boolean defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Byte getByte(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public byte getByte(CharSequence name, byte defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Character getChar(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public char getChar(CharSequence name, char defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Short getShort(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public short getShort(CharSequence name, short defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Integer getInt(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public int getInt(CharSequence name, int defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Long getLong(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public long getLong(CharSequence name, long defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Float getFloat(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public float getFloat(CharSequence name, float defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Double getDouble(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public double getDouble(CharSequence name, double defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Long getTimeMillis(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public long getTimeMillis(CharSequence name, long defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Boolean getBooleanAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean getBooleanAndRemove(CharSequence name, boolean defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Byte getByteAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public byte getByteAndRemove(CharSequence name, byte defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Character getCharAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public char getCharAndRemove(CharSequence name, char defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Short getShortAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public short getShortAndRemove(CharSequence name, short defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Integer getIntAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public int getIntAndRemove(CharSequence name, int defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Long getLongAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public long getLongAndRemove(CharSequence name, long defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Float getFloatAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public float getFloatAndRemove(CharSequence name, float defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Double getDoubleAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public double getDoubleAndRemove(CharSequence name, double defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Long getTimeMillisAndRemove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public long getTimeMillisAndRemove(CharSequence name, long defaultValue) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean contains(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean contains(CharSequence name, CharSequence value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean contains(CharSequence name, CharSequence value, boolean caseInsensitive) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsObject(CharSequence name, Object value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsBoolean(CharSequence name, boolean value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsByte(CharSequence name, byte value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsChar(CharSequence name, char value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsShort(CharSequence name, short value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsInt(CharSequence name, int value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsLong(CharSequence name, long value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsFloat(CharSequence name, float value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsDouble(CharSequence name, double value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean containsTimeMillis(CharSequence name, long value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers add(CharSequence name, CharSequence... values) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers add(Headers<? extends CharSequence, ? extends CharSequence, ?> headers) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers add(CharSequence name, CharSequence value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers add(CharSequence name, Iterable<? extends CharSequence> values) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addObject(CharSequence name, Object value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addObject(CharSequence name, Iterable<?> values) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addObject(CharSequence name, Object... values) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addBoolean(CharSequence name, boolean value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addByte(CharSequence name, byte value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addChar(CharSequence name, char value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addShort(CharSequence name, short value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addInt(CharSequence name, int value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addLong(CharSequence name, long value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addFloat(CharSequence name, float value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addDouble(CharSequence name, double value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers addTimeMillis(CharSequence name, long value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers set(Headers<? extends CharSequence, ? extends CharSequence, ?> headers) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers set(CharSequence name, CharSequence value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers set(CharSequence name, Iterable<? extends CharSequence> values) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers set(CharSequence name, CharSequence... values) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setObject(CharSequence name, Object value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setObject(CharSequence name, Iterable<?> values) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setObject(CharSequence name, Object... values) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setBoolean(CharSequence name, boolean value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setByte(CharSequence name, byte value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setChar(CharSequence name, char value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setShort(CharSequence name, short value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setInt(CharSequence name, int value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setLong(CharSequence name, long value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setFloat(CharSequence name, float value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setDouble(CharSequence name, double value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setTimeMillis(CharSequence name, long value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers setAll(Headers<? extends CharSequence, ? extends CharSequence, ?> headers) {
    throw new UnsupportedOperationException();
  }

  @Override
  public boolean remove(CharSequence name) {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers clear() {
    throw new UnsupportedOperationException();
  }

  @Override
  public Iterator<Entry<CharSequence, CharSequence>> iterator() {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers method(CharSequence value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public CharSequence method() {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers scheme(CharSequence value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public CharSequence scheme() {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers authority(CharSequence value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public CharSequence authority() {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers path(CharSequence value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public CharSequence path() {
    throw new UnsupportedOperationException();
  }

  @Override
  public Http2Headers status(CharSequence value) {
    throw new UnsupportedOperationException();
  }

  @Override
  public CharSequence status() {
    throw new UnsupportedOperationException();
  }
}

