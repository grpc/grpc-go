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

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ForwardingClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;

/**
 * Use a {@link ClientInterceptor} to insert a JWT in the client to outgoing calls, that's designed
 * specifically to append credentials information.  A token might expire, so for each call, if the
 * token has expired, you can proactively refresh it.
 *
 * Every time a call takes place, the handler will execute, and the
 * {@link io.grpc.CallCredentials2.MetadataApplier}
 * is applied to add a header. The method should not block: to refresh or fetch a token from the
 * network, it should be done asynchronously.
 */
public class JwtClientInterceptor implements ClientInterceptor {

    private String tokenValue = "my-default-token";

    public void setTokenValue(String tokenValue) {
      this.tokenValue = tokenValue;
    }

    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
            MethodDescriptor<ReqT, RespT> methodDescriptor, CallOptions callOptions,
            Channel channel) {
        return new ForwardingClientCall.SimpleForwardingClientCall<ReqT, RespT>(
                channel.newCall(methodDescriptor, callOptions)) {
            @Override
            public void start(Listener<RespT> responseListener, Metadata headers) {
              headers.put(Constant.JWT_METADATA_KEY, tokenValue);
              super.start(responseListener, headers);
            }
        };
    }
}
