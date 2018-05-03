/*
 * Copyright 2014 The gRPC Authors
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

import java.io.IOException;

/**
 * An {@link java.io.InputStream} or alike whose total number of bytes that can be read is known
 * upfront.
 *
 * <p>Usually it's a {@link java.io.InputStream} that also implements this interface, in which case
 * {@link java.io.InputStream#available()} has a stronger semantic by returning an accurate number
 * instead of an estimation.
 */
public interface KnownLength {
  /**
   * Returns the total number of bytes that can be read (or skipped over) from this object until all
   * bytes have been read out.
   */
  int available() throws IOException;
}
