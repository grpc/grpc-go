/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.netty;

import io.netty.handler.codec.Headers;
import io.netty.handler.codec.http2.Http2Headers;
import java.util.Iterator;
import java.util.List;
import java.util.Map.Entry;
import java.util.Set;

abstract class AbstractHttp2Headers implements Http2Headers {

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

