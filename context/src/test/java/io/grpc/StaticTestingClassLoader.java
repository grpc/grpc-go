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

import com.google.common.base.Preconditions;
import com.google.common.io.ByteStreams;
import java.io.IOException;
import java.io.InputStream;
import java.util.regex.Pattern;

/**
 * A class loader that can be used to repeatedly trigger static initialization of a class. A new
 * instance is required per test.
 */
public final class StaticTestingClassLoader extends ClassLoader {
  private final Pattern classesToDefine;

  public StaticTestingClassLoader(ClassLoader parent, Pattern classesToDefine) {
    super(parent);
    this.classesToDefine = Preconditions.checkNotNull(classesToDefine, "classesToDefine");
  }

  @Override
  protected Class<?> findClass(String name) throws ClassNotFoundException {
    if (!classesToDefine.matcher(name).matches()) {
      throw new ClassNotFoundException(name);
    }
    InputStream is = getResourceAsStream(name.replace('.', '/') + ".class");
    if (is == null) {
      throw new ClassNotFoundException(name);
    }
    byte[] b;
    try {
      b = ByteStreams.toByteArray(is);
    } catch (IOException ex) {
      throw new ClassNotFoundException(name, ex);
    }
    return defineClass(name, b, 0, b.length);
  }

  @Override
  protected Class<?> loadClass(String name, boolean resolve) throws ClassNotFoundException {
    // Reverse normal loading order; check this class loader before its parent
    synchronized (getClassLoadingLock(name)) {
      Class<?> klass = findLoadedClass(name);
      if (klass == null) {
        try {
          klass = findClass(name);
        } catch (ClassNotFoundException e) {
          // This ClassLoader doesn't know a class with that name; that's part of normal operation
        }
      }
      if (klass == null) {
        klass = super.loadClass(name, false);
      }
      if (resolve) {
        resolveClass(klass);
      }
      return klass;
    }
  }
}
