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

package io.grpc.testing.protobuf;

import static io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING;
import static io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING;
import static io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING;
import static io.grpc.MethodDescriptor.MethodType.UNARY;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import io.grpc.MethodDescriptor;
import io.grpc.stub.annotations.RpcMethod;
import java.io.File;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import java.util.Set;
import javax.annotation.processing.AbstractProcessor;
import javax.annotation.processing.RoundEnvironment;
import javax.lang.model.element.Element;
import javax.lang.model.element.TypeElement;
import javax.lang.model.type.MirroredTypeException;
import javax.tools.JavaCompiler;
import javax.tools.JavaCompiler.CompilationTask;
import javax.tools.JavaFileObject;
import javax.tools.StandardJavaFileManager;
import javax.tools.ToolProvider;
import org.junit.Assume;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Test to verify that the proto file simpleservice.proto generates the expected service. */
@RunWith(JUnit4.class)
public class SimpleServiceTest {
  @Test
  public void serviceDescriptor() {
    assertEquals("grpc.testing.SimpleService", SimpleServiceGrpc.getServiceDescriptor().getName());
  }

  @Test
  public void serviceMethodDescriotrs() {
    MethodDescriptor<SimpleRequest, SimpleResponse> genericTypeShouldMatchWhenAssigned;

    genericTypeShouldMatchWhenAssigned = SimpleServiceGrpc.getUnaryRpcMethod();
    assertEquals(UNARY, genericTypeShouldMatchWhenAssigned.getType());

    genericTypeShouldMatchWhenAssigned = SimpleServiceGrpc.getClientStreamingRpcMethod();
    assertEquals(CLIENT_STREAMING, genericTypeShouldMatchWhenAssigned.getType());

    genericTypeShouldMatchWhenAssigned = SimpleServiceGrpc.getServerStreamingRpcMethod();
    assertEquals(SERVER_STREAMING, genericTypeShouldMatchWhenAssigned.getType());

    genericTypeShouldMatchWhenAssigned = SimpleServiceGrpc.getBidiStreamingRpcMethod();
    assertEquals(BIDI_STREAMING, genericTypeShouldMatchWhenAssigned.getType());
  }

  @Test
  public void generatedMethodsAreSampledToLocalTracing() throws Exception {
    assertTrue(SimpleServiceGrpc.getUnaryRpcMethod().isSampledToLocalTracing());
  }

  public static class AnnotationProcessor extends AbstractProcessor {

    private boolean processedClass = false;

    @Override
    public Set<String> getSupportedAnnotationTypes() {
      return Collections.singleton(RpcMethod.class.getCanonicalName());
    }

    private void verifyRpcMethodAnnotation(
        MethodDescriptor<SimpleRequest, SimpleResponse> descriptor, RpcMethod annotation) {
      assertEquals(descriptor.getFullMethodName(), annotation.fullMethodName());
      assertEquals(descriptor.getType(), annotation.methodType());

      // Class objects may not be available at runtime, handle MirroredTypeException if it occurs
      try {
        assertEquals(SimpleRequest.class, annotation.requestType());
      } catch (MirroredTypeException e) {
        assertEquals(SimpleRequest.class.getCanonicalName(), e.getTypeMirror().toString());
      }

      try {
        assertEquals(SimpleResponse.class, annotation.responseType());
      } catch (MirroredTypeException e) {
        assertEquals(SimpleResponse.class.getCanonicalName(), e.getTypeMirror().toString());
      }
    }

    @Override
    public synchronized boolean process(
        Set<? extends TypeElement> annotations, RoundEnvironment roundEnv) {
      for (Element rootElement : roundEnv.getRootElements()) {
        if (!rootElement.asType().toString().equals(SimpleServiceGrpc.class.getCanonicalName())) {
          continue;
        }

        Map<String, RpcMethod> methodToAnnotation = new HashMap<>();
        for (Element enclosedElement : rootElement.getEnclosedElements()) {
          RpcMethod annotation = enclosedElement.getAnnotation(RpcMethod.class);
          if (annotation != null) {
            methodToAnnotation.put(enclosedElement.getSimpleName().toString(), annotation);
          }
        }

        verifyRpcMethodAnnotation(
            SimpleServiceGrpc.getUnaryRpcMethod(), methodToAnnotation.get("getUnaryRpcMethod"));
        verifyRpcMethodAnnotation(
            SimpleServiceGrpc.getServerStreamingRpcMethod(),
            methodToAnnotation.get("getServerStreamingRpcMethod"));
        verifyRpcMethodAnnotation(
            SimpleServiceGrpc.getClientStreamingRpcMethod(),
            methodToAnnotation.get("getClientStreamingRpcMethod"));
        verifyRpcMethodAnnotation(
            SimpleServiceGrpc.getBidiStreamingRpcMethod(),
            methodToAnnotation.get("getBidiStreamingRpcMethod"));

        processedClass = true;
      }
      return false;
    }
  }

  @Test
  public void testRpcMethodAnnotations() throws Exception {
    File grpcJavaFile =
        new File("./src/generated/main/grpc/io/grpc/testing/protobuf/SimpleServiceGrpc.java");
    Assume.assumeTrue(grpcJavaFile.exists());

    JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
    StandardJavaFileManager fileManager = compiler.getStandardFileManager(null, null, null);
    AnnotationProcessor processor = new AnnotationProcessor();
    Iterable<? extends JavaFileObject> obs = fileManager.getJavaFileObjects(grpcJavaFile);

    CompilationTask task =
        compiler.getTask(
            null,
            fileManager,
            null,
            Collections.singleton("-proc:only"),
            Collections.singleton(SimpleServiceGrpc.class.getCanonicalName()),
            obs);
    task.setProcessors(Collections.singleton(processor));
    assertTrue(task.call());
    assertTrue(processor.processedClass);
    fileManager.close();
  }
}
