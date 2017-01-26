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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.protobuf.Descriptors.Descriptor;
import com.google.protobuf.Descriptors.FieldDescriptor;
import com.google.protobuf.Descriptors.FileDescriptor;
import com.google.protobuf.Descriptors.MethodDescriptor;
import com.google.protobuf.Descriptors.ServiceDescriptor;
import io.grpc.BindableService;
import io.grpc.ExperimentalApi;
import io.grpc.InternalNotifyOnServerBuild;
import io.grpc.Server;
import io.grpc.ServerServiceDefinition;
import io.grpc.Status;
import io.grpc.protobuf.ProtoFileDescriptorSupplier;
import io.grpc.reflection.v1alpha.ErrorResponse;
import io.grpc.reflection.v1alpha.ExtensionNumberResponse;
import io.grpc.reflection.v1alpha.ExtensionRequest;
import io.grpc.reflection.v1alpha.FileDescriptorResponse;
import io.grpc.reflection.v1alpha.ListServiceResponse;
import io.grpc.reflection.v1alpha.ServerReflectionGrpc;
import io.grpc.reflection.v1alpha.ServerReflectionRequest;
import io.grpc.reflection.v1alpha.ServerReflectionResponse;
import io.grpc.reflection.v1alpha.ServiceResponse;
import io.grpc.stub.ServerCallStreamObserver;
import io.grpc.stub.StreamObserver;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedList;
import java.util.List;
import java.util.Map;
import java.util.Queue;
import java.util.Set;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

/**
 * Provides a reflection service for Protobuf services (including the reflection service itself).
 *
 * <p>Separately tracks mutable and immutable services. Throws an exception if either group of
 * services contains multiple Protobuf files with declarations of the same service, method, type, or
 * extension.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2222")
public final class ProtoReflectionService extends ServerReflectionGrpc.ServerReflectionImplBase
    implements InternalNotifyOnServerBuild {

  private volatile ServerReflectionIndex serverReflectionIndex;

  private ProtoReflectionService() {}

  public static BindableService getInstance() {
    return new ProtoReflectionService();
  }

  /**
   * Receives a reference to the server at build time.
   */
  @Override
  public void notifyOnBuild(Server server) {
    checkState(serverReflectionIndex == null);
    serverReflectionIndex = new ServerReflectionIndex(checkNotNull(server, "server"));
  }

  @Override
  public StreamObserver<ServerReflectionRequest> serverReflectionInfo(
      final StreamObserver<ServerReflectionResponse> responseObserver) {

    checkState(serverReflectionIndex != null);
    serverReflectionIndex.initializeImmutableServicesIndex();

    final ServerCallStreamObserver<ServerReflectionResponse> serverCallStreamObserver =
        (ServerCallStreamObserver<ServerReflectionResponse>) responseObserver;
    ProtoReflectionStreamObserver requestObserver =
        new ProtoReflectionStreamObserver(serverCallStreamObserver);
    serverCallStreamObserver.setOnReadyHandler(requestObserver);
    serverCallStreamObserver.disableAutoInboundFlowControl();
    serverCallStreamObserver.request(1);
    return requestObserver;
  }

  private class ProtoReflectionStreamObserver implements Runnable,
      StreamObserver<ServerReflectionRequest> {
    private final ServerCallStreamObserver<ServerReflectionResponse> serverCallStreamObserver;

    private boolean closeAfterSend = false;
    private ServerReflectionRequest request;

    ProtoReflectionStreamObserver(
        ServerCallStreamObserver<ServerReflectionResponse> serverCallStreamObserver) {
      this.serverCallStreamObserver = checkNotNull(serverCallStreamObserver, "observer");
    }

    @Override
    public void run() {
      if (request != null) {
        handleReflectionRequest();
      }
    }

    @Override
    public void onNext(ServerReflectionRequest request) {
      checkState(this.request == null);
      this.request = checkNotNull(request);
      handleReflectionRequest();
    }

    private void handleReflectionRequest() {
      if (serverCallStreamObserver.isReady()) {
        serverReflectionIndex.updateMutableIndexIfNecessary();

        switch (request.getMessageRequestCase()) {
          case FILE_BY_FILENAME:
            getFileByName(request);
            break;
          case FILE_CONTAINING_SYMBOL:
            getFileContainingSymbol(request);
            break;
          case FILE_CONTAINING_EXTENSION:
            getFileByExtension(request);
            break;
          case ALL_EXTENSION_NUMBERS_OF_TYPE:
            getAllExtensions(request);
            break;
          case LIST_SERVICES:
            listServices(request);
            break;
          default:
            sendErrorResponse(request, Status.UNIMPLEMENTED, "");
        }
        request = null;
        if (closeAfterSend) {
          serverCallStreamObserver.onCompleted();
        } else {
          serverCallStreamObserver.request(1);
        }
      }
    }

    @Override
    public void onCompleted() {
      if (request != null) {
        closeAfterSend = true;
      } else {
        serverCallStreamObserver.onCompleted();
      }
    }

    @Override
    public void onError(Throwable cause) {
      serverCallStreamObserver.onError(cause);
    }

    private void getFileByName(ServerReflectionRequest request) {
      String name = request.getFileByFilename();
      FileDescriptor fd = serverReflectionIndex.getFileDescriptorByName(name);
      if (fd != null) {
        serverCallStreamObserver.onNext(createServerReflectionResponse(request, fd));
      } else {
        sendErrorResponse(request, Status.NOT_FOUND, "File not found.");
      }
    }

    private void getFileContainingSymbol(ServerReflectionRequest request) {
      String symbol = request.getFileContainingSymbol();
      FileDescriptor fd = serverReflectionIndex.getFileDescriptorBySymbol(symbol);
      if (fd != null) {
        serverCallStreamObserver.onNext(createServerReflectionResponse(request, fd));
      } else {
        sendErrorResponse(request, Status.NOT_FOUND, "Symbol not found.");
      }
    }

    private void getFileByExtension(ServerReflectionRequest request) {
      ExtensionRequest extensionRequest = request.getFileContainingExtension();
      String type = extensionRequest.getContainingType();
      int extension = extensionRequest.getExtensionNumber();
      FileDescriptor fd =
          serverReflectionIndex.getFileDescriptorByExtensionAndNumber(type, extension);
      if (fd != null) {
        serverCallStreamObserver.onNext(createServerReflectionResponse(request, fd));
      } else {
        sendErrorResponse(request, Status.NOT_FOUND, "Extension not found.");
      }
    }

    private void getAllExtensions(ServerReflectionRequest request) {
      String type = request.getAllExtensionNumbersOfType();
      Set<Integer> extensions = serverReflectionIndex.getExtensionNumbersOfType(type);
      if (extensions != null) {
        ExtensionNumberResponse.Builder builder =
            ExtensionNumberResponse.newBuilder()
                .setBaseTypeName(type)
                .addAllExtensionNumber(extensions);
        serverCallStreamObserver.onNext(
            ServerReflectionResponse.newBuilder()
                .setValidHost(request.getHost())
                .setOriginalRequest(request)
                .setAllExtensionNumbersResponse(builder)
                .build());
      } else {
        sendErrorResponse(request, Status.NOT_FOUND, "Type not found.");
      }
    }

    private void listServices(ServerReflectionRequest request) {
      ListServiceResponse.Builder builder = ListServiceResponse.newBuilder();
      for (String serviceName : serverReflectionIndex.getServiceNames()) {
        builder.addService(ServiceResponse.newBuilder().setName(serviceName));
      }
      serverCallStreamObserver.onNext(
          ServerReflectionResponse.newBuilder()
              .setValidHost(request.getHost())
              .setOriginalRequest(request)
              .setListServicesResponse(builder)
              .build());
    }

    private void sendErrorResponse(
        ServerReflectionRequest request, Status status, String message) {
      ServerReflectionResponse response =
          ServerReflectionResponse.newBuilder()
              .setValidHost(request.getHost())
              .setOriginalRequest(request)
              .setErrorResponse(
                  ErrorResponse.newBuilder()
                      .setErrorCode(status.getCode().value())
                      .setErrorMessage(message))
              .build();
      serverCallStreamObserver.onNext(response);
    }

    private ServerReflectionResponse createServerReflectionResponse(
        ServerReflectionRequest request, FileDescriptor fd) {
      FileDescriptorResponse.Builder fdRBuilder = FileDescriptorResponse.newBuilder();

      Set<String> seenFiles = new HashSet<String>();
      Queue<FileDescriptor> frontier = new LinkedList<FileDescriptor>();
      seenFiles.add(fd.getName());
      frontier.add(fd);
      while (!frontier.isEmpty()) {
        FileDescriptor nextFd = frontier.remove();
        fdRBuilder.addFileDescriptorProto(nextFd.toProto().toByteString());
        for (FileDescriptor dependencyFd : nextFd.getDependencies()) {
          if (!seenFiles.contains(dependencyFd.getName())) {
            seenFiles.add(dependencyFd.getName());
            frontier.add(dependencyFd);
          }
        }
      }
      return ServerReflectionResponse.newBuilder()
          .setValidHost(request.getHost())
          .setOriginalRequest(request)
          .setFileDescriptorResponse(fdRBuilder)
          .build();
    }
  }

  /**
   * Indexes the server's services and allows lookups of file descriptors by filename, symbol, type,
   * and extension number.
   *
   * <p>Internally, this stores separate indices for the immutable and mutable services. When
   * queried, the immutable service index is checked for a matching value. Only if there is no match
   * in the immutable service index are the mutable services checked.
   */
  private static final class ServerReflectionIndex {
    private FileDescriptorIndex immutableServicesIndex;
    private final Object lock = new Object();
    /**
     * Tracks mutable services. Accesses must be synchronized.
     */
    @GuardedBy("lock") private FileDescriptorIndex mutableServicesIndex
        = new FileDescriptorIndex(Collections.<ServerServiceDefinition>emptyList());

    private final Server server;

    public ServerReflectionIndex(Server server) {
      this.server = server;
    }

    /**
     * When first called, initializes the immutable services index. Subsequent calls have no effect.
     *
     * <p>This must be called by the reflection service before returning a new
     * {@link ProtoReflectionStreamObserver}.
     */
    private synchronized void initializeImmutableServicesIndex() {
      if (immutableServicesIndex == null) {
        immutableServicesIndex = new FileDescriptorIndex(server.getImmutableServices());
      }
    }

    /**
     * Checks for updates to the server's mutable services and updates the index if any changes
     * are detected. A change is any addition or removal in the set of file descriptors attached to
     * the mutable services or a change in the service names.
     */
    private void updateMutableIndexIfNecessary() {
      Set<FileDescriptor> currentFileDescriptors = new HashSet<FileDescriptor>();
      Set<String> currentServiceNames = new HashSet<String>();
      synchronized (lock) {
        List<ServerServiceDefinition> currentMutableServices = server.getMutableServices();
        for (ServerServiceDefinition mutableService : currentMutableServices) {
          io.grpc.ServiceDescriptor serviceDescriptor = mutableService.getServiceDescriptor();
          if (serviceDescriptor.getMarshallerDescriptor() instanceof ProtoFileDescriptorSupplier) {
            String serviceName = serviceDescriptor.getName();
            FileDescriptor fileDescriptor =
                ((ProtoFileDescriptorSupplier) serviceDescriptor.getMarshallerDescriptor())
                    .getFileDescriptor();
            currentFileDescriptors.add(fileDescriptor);
            checkState(!currentServiceNames.contains(serviceName),
                "Service already defined: %s", serviceName);
            currentServiceNames.add(serviceName);
          }
        }

        // Replace the mutable index if the underlying services have changed. Check both the file
        // descriptors and the service names, because one file descriptor can define multiple
        // services.
        if (!mutableServicesIndex.getServiceFileDescriptors().equals(currentFileDescriptors)
            || !mutableServicesIndex.getServiceNames().equals(currentServiceNames)) {
          mutableServicesIndex = new FileDescriptorIndex(currentMutableServices);
        }
      }
    }

    private Set<String> getServiceNames() {
      Set<String> serviceNames = new HashSet<String>(immutableServicesIndex.getServiceNames());
      synchronized (lock) {
        serviceNames.addAll(mutableServicesIndex.getServiceNames());
      }
      return serviceNames;
    }

    @Nullable
    private FileDescriptor getFileDescriptorByName(String name) {
      FileDescriptor fd = immutableServicesIndex.getFileDescriptorByName(name);
      if (fd == null) {
        synchronized (lock) {
          fd = mutableServicesIndex.getFileDescriptorByName(name);
        }
      }
      return fd;
    }

    @Nullable
    private FileDescriptor getFileDescriptorBySymbol(String symbol) {
      FileDescriptor fd = immutableServicesIndex.getFileDescriptorBySymbol(symbol);
      if (fd == null) {
        synchronized (lock) {
          fd = mutableServicesIndex.getFileDescriptorBySymbol(symbol);
        }
      }
      return fd;
    }

    @Nullable
    private FileDescriptor getFileDescriptorByExtensionAndNumber(String type, int extension) {
      FileDescriptor fd
          = immutableServicesIndex.getFileDescriptorByExtensionAndNumber(type, extension);
      if (fd == null) {
        synchronized (lock) {
          fd = mutableServicesIndex.getFileDescriptorByExtensionAndNumber(type, extension);
        }
      }
      return fd;
    }

    @Nullable
    private Set<Integer> getExtensionNumbersOfType(String type) {
      Set<Integer> extensionNumbers = immutableServicesIndex.getExtensionNumbersOfType(type);
      if (extensionNumbers == null) {
        synchronized (lock) {
          extensionNumbers = mutableServicesIndex.getExtensionNumbersOfType(type);
        }
      }
      return extensionNumbers;
    }
  }

  /**
   * Provides a set of methods for answering reflection queries for the file descriptors
   * underlying a set of services. Used by {@link ServerReflectionIndex} to separately index
   * immutable and mutable services.
   */
  private static final class FileDescriptorIndex {
    private final Set<String> serviceNames = new HashSet<String>();
    private final Set<FileDescriptor> serviceFileDescriptors = new HashSet<FileDescriptor>();
    private final Map<String, FileDescriptor> fileDescriptorsByName
        = new HashMap<String, FileDescriptor>();
    private final Map<String, FileDescriptor> fileDescriptorsBySymbol
        = new HashMap<String, FileDescriptor>();
    private final Map<String, Map<Integer, FileDescriptor>> fileDescriptorsByExtensionAndNumber
        = new HashMap<String, Map<Integer, FileDescriptor>>();

    FileDescriptorIndex(List<ServerServiceDefinition> services) {
      Queue<FileDescriptor> fileDescriptorsToProcess = new LinkedList<FileDescriptor>();
      Set<String> seenFiles = new HashSet<String>();
      for (ServerServiceDefinition service : services) {
        io.grpc.ServiceDescriptor serviceDescriptor = service.getServiceDescriptor();
        if (serviceDescriptor.getMarshallerDescriptor() instanceof ProtoFileDescriptorSupplier) {
          FileDescriptor fileDescriptor =
              ((ProtoFileDescriptorSupplier) serviceDescriptor.getMarshallerDescriptor())
                  .getFileDescriptor();
          String serviceName = serviceDescriptor.getName();
          checkState(!serviceNames.contains(serviceName),
              "Service already defined: %s", serviceName);
          serviceFileDescriptors.add(fileDescriptor);
          serviceNames.add(serviceName);
          if (!seenFiles.contains(fileDescriptor.getName())) {
            seenFiles.add(fileDescriptor.getName());
            fileDescriptorsToProcess.add(fileDescriptor);
          }
        }
      }

      while (!fileDescriptorsToProcess.isEmpty()) {
        FileDescriptor currentFd = fileDescriptorsToProcess.remove();
        processFileDescriptor(currentFd);
        for (FileDescriptor dependencyFd : currentFd.getDependencies()) {
          if (!seenFiles.contains(dependencyFd.getName())) {
            seenFiles.add(dependencyFd.getName());
            fileDescriptorsToProcess.add(dependencyFd);
          }
        }
      }
    }

    /**
     * Returns the file descriptors for the indexed services, but not their dependencies. This is
     * used to check if the server's mutable services have changed.
     */
    private Set<FileDescriptor> getServiceFileDescriptors() {
      return Collections.unmodifiableSet(serviceFileDescriptors);
    }

    private Set<String> getServiceNames() {
      return Collections.unmodifiableSet(serviceNames);
    }

    @Nullable
    private FileDescriptor getFileDescriptorByName(String name) {
      return fileDescriptorsByName.get(name);
    }

    @Nullable
    private FileDescriptor getFileDescriptorBySymbol(String symbol) {
      return fileDescriptorsBySymbol.get(symbol);
    }

    @Nullable
    private FileDescriptor getFileDescriptorByExtensionAndNumber(String type, int number) {
      if (fileDescriptorsByExtensionAndNumber.containsKey(type)) {
        return fileDescriptorsByExtensionAndNumber.get(type).get(number);
      }
      return null;
    }

    @Nullable
    private Set<Integer> getExtensionNumbersOfType(String type) {
      if (fileDescriptorsByExtensionAndNumber.containsKey(type)) {
        return Collections.unmodifiableSet(fileDescriptorsByExtensionAndNumber.get(type).keySet());
      }
      return null;
    }

    private void processFileDescriptor(FileDescriptor fd) {
      String fdName = fd.getName();
      checkState(!fileDescriptorsByName.containsKey(fdName),
          "File name already used: %s", fdName);
      fileDescriptorsByName.put(fdName, fd);
      for (ServiceDescriptor service : fd.getServices()) {
        processService(service, fd);
      }
      for (Descriptor type : fd.getMessageTypes()) {
        processType(type, fd);
      }
      for (FieldDescriptor extension : fd.getExtensions()) {
        processExtension(extension, fd);
      }
    }

    private void processService(ServiceDescriptor service, FileDescriptor fd) {
      String serviceName = service.getFullName();
      checkState(!fileDescriptorsBySymbol.containsKey(serviceName),
          "Service already defined: %s", serviceName);
      fileDescriptorsBySymbol.put(serviceName, fd);
      for (MethodDescriptor method : service.getMethods()) {
        String methodName = method.getFullName();
        checkState(!fileDescriptorsBySymbol.containsKey(methodName),
            "Method already defined: %s", methodName);
        fileDescriptorsBySymbol.put(methodName, fd);
      }
    }

    private void processType(Descriptor type, FileDescriptor fd) {
      String typeName = type.getFullName();
      checkState(!fileDescriptorsBySymbol.containsKey(typeName),
          "Type already defined: %s", typeName);
      fileDescriptorsBySymbol.put(typeName, fd);
      for (FieldDescriptor extension : type.getExtensions()) {
        processExtension(extension, fd);
      }
      for (Descriptor nestedType : type.getNestedTypes()) {
        processType(nestedType, fd);
      }
    }

    private void processExtension(FieldDescriptor extension, FileDescriptor fd) {
      String extensionName = extension.getContainingType().getFullName();
      int extensionNumber = extension.getNumber();
      if (!fileDescriptorsByExtensionAndNumber.containsKey(extensionName)) {
        fileDescriptorsByExtensionAndNumber.put(
            extensionName, new HashMap<Integer, FileDescriptor>());
      }
      checkState(
          !fileDescriptorsByExtensionAndNumber.get(extensionName).containsKey(extensionNumber),
          "Extension name and number already defined: %s, %s", extensionName, extensionNumber);
      fileDescriptorsByExtensionAndNumber.get(extensionName).put(extensionNumber, fd);
    }
  }
}
