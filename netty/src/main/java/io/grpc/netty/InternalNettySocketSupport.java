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

package io.grpc.netty;

import io.grpc.Internal;
import io.grpc.InternalChannelz.TcpInfo;
import java.util.Map;

/**
 * An internal accessor. Do not use.
 */
@Internal
public final class InternalNettySocketSupport {

  public interface InternalHelper extends NettySocketSupport.Helper {
    @Override
    InternalNativeSocketOptions getNativeSocketOptions(io.netty.channel.Channel ch);
  }

  public static final class InternalNativeSocketOptions
      extends NettySocketSupport.NativeSocketOptions {
    public InternalNativeSocketOptions(TcpInfo tcpInfo, Map<String, String> otherInfo) {
      super(tcpInfo, otherInfo);
    }
  }

  public static void setHelper(InternalHelper helper) {
    NettySocketSupport.setHelper(helper);
  }

  private InternalNettySocketSupport() {}
}
