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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;

import java.util.concurrent.atomic.AtomicLong;
import javax.annotation.Nullable;

/**
 * An internal class. Do not use.
 *
 *<p>An object that has an ID that is unique within the JVM, primarily for debug logging.
 */
@Internal
public final class InternalLogId {

  private static final AtomicLong idAlloc = new AtomicLong();

  /**
   * Creates a log id.
   *
   * @param type the "Type" to be used when logging this id.   The short name of this class will be
   *     used, or else a default if the class is anonymous.
   * @param details a short, human readable string that describes the object the id is attached to.
   *     Typically this will be an address or target.
   */
  public static InternalLogId allocate(Class<?> type, @Nullable String details) {
    return allocate(getClassName(type), details);
  }

  /**
   * Creates a log id.
   *
   * @param typeName the "Type" to be used when logging this id.
   * @param details a short, human readable string that describes the object the id is attached to.
   *     Typically this will be an address or target.
   */
  public static InternalLogId allocate(String typeName, @Nullable String details) {
    return new InternalLogId(typeName, details, getNextId());
  }

  static long getNextId() {
    return idAlloc.incrementAndGet();
  }

  private final String typeName;
  @Nullable
  private final String details;
  private final long id;

  InternalLogId(String typeName, String details, long id) {
    checkNotNull(typeName, "typeName");
    checkArgument(!typeName.isEmpty(), "empty type");
    this.typeName = typeName;
    this.details = details;
    this.id = id;
  }

  public String getTypeName() {
    return typeName;
  }

  @Nullable
  public String getDetails() {
    return details;
  }

  public long getId() {
    return id;
  }

  @Override
  public String toString() {
    StringBuilder sb = new StringBuilder();
    sb.append(shortName());
    if (details != null) {
      sb.append(": (");
      sb.append(details);
      sb.append(')');
    }
    return sb.toString();
  }

  private static String getClassName(Class<?> type) {
    String className = checkNotNull(type, "type").getSimpleName();
    if (!className.isEmpty()) {
      return className;
    }
    // + 1 removes the separating '.'
    return type.getName().substring(type.getPackage().getName().length() + 1);
  }

  public String shortName() {
    return typeName + "<" + id + ">";
  }
}
