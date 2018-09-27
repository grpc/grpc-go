/*
 * Copyright 2018, gRPC Authors All rights reserved.
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

import java.io.Closeable;

/**
 * A binary log that can be installed on a channel or server. {@link #close} must be called after
 * all the servers and channels associated with the binary log are terminated.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4017")
public abstract class BinaryLog implements Closeable {

  public abstract <ReqT, RespT> ServerMethodDefinition<?, ?> wrapMethodDefinition(
      ServerMethodDefinition<ReqT, RespT> oMethodDef);

  public abstract Channel wrapChannel(Channel channel);
}
