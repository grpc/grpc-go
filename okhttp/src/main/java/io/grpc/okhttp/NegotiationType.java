/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.okhttp;

/**
 * Identifies the negotiation used for starting up HTTP/2.
 *
 * @deprecated use {@link OkHttpChannelBuilder#usePlaintext()} or {@link
 *     OkHttpChannelBuilder#useTransportSecurity()} directly rather than {@link
 *     OkHttpChannelBuilder#negotiationType(NegotiationType)}.
 */
@Deprecated
public enum NegotiationType {
  /**
   * Uses TLS ALPN/NPN negotiation, assumes an SSL connection.
   */
  TLS,

  /**
   * Just assume the connection is plaintext (non-SSL) and the remote endpoint supports HTTP/2
   * directly without an upgrade.
   */
  PLAINTEXT
}
