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

import java.lang.reflect.Constructor;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A {@link LongCounter} that is implemented with a JDK8 {@link LongAdder}. Instantiates the object
 * and invokes methods reflectively to avoid a compile time dependency on LongAdder.
 */
public final class ReflectionLongAdderCounter implements LongCounter {
  private static final Logger logger = Logger.getLogger(ReflectionLongAdderCounter.class.getName());
  private static final Constructor<?> defaultConstructor;
  private static final Method addMethod;
  private static final Method sumMethod;
  private static final RuntimeException initializationException;

  private final Object instance;

  static {
    Class<?> klass = null;
    Constructor<?> defaultConstructorLookup = null;
    Method addMethodLookup = null;
    Method sumMethodLookup = null;
    Throwable caught = null;
    try {
      klass = Class.forName("java.util.concurrent.atomic.LongAdder");

      addMethodLookup = klass.getMethod("add", Long.TYPE);
      sumMethodLookup = klass.getMethod("sum");

      Constructor<?>[] constructors = klass.getConstructors();
      for (Constructor<?> ctor : constructors) {
        if (ctor.getParameterTypes().length == 0) {
          defaultConstructorLookup = ctor;
          break;
        }
      }
    } catch (Throwable e) {
      logger.log(
          Level.FINE,
          "LongAdder can not be found via reflection, this is normal for JDK7 and below",
          e);
      caught = e;
    }

    if (caught == null && defaultConstructorLookup != null) {
      defaultConstructor = defaultConstructorLookup;
      addMethod = addMethodLookup;
      sumMethod = sumMethodLookup;
      initializationException = null;
    } else {
      defaultConstructor = null;
      addMethod = null;
      sumMethod = null;
      initializationException = new RuntimeException(caught);
    }
  }

  ReflectionLongAdderCounter() {
    if (initializationException != null) {
      throw initializationException;
    }
    try {
      instance = defaultConstructor.newInstance();
    } catch (InstantiationException e) {
      throw new RuntimeException(e);
    } catch (IllegalAccessException e) {
      throw new RuntimeException(e);
    } catch (InvocationTargetException e) {
      throw new RuntimeException(e);
    }
  }

  /**
   * Returns true if the environment supports LongAdder. In other words, we are running in >= JDK8.
   */
  static boolean isAvailable() {
    return initializationException == null;
  }

  @Override
  public void add(long delta) {
    try {
      addMethod.invoke(instance, delta);
    } catch (IllegalAccessException e) {
      throw new RuntimeException(e);
    } catch (InvocationTargetException e) {
      throw new RuntimeException(e);
    }
  }

  @Override
  public long value() {
    try {
      return (Long) sumMethod.invoke(instance);
    } catch (IllegalAccessException e) {
      throw new RuntimeException();
    } catch (InvocationTargetException e) {
      throw new RuntimeException();
    }
  }
}
