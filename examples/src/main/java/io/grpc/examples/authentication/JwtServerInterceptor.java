/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.examples.authentication;

import io.grpc.Metadata;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;

/**
 * Use a {@link ServerInterceptor} to capture metadata and retrieve any JWT token.
 *
 * This interceptor only captures the JWT token and prints it out.
 * Normally the token will need to be validated against an identity provider.
 */
public class JwtServerInterceptor implements ServerInterceptor {

    @Override
    public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(ServerCall<ReqT, RespT> serverCall, Metadata metadata, ServerCallHandler<ReqT, RespT> serverCallHandler) {
        // Get token from Metadata
        // Capture the JWT token and just print it out.
        String token = metadata.get(Constant.JWT_METADATA_KEY);
        System.out.println("Token: " + token);

        return serverCallHandler.startCall(serverCall, metadata);
    }

}
