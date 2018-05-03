/*
 * Copyright 2015 The gRPC Authors
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

import static com.google.common.base.Charsets.UTF_8;

import com.google.common.io.ByteStreams;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;

/** Marshalls UTF-8 encoded strings. */
public class StringMarshaller implements MethodDescriptor.Marshaller<String> {
  public static StringMarshaller INSTANCE = new StringMarshaller();

  @Override
  public InputStream stream(String value) {
    return new ByteArrayInputStream(value.getBytes(UTF_8));
  }

  @Override
  public String parse(InputStream stream) {
    try {
      return new String(ByteStreams.toByteArray(stream), UTF_8);
    } catch (IOException ex) {
      throw new RuntimeException(ex);
    }
  }
}
