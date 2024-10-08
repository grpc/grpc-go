/*
 * Copyright 2024 gRPC authors.
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
 *
 */

// Package internal contains code internal to the pickfirst package.
package internal

var (
	// SetRandShuffleForTesting pseudo-randomizes the order of addresses. "n"
	// is the number of elements. "swap" swaps the elements with indexes i and j.
	SetRandShuffleForTesting func(sf func(n int, swap func(i, j int)))

	// RevertRandShuffleFuncForTesting sets the real shuffle function back after testing.
	RevertRandShuffleFuncForTesting func()
)
