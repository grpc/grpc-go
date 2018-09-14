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

import static com.google.common.base.Preconditions.checkState;

import com.google.gson.stream.JsonReader;
import com.google.gson.stream.JsonToken;
import java.io.IOException;
import java.io.StringReader;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Parses JSON with as few preconceived notions as possible.
 */
public final class JsonParser {

  private static final Logger logger = Logger.getLogger(JsonParser.class.getName());

  private JsonParser() {}

  /**
   * Parses a json string, returning either a {@code Map<String, Object>}, {@code List<Object>},
   * {@code String}, {@code Double}, {@code Boolean}, or {@code null}.
   */
  @SuppressWarnings("unchecked")
  public static Object parse(String raw) throws IOException {
    JsonReader jr = new JsonReader(new StringReader(raw));
    try {
      return parseRecursive(jr);
    } finally {
      try {
        jr.close();
      } catch (IOException e) {
        logger.log(Level.WARNING, "Failed to close", e);
      }
    }
  }

  private static Object parseRecursive(JsonReader jr) throws IOException {
    checkState(jr.hasNext(), "unexpected end of JSON");
    switch (jr.peek()) {
      case BEGIN_ARRAY:
        return parseJsonArray(jr);
      case BEGIN_OBJECT:
        return parseJsonObject(jr);
      case STRING:
        return jr.nextString();
      case NUMBER:
        return jr.nextDouble();
      case BOOLEAN:
        return jr.nextBoolean();
      case NULL:
        return parseJsonNull(jr);
      default:
        throw new IllegalStateException("Bad token: " + jr.getPath());
    }
  }

  private static Map<String, Object> parseJsonObject(JsonReader jr) throws IOException {
    jr.beginObject();
    Map<String, Object> obj = new LinkedHashMap<String, Object>();
    while (jr.hasNext()) {
      String name = jr.nextName();
      Object value = parseRecursive(jr);
      obj.put(name, value);
    }
    checkState(jr.peek() == JsonToken.END_OBJECT, "Bad token: " + jr.getPath());
    jr.endObject();
    return Collections.unmodifiableMap(obj);
  }

  private static List<Object> parseJsonArray(JsonReader jr) throws IOException {
    jr.beginArray();
    List<Object> array = new ArrayList<>();
    while (jr.hasNext()) {
      Object value = parseRecursive(jr);
      array.add(value);
    }
    checkState(jr.peek() == JsonToken.END_ARRAY, "Bad token: " + jr.getPath());
    jr.endArray();
    return Collections.unmodifiableList(array);
  }

  private static Void parseJsonNull(JsonReader jr) throws IOException {
    jr.nextNull();
    return null;
  }
}
