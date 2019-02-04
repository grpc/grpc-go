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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;

import com.google.gson.stream.MalformedJsonException;
import java.io.EOFException;
import java.io.IOException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link JsonParser}.
 */
@RunWith(JUnit4.class)
public class JsonParserTest {

  @Rule
  public final ExpectedException thrown = ExpectedException.none();

  @Test
  public void emptyObject() throws IOException {
    assertEquals(new LinkedHashMap<String, Object>(), JsonParser.parse("{}"));
  }

  @Test
  public void emptyArray() throws IOException {
    assertEquals(new ArrayList<>(), JsonParser.parse("[]"));
  }

  @Test
  public void intNumber() throws IOException {
    assertEquals(Double.valueOf("1"), JsonParser.parse("1"));
  }

  @Test
  public void doubleNumber() throws IOException {
    assertEquals(Double.valueOf("1.2"), JsonParser.parse("1.2"));
  }

  @Test
  public void longNumber() throws IOException {
    assertEquals(Double.valueOf("9999999999"), JsonParser.parse("9999999999"));
  }

  @Test
  public void booleanValue() throws IOException {
    assertEquals(true, JsonParser.parse("true"));
  }

  @Test
  public void nullValue() throws IOException {
    assertEquals(null, JsonParser.parse("null"));
  }

  @Test
  public void nanFails() throws IOException {
    thrown.expect(MalformedJsonException.class);

    JsonParser.parse("NaN");
  }

  @Test
  public void objectEarlyEnd() throws IOException {
    thrown.expect(MalformedJsonException.class);

    JsonParser.parse("{foo:}");
  }

  @Test
  public void earlyEndArray() throws IOException {
    thrown.expect(EOFException.class);

    JsonParser.parse("[1, 2, ");
  }

  @Test
  public void arrayMissingElement() throws IOException {
    thrown.expect(MalformedJsonException.class);

    JsonParser.parse("[1, 2, ]");
  }

  @Test
  public void objectMissingElement() throws IOException {
    thrown.expect(MalformedJsonException.class);

    JsonParser.parse("{1: ");
  }

  @Test
  public void objectNoName() throws IOException {
    thrown.expect(MalformedJsonException.class);

    JsonParser.parse("{: 1");
  }

  @Test
  public void objectStringName() throws IOException {
    LinkedHashMap<String, Object> expected = new LinkedHashMap<>();
    expected.put("hi", Double.valueOf("2"));

    assertEquals(expected, JsonParser.parse("{\"hi\": 2}"));
  }
}