/*
 * Copyright 2016 The gRPC Authors
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

import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * A {@link ThreadLocal}-based context storage implementation.  Used by default.
 */
final class ThreadLocalContextStorage extends Context.Storage {
  private static final Logger log = Logger.getLogger(ThreadLocalContextStorage.class.getName());

  /**
   * Currently bound context.
   */
  private static final ThreadLocal<Context> localContext = new ThreadLocal<Context>();

  @Override
  public Context doAttach(Context toAttach) {
    Context current = current();
    localContext.set(toAttach);
    return current;
  }

  @Override
  public void detach(Context toDetach, Context toRestore) {
    if (current() != toDetach) {
      // Log a severe message instead of throwing an exception as the context to attach is assumed
      // to be the correct one and the unbalanced state represents a coding mistake in a lower
      // layer in the stack that cannot be recovered from here.
      log.log(Level.SEVERE, "Context was not attached when detaching",
          new Throwable().fillInStackTrace());
    }
    doAttach(toRestore);
  }

  @Override
  public Context current() {
    return localContext.get();
  }
}
