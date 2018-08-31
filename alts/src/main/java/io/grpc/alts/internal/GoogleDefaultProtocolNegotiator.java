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

package io.grpc.alts.internal;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.internal.GrpcAttributes;
import io.grpc.netty.GrpcHttp2ConnectionHandler;
import io.grpc.netty.ProtocolNegotiator;
import io.grpc.netty.ProtocolNegotiators;
import io.netty.handler.ssl.SslContext;

/** A client-side GPRC {@link ProtocolNegotiator} for Google Default Channel. */
public final class GoogleDefaultProtocolNegotiator implements ProtocolNegotiator {
  private final ProtocolNegotiator altsProtocolNegotiator;
  private final ProtocolNegotiator tlsProtocolNegotiator;

  public GoogleDefaultProtocolNegotiator(TsiHandshakerFactory altsFactory, SslContext sslContext) {
    altsProtocolNegotiator = AltsProtocolNegotiator.create(altsFactory);
    tlsProtocolNegotiator = ProtocolNegotiators.tls(sslContext);
  }

  @VisibleForTesting
  GoogleDefaultProtocolNegotiator(
      ProtocolNegotiator altsProtocolNegotiator, ProtocolNegotiator tlsProtocolNegotiator) {
    this.altsProtocolNegotiator = altsProtocolNegotiator;
    this.tlsProtocolNegotiator = tlsProtocolNegotiator;
  }

  @Override
  public Handler newHandler(GrpcHttp2ConnectionHandler grpcHandler) {
    if (grpcHandler.getEagAttributes().get(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY) != null
        || grpcHandler.getEagAttributes().get(GrpcAttributes.ATTR_LB_PROVIDED_BACKEND) != null) {
      return altsProtocolNegotiator.newHandler(grpcHandler);
    } else {
      return tlsProtocolNegotiator.newHandler(grpcHandler);
    }
  }
}
