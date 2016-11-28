/*
 * Copyright 2016, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.protobuf.service;

import static org.junit.Assert.assertEquals;

import com.google.protobuf.ByteString;

import io.grpc.ServerServiceDefinition;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.ServerImpl;
import io.grpc.reflection.testing.DynamicServiceGrpc;
import io.grpc.reflection.testing.ReflectableServiceGrpc;
import io.grpc.reflection.testing.ReflectionTestDepthThreeProto;
import io.grpc.reflection.testing.ReflectionTestDepthTwoAlternateProto;
import io.grpc.reflection.testing.ReflectionTestDepthTwoProto;
import io.grpc.reflection.testing.ReflectionTestProto;
import io.grpc.reflection.v1alpha.ExtensionRequest;
import io.grpc.reflection.v1alpha.FileDescriptorResponse;
import io.grpc.reflection.v1alpha.ServerReflectionRequest;
import io.grpc.reflection.v1alpha.ServerReflectionResponse;
import io.grpc.reflection.v1alpha.ServiceResponse;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.StreamRecorder;
import io.grpc.util.MutableHandlerRegistry;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.IOException;
import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * Tests for {@link ProtoReflectionService}.
 */
@RunWith(JUnit4.class)
public class ProtoReflectionServiceTest {

  private static final String TEST_HOST = "localhost";

  private MutableHandlerRegistry handlerRegistry = new MutableHandlerRegistry();

  private ProtoReflectionService reflectionService;

  private ServerImpl server;

  @Before
  public void setUp() throws IOException {
    reflectionService = new ProtoReflectionService();
    server = InProcessServerBuilder.forName("proto-reflection-test")
        .addService(reflectionService)
        .addService(new ReflectableServiceGrpc.ReflectableServiceImplBase() {})
        .fallbackHandlerRegistry(handlerRegistry)
        .build();
  }

  @Test
  public void listServices() throws Exception {
    Set<ServiceResponse> originalServices = new HashSet<ServiceResponse>(
        Arrays.asList(
            ServiceResponse.newBuilder()
                .setName("grpc.reflection.v1alpha.ServerReflection")
                .build(),
            ServiceResponse.newBuilder()
                .setName("grpc.reflection.testing.ReflectableService")
                .build())
    );
    assertServiceResponseEquals(originalServices);

    ServerServiceDefinition dynamicService =
        (new DynamicServiceGrpc.DynamicServiceImplBase() {}).bindService();
    handlerRegistry.addService(dynamicService);
    assertServiceResponseEquals(new HashSet<ServiceResponse>(
        Arrays.asList(
            ServiceResponse.newBuilder()
                .setName("grpc.reflection.v1alpha.ServerReflection")
                .build(),
            ServiceResponse.newBuilder()
                .setName("grpc.reflection.testing.ReflectableService")
                .build(),
            ServiceResponse.newBuilder()
                .setName("grpc.reflection.testing.DynamicService")
                .build())
    ));

    handlerRegistry.removeService(dynamicService);
    assertServiceResponseEquals(originalServices);
  }

  @Test
  public void fileByFilename() throws Exception {
    ServerReflectionRequest request =
        ServerReflectionRequest.newBuilder()
            .setHost(TEST_HOST)
            .setFileByFilename("io/grpc/reflection/testing/reflection_test_depth_three.proto")
            .build();

    ServerReflectionResponse goldenResponse =
        ServerReflectionResponse.newBuilder()
            .setValidHost(TEST_HOST)
            .setOriginalRequest(request)
            .setFileDescriptorResponse(
                FileDescriptorResponse.newBuilder()
                    .addFileDescriptorProto(
                        ReflectionTestDepthThreeProto.getDescriptor().toProto().toByteString())
                    .build())
            .build();

    StreamRecorder<ServerReflectionResponse> responseObserver = StreamRecorder.create();
    StreamObserver<ServerReflectionRequest> requestObserver =
        reflectionService.serverReflectionInfo(responseObserver);
    requestObserver.onNext(request);
    requestObserver.onCompleted();
    assertEquals(goldenResponse, responseObserver.firstValue().get());
  }

  @Test
  public void fileContainingSymbol() throws Exception {
    ServerReflectionRequest request =
        ServerReflectionRequest.newBuilder()
            .setHost(TEST_HOST)
            .setFileContainingSymbol("grpc.reflection.testing.ReflectableService.Method")
            .build();

    List<ByteString> goldenResponse =
        Arrays.asList(
            ReflectionTestProto.getDescriptor().toProto().toByteString(),
            ReflectionTestDepthTwoProto.getDescriptor().toProto().toByteString(),
            ReflectionTestDepthTwoAlternateProto.getDescriptor().toProto().toByteString(),
            ReflectionTestDepthThreeProto.getDescriptor().toProto().toByteString()
        );

    StreamRecorder<ServerReflectionResponse> responseObserver = StreamRecorder.create();
    StreamObserver<ServerReflectionRequest> requestObserver =
        reflectionService.serverReflectionInfo(responseObserver);
    requestObserver.onNext(request);
    requestObserver.onCompleted();

    List<ByteString> response =
        responseObserver.firstValue().get()
            .getFileDescriptorResponse().getFileDescriptorProtoList();
    assertEquals(goldenResponse.size(), response.size());
    assertEquals(new HashSet<ByteString>(goldenResponse), new HashSet<ByteString>(response));
  }

  @Test
  public void fileContainingNestedSymbol() throws Exception {
    ServerReflectionRequest request =
        ServerReflectionRequest.newBuilder()
            .setHost(TEST_HOST)
            .setFileContainingSymbol("grpc.reflection.testing.NestedTypeOuter.Middle.Inner")
            .build();

    ServerReflectionResponse goldenResponse =
        ServerReflectionResponse.newBuilder()
            .setValidHost(TEST_HOST)
            .setOriginalRequest(request)
            .setFileDescriptorResponse(
                FileDescriptorResponse.newBuilder()
                    .addFileDescriptorProto(
                        ReflectionTestDepthThreeProto.getDescriptor().toProto().toByteString())
                    .build())
            .build();

    StreamRecorder<ServerReflectionResponse> responseObserver = StreamRecorder.create();
    StreamObserver<ServerReflectionRequest> requestObserver =
        reflectionService.serverReflectionInfo(responseObserver);
    requestObserver.onNext(request);
    requestObserver.onCompleted();
    assertEquals(goldenResponse, responseObserver.firstValue().get());
  }

  @Test
  public void fileContainingExtension() throws Exception {
    ServerReflectionRequest request =
        ServerReflectionRequest.newBuilder()
            .setHost(TEST_HOST)
            .setFileContainingExtension(
                ExtensionRequest.newBuilder()
                    .setContainingType("grpc.reflection.testing.ThirdLevelType")
                    .setExtensionNumber(100)
                    .build())
            .build();

    List<ByteString> goldenResponse =
        Arrays.asList(
            ReflectionTestProto.getDescriptor().toProto().toByteString(),
            ReflectionTestDepthTwoProto.getDescriptor().toProto().toByteString(),
            ReflectionTestDepthTwoAlternateProto.getDescriptor().toProto().toByteString(),
            ReflectionTestDepthThreeProto.getDescriptor().toProto().toByteString()
        );

    StreamRecorder<ServerReflectionResponse> responseObserver = StreamRecorder.create();
    StreamObserver<ServerReflectionRequest> requestObserver =
        reflectionService.serverReflectionInfo(responseObserver);
    requestObserver.onNext(request);
    requestObserver.onCompleted();

    List<ByteString> response =
        responseObserver.firstValue().get()
            .getFileDescriptorResponse().getFileDescriptorProtoList();
    assertEquals(goldenResponse.size(), response.size());
    assertEquals(new HashSet<ByteString>(goldenResponse), new HashSet<ByteString>(response));
  }

  @Test
  public void fileContainingNestedExtension() throws Exception {
    ServerReflectionRequest request =
        ServerReflectionRequest.newBuilder()
            .setHost(TEST_HOST)
            .setFileContainingExtension(
                ExtensionRequest.newBuilder()
                    .setContainingType("grpc.reflection.testing.ThirdLevelType")
                    .setExtensionNumber(101)
                    .build())
            .build();

    ServerReflectionResponse goldenResponse =
        ServerReflectionResponse.newBuilder()
            .setValidHost(TEST_HOST)
            .setOriginalRequest(request)
            .setFileDescriptorResponse(
                FileDescriptorResponse.newBuilder()
                    .addFileDescriptorProto(
                        ReflectionTestDepthTwoProto.getDescriptor().toProto().toByteString())
                    .addFileDescriptorProto(
                        ReflectionTestDepthThreeProto.getDescriptor().toProto().toByteString())
                    .build())
            .build();

    StreamRecorder<ServerReflectionResponse> responseObserver = StreamRecorder.create();
    StreamObserver<ServerReflectionRequest> requestObserver =
        reflectionService.serverReflectionInfo(responseObserver);
    requestObserver.onNext(request);
    requestObserver.onCompleted();
    assertEquals(goldenResponse, responseObserver.firstValue().get());
  }

  @Test
  public void allExtensionNumbersOfType() throws Exception {
    ServerReflectionRequest request =
        ServerReflectionRequest.newBuilder()
            .setHost(TEST_HOST)
            .setAllExtensionNumbersOfType("grpc.reflection.testing.ThirdLevelType")
            .build();

    Set<Integer> goldenResponse = new HashSet<Integer>(Arrays.asList(100, 101));

    StreamRecorder<ServerReflectionResponse> responseObserver = StreamRecorder.create();
    StreamObserver<ServerReflectionRequest> requestObserver =
        reflectionService.serverReflectionInfo(responseObserver);
    requestObserver.onNext(request);
    requestObserver.onCompleted();
    Set<Integer> extensionNumberResponseSet =
        new HashSet<Integer>(
            responseObserver
                .firstValue()
                .get()
                .getAllExtensionNumbersResponse()
                .getExtensionNumberList());
    assertEquals(goldenResponse, extensionNumberResponseSet);
  }


  private void assertServiceResponseEquals(Set<ServiceResponse> goldenResponse) throws Exception {
    ServerReflectionRequest request =
        ServerReflectionRequest.newBuilder().setHost(TEST_HOST).setListServices("services").build();
    StreamRecorder<ServerReflectionResponse> responseObserver = StreamRecorder.create();
    StreamObserver<ServerReflectionRequest> requestObserver =
        reflectionService.serverReflectionInfo(responseObserver);
    requestObserver.onNext(request);
    requestObserver.onCompleted();
    List<ServiceResponse> response =
        responseObserver.firstValue().get().getListServicesResponse().getServiceList();
    assertEquals(goldenResponse.size(), response.size());
    assertEquals(goldenResponse, new HashSet<ServiceResponse>(response));
  }
}
