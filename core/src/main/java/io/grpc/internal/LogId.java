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

package io.grpc.internal;

import java.util.concurrent.atomic.AtomicLong;

/**
 * An object that has an ID that is unique within the JVM, primarily for debug logging.
 */
public final class LogId {
  private static final AtomicLong idAlloc = new AtomicLong();

  /**
   * @param tag a loggable tag associated with this tag. The ID that is allocated is guaranteed
   *            to be unique and increasing, irrespective of the tag.
   */
  public static LogId allocate(String tag) {
    return new LogId(tag, getNextId());
  }

  static long getNextId() {
    return idAlloc.incrementAndGet();
  }

  private final String tag;
  private final long id;

  protected LogId(String tag, long id) {
    this.tag = tag;
    this.id = id;
  }

  public long getId() {
    return id;
  }

  public String getTag() {
    return tag;
  }

  @Override
  public String toString() {
    return tag + "-" + id;
  }
}
