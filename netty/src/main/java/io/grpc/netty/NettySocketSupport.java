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

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableMap;
import io.grpc.InternalChannelz.TcpInfo;
import io.netty.channel.Channel;
import java.util.Map;
import javax.annotation.Nullable;

/**
 * An class for getting low level socket info.
 */
final class NettySocketSupport {
  private static volatile Helper instance = new NettySocketHelperImpl();

  interface Helper {
    /**
     * Returns the info on the socket if possible. Returns null if the info can not be discovered.
     */
    @Nullable
    NativeSocketOptions getNativeSocketOptions(Channel ch);
  }

  /**
   * A TcpInfo and additional other info that will be turned into channelz socket options.
   */
  public static class NativeSocketOptions {
    @Nullable
    public final TcpInfo tcpInfo;
    public final ImmutableMap<String, String> otherInfo;

    /** Creates an instance. */
    public NativeSocketOptions(
        TcpInfo tcpInfo,
        Map<String, String> otherInfo) {
      Preconditions.checkNotNull(otherInfo);
      this.tcpInfo = tcpInfo;
      this.otherInfo = ImmutableMap.copyOf(otherInfo);
    }
  }

  public static NativeSocketOptions getNativeSocketOptions(Channel ch) {
    return instance.getNativeSocketOptions(ch);
  }

  static void setHelper(Helper helper) {
    instance = Preconditions.checkNotNull(helper);
  }

  private static final class NettySocketHelperImpl implements Helper {
    @Override
    public NativeSocketOptions getNativeSocketOptions(Channel ch) {
      // TODO(zpencer): if netty-epoll, use reflection to call EpollSocketChannel.tcpInfo()
      // And/or if some other low level socket support library is available, call it now.
      return null;
    }
  }
}
