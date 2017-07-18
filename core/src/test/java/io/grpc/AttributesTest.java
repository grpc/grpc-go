/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link Attributes}. */
@RunWith(JUnit4.class)
public class AttributesTest {
  private static final Attributes.Key<String> YOLO_KEY = Attributes.Key.of("yolo");

  @Test
  public void buildAttributes() {
    Attributes attrs = Attributes.newBuilder().set(YOLO_KEY, "To be, or not to be?").build();
    assertSame("To be, or not to be?", attrs.get(YOLO_KEY));
    assertEquals(1, attrs.keys().size());
  }

  @Test
  public void duplicates() {
    Attributes attrs = Attributes.newBuilder()
        .set(YOLO_KEY, "To be?")
        .set(YOLO_KEY, "Or not to be?")
        .set(Attributes.Key.of("yolo"), "I'm not a duplicate")
        .build();
    assertSame("Or not to be?", attrs.get(YOLO_KEY));
    assertEquals(2, attrs.keys().size());
  }

  @Test
  public void empty() {
    assertEquals(0, Attributes.EMPTY.keys().size());
  }
}
