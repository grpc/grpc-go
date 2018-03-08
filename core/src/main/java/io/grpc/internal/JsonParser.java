/*
 * Copyright 2018, gRPC Authors All rights reserved.
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
final class JsonParser {

  private static final Logger logger = Logger.getLogger(JsonParser.class.getName());

  private JsonParser() {}

  @SuppressWarnings("unchecked")
  static Object parse(String raw) throws IOException {
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
      switch (jr.peek()) {
        case BEGIN_ARRAY:
          obj.put(name, parseJsonArray(jr));
          break;
        case BEGIN_OBJECT:
          obj.put(name, parseJsonObject(jr));
          break;
        case STRING:
          obj.put(name, jr.nextString());
          break;
        case NUMBER:
          obj.put(name, jr.nextDouble());
          break;
        case BOOLEAN:
          obj.put(name, jr.nextBoolean());
          break;
        case NULL:
          obj.put(name, parseJsonNull(jr));
          break;
        default:
          throw new IllegalStateException("Bad token: " + jr.getPath());
      }
    }
    checkState(jr.peek() == JsonToken.END_OBJECT, "Bad token: " + jr.getPath());
    jr.endObject();
    return Collections.unmodifiableMap(obj);
  }

  private static List<Object> parseJsonArray(JsonReader jr) throws IOException {
    jr.beginArray();
    List<Object> array = new ArrayList<Object>();
    while (jr.hasNext()) {
      switch (jr.peek()) {
        case BEGIN_ARRAY:
          array.add(parseJsonArray(jr));
          break;
        case BEGIN_OBJECT:
          array.add(parseJsonObject(jr));
          break;
        case STRING:
          array.add(jr.nextString());
          break;
        case NUMBER:
          array.add(jr.nextDouble());
          break;
        case BOOLEAN:
          array.add(jr.nextBoolean());
          break;
        case NULL:
          array.add(parseJsonNull(jr));
          break;
        default:
          throw new IllegalStateException("Bad token: " + jr.getPath());
      }
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
