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
 */

// Package internal defines the PluginOption interface.
package internal

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// PluginOption is the interface which represents a plugin option for the
// OpenTelemetry instrumentation component. This plugin option emits labels from
// metadata and also sets labels in different forms of metadata. These labels
// are intended to be added to applicable OpenTelemetry metrics recorded in the
// OpenTelemetry instrumentation component.
//
// This API is experimental. In the future, we hope to stabilize and expose this
// API to allow pluggable plugin options to allow users to inject labels of
// their choosing into metrics recorded.
type PluginOption interface {
	// AddLabels adds metadata exchange labels to the outgoing metadata of the
	// context.
	AddLabels(context.Context) context.Context
	// NewLabelsMD creates metadata exchange labels as a new MD.
	NewLabelsMD() metadata.MD
	// GetLabels emits relevant labels from the metadata provided and the
	// optional labels provided, alongside any relevant labels.
	GetLabels(metadata.MD, map[string]string) map[string]string
}
