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

import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import com.google.common.reflect.ClassPath;
import com.google.common.reflect.ClassPath.ClassInfo;
import com.google.common.truth.Truth;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import org.junit.Assume;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.Parameterized;
import org.junit.runners.Parameterized.Parameter;
import org.junit.runners.Parameterized.Parameters;

/**
 * Tests that Channel and Server builders properly hide the static constructors.
 *
 * <p>This test does nothing on Java 9.
 */
@RunWith(Parameterized.class)
public class ChannelAndServerBuilderTest {

  @Parameter
  public Class<?> builderClass;

  /**
   * Javadoc.
   */
  @Parameters(name = "class={0}")
  public static Collection<Object[]> params() throws Exception {
    ClassLoader loader = ChannelAndServerBuilderTest.class.getClassLoader();
    Collection<ClassInfo> classInfos =
        ClassPath.from(loader).getTopLevelClassesRecursive("io.grpc");
    // Java 9 doesn't expose the URLClassLoader, which breaks searching through the classpath
    if (classInfos.isEmpty()) {
      return new ArrayList<>();
    }
    List<Object[]> classes = new ArrayList<>();
    for (ClassInfo classInfo : classInfos) {
      Class<?> clazz = Class.forName(classInfo.getName(), false /*initialize*/, loader);
      if (ServerBuilder.class.isAssignableFrom(clazz) && clazz != ServerBuilder.class) {
        classes.add(new Object[]{clazz});
      } else if (ManagedChannelBuilder.class.isAssignableFrom(clazz)
          && clazz != ManagedChannelBuilder.class) {
        classes.add(new Object[]{clazz});
      }
    }
    Truth.assertWithMessage("Unable to find any builder classes").that(classes).isNotEmpty();
    return classes;
  }

  @Test
  public void serverBuilderHidesMethod_forPort() throws Exception {
    Assume.assumeTrue(ServerBuilder.class.isAssignableFrom(builderClass));
    Method method = builderClass.getMethod("forPort", int.class);

    assertTrue(Modifier.isStatic(method.getModifiers()));
    assertTrue(ServerBuilder.class.isAssignableFrom(method.getReturnType()));
    assertSame(builderClass, method.getDeclaringClass());
  }

  @Test
  public void channelBuilderHidesMethod_forAddress() throws Exception {
    Assume.assumeTrue(ManagedChannelBuilder.class.isAssignableFrom(builderClass));
    Method method = builderClass.getMethod("forAddress", String.class, int.class);

    assertTrue(Modifier.isStatic(method.getModifiers()));
    assertTrue(ManagedChannelBuilder.class.isAssignableFrom(method.getReturnType()));
    assertSame(builderClass, method.getDeclaringClass());
  }

  @Test
  public void channelBuilderHidesMethod_forTarget() throws Exception {
    Assume.assumeTrue(ManagedChannelBuilder.class.isAssignableFrom(builderClass));
    Method method = builderClass.getMethod("forTarget", String.class);

    assertTrue(Modifier.isStatic(method.getModifiers()));
    assertTrue(ManagedChannelBuilder.class.isAssignableFrom(method.getReturnType()));
    assertSame(builderClass, method.getDeclaringClass());
  }
}
