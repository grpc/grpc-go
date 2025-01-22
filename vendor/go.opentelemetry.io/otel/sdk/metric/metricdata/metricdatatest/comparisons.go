// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package metricdatatest // import "go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// equalResourceMetrics returns reasons ResourceMetrics are not equal. If they
// are equal, the returned reasons will be empty.
//
// The ScopeMetrics each ResourceMetrics contains are compared based on
// containing the same ScopeMetrics, not the order they are stored in.
func equalResourceMetrics(a, b metricdata.ResourceMetrics, cfg config) (reasons []string) {
	if !a.Resource.Equal(b.Resource) {
		reasons = append(reasons, notEqualStr("Resources", a.Resource, b.Resource))
	}

	r := compareDiff(diffSlices(
		a.ScopeMetrics,
		b.ScopeMetrics,
		func(a, b metricdata.ScopeMetrics) bool {
			r := equalScopeMetrics(a, b, cfg)
			return len(r) == 0
		},
	))
	if r != "" {
		reasons = append(reasons, "ResourceMetrics ScopeMetrics not equal:\n"+r)
	}
	return reasons
}

// equalScopeMetrics returns reasons ScopeMetrics are not equal. If they are
// equal, the returned reasons will be empty.
//
// The Metrics each ScopeMetrics contains are compared based on containing the
// same Metrics, not the order they are stored in.
func equalScopeMetrics(a, b metricdata.ScopeMetrics, cfg config) (reasons []string) {
	if a.Scope != b.Scope {
		reasons = append(reasons, notEqualStr("Scope", a.Scope, b.Scope))
	}

	r := compareDiff(diffSlices(
		a.Metrics,
		b.Metrics,
		func(a, b metricdata.Metrics) bool {
			r := equalMetrics(a, b, cfg)
			return len(r) == 0
		},
	))
	if r != "" {
		reasons = append(reasons, "ScopeMetrics Metrics not equal:\n"+r)
	}
	return reasons
}

// equalMetrics returns reasons Metrics are not equal. If they are equal, the
// returned reasons will be empty.
func equalMetrics(a, b metricdata.Metrics, cfg config) (reasons []string) {
	if a.Name != b.Name {
		reasons = append(reasons, notEqualStr("Name", a.Name, b.Name))
	}
	if a.Description != b.Description {
		reasons = append(reasons, notEqualStr("Description", a.Description, b.Description))
	}
	if a.Unit != b.Unit {
		reasons = append(reasons, notEqualStr("Unit", a.Unit, b.Unit))
	}

	r := equalAggregations(a.Data, b.Data, cfg)
	if len(r) > 0 {
		reasons = append(reasons, "Metrics Data not equal:")
		reasons = append(reasons, r...)
	}
	return reasons
}

// equalAggregations returns reasons a and b are not equal. If they are equal,
// the returned reasons will be empty.
func equalAggregations(a, b metricdata.Aggregation, cfg config) (reasons []string) {
	if a == nil || b == nil {
		if a != b {
			return []string{notEqualStr("Aggregation", a, b)}
		}
		return reasons
	}

	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return []string{fmt.Sprintf("Aggregation types not equal:\nexpected: %T\nactual: %T", a, b)}
	}

	switch v := a.(type) {
	case metricdata.Gauge[int64]:
		r := equalGauges(v, b.(metricdata.Gauge[int64]), cfg)
		if len(r) > 0 {
			reasons = append(reasons, "Gauge[int64] not equal:")
			reasons = append(reasons, r...)
		}
	case metricdata.Gauge[float64]:
		r := equalGauges(v, b.(metricdata.Gauge[float64]), cfg)
		if len(r) > 0 {
			reasons = append(reasons, "Gauge[float64] not equal:")
			reasons = append(reasons, r...)
		}
	case metricdata.Sum[int64]:
		r := equalSums(v, b.(metricdata.Sum[int64]), cfg)
		if len(r) > 0 {
			reasons = append(reasons, "Sum[int64] not equal:")
			reasons = append(reasons, r...)
		}
	case metricdata.Sum[float64]:
		r := equalSums(v, b.(metricdata.Sum[float64]), cfg)
		if len(r) > 0 {
			reasons = append(reasons, "Sum[float64] not equal:")
			reasons = append(reasons, r...)
		}
	case metricdata.Histogram[int64]:
		r := equalHistograms(v, b.(metricdata.Histogram[int64]), cfg)
		if len(r) > 0 {
			reasons = append(reasons, "Histogram not equal:")
			reasons = append(reasons, r...)
		}
	case metricdata.Histogram[float64]:
		r := equalHistograms(v, b.(metricdata.Histogram[float64]), cfg)
		if len(r) > 0 {
			reasons = append(reasons, "Histogram not equal:")
			reasons = append(reasons, r...)
		}
	case metricdata.ExponentialHistogram[int64]:
		r := equalExponentialHistograms(v, b.(metricdata.ExponentialHistogram[int64]), cfg)
		if len(r) > 0 {
			reasons = append(reasons, "ExponentialHistogram not equal:")
			reasons = append(reasons, r...)
		}
	case metricdata.ExponentialHistogram[float64]:
		r := equalExponentialHistograms(v, b.(metricdata.ExponentialHistogram[float64]), cfg)
		if len(r) > 0 {
			reasons = append(reasons, "ExponentialHistogram not equal:")
			reasons = append(reasons, r...)
		}
	case metricdata.Summary:
		r := equalSummary(v, b.(metricdata.Summary), cfg)
		if len(r) > 0 {
			reasons = append(reasons, "Summary not equal:")
			reasons = append(reasons, r...)
		}
	default:
		reasons = append(reasons, fmt.Sprintf("Aggregation of unknown types %T", a))
	}
	return reasons
}

// equalGauges returns reasons Gauges are not equal. If they are equal, the
// returned reasons will be empty.
//
// The DataPoints each Gauge contains are compared based on containing the
// same DataPoints, not the order they are stored in.
func equalGauges[N int64 | float64](a, b metricdata.Gauge[N], cfg config) (reasons []string) {
	r := compareDiff(diffSlices(
		a.DataPoints,
		b.DataPoints,
		func(a, b metricdata.DataPoint[N]) bool {
			r := equalDataPoints(a, b, cfg)
			return len(r) == 0
		},
	))
	if r != "" {
		reasons = append(reasons, "Gauge DataPoints not equal:\n"+r)
	}
	return reasons
}

// equalSums returns reasons Sums are not equal. If they are equal, the
// returned reasons will be empty.
//
// The DataPoints each Sum contains are compared based on containing the same
// DataPoints, not the order they are stored in.
func equalSums[N int64 | float64](a, b metricdata.Sum[N], cfg config) (reasons []string) {
	if a.Temporality != b.Temporality {
		reasons = append(reasons, notEqualStr("Temporality", a.Temporality, b.Temporality))
	}
	if a.IsMonotonic != b.IsMonotonic {
		reasons = append(reasons, notEqualStr("IsMonotonic", a.IsMonotonic, b.IsMonotonic))
	}

	r := compareDiff(diffSlices(
		a.DataPoints,
		b.DataPoints,
		func(a, b metricdata.DataPoint[N]) bool {
			r := equalDataPoints(a, b, cfg)
			return len(r) == 0
		},
	))
	if r != "" {
		reasons = append(reasons, "Sum DataPoints not equal:\n"+r)
	}
	return reasons
}

// equalHistograms returns reasons Histograms are not equal. If they are
// equal, the returned reasons will be empty.
//
// The DataPoints each Histogram contains are compared based on containing the
// same HistogramDataPoint, not the order they are stored in.
func equalHistograms[N int64 | float64](a, b metricdata.Histogram[N], cfg config) (reasons []string) {
	if a.Temporality != b.Temporality {
		reasons = append(reasons, notEqualStr("Temporality", a.Temporality, b.Temporality))
	}

	r := compareDiff(diffSlices(
		a.DataPoints,
		b.DataPoints,
		func(a, b metricdata.HistogramDataPoint[N]) bool {
			r := equalHistogramDataPoints(a, b, cfg)
			return len(r) == 0
		},
	))
	if r != "" {
		reasons = append(reasons, "Histogram DataPoints not equal:\n"+r)
	}
	return reasons
}

// equalDataPoints returns reasons DataPoints are not equal. If they are
// equal, the returned reasons will be empty.
func equalDataPoints[N int64 | float64](a, b metricdata.DataPoint[N], cfg config) (reasons []string) { // nolint: revive // Intentional internal control flag
	if !a.Attributes.Equals(&b.Attributes) {
		reasons = append(reasons, notEqualStr(
			"Attributes",
			a.Attributes.Encoded(attribute.DefaultEncoder()),
			b.Attributes.Encoded(attribute.DefaultEncoder()),
		))
	}

	if !cfg.ignoreTimestamp {
		if !a.StartTime.Equal(b.StartTime) {
			reasons = append(reasons, notEqualStr("StartTime", a.StartTime.UnixNano(), b.StartTime.UnixNano()))
		}
		if !a.Time.Equal(b.Time) {
			reasons = append(reasons, notEqualStr("Time", a.Time.UnixNano(), b.Time.UnixNano()))
		}
	}

	if !cfg.ignoreValue {
		if a.Value != b.Value {
			reasons = append(reasons, notEqualStr("Value", a.Value, b.Value))
		}
	}

	if !cfg.ignoreExemplars {
		r := compareDiff(diffSlices(
			a.Exemplars,
			b.Exemplars,
			func(a, b metricdata.Exemplar[N]) bool {
				r := equalExemplars(a, b, cfg)
				return len(r) == 0
			},
		))
		if r != "" {
			reasons = append(reasons, "Exemplars not equal:\n"+r)
		}
	}
	return reasons
}

// equalHistogramDataPoints returns reasons HistogramDataPoints are not equal.
// If they are equal, the returned reasons will be empty.
func equalHistogramDataPoints[N int64 | float64](a, b metricdata.HistogramDataPoint[N], cfg config) (reasons []string) { // nolint: revive // Intentional internal control flag
	if !a.Attributes.Equals(&b.Attributes) {
		reasons = append(reasons, notEqualStr(
			"Attributes",
			a.Attributes.Encoded(attribute.DefaultEncoder()),
			b.Attributes.Encoded(attribute.DefaultEncoder()),
		))
	}
	if !cfg.ignoreTimestamp {
		if !a.StartTime.Equal(b.StartTime) {
			reasons = append(reasons, notEqualStr("StartTime", a.StartTime.UnixNano(), b.StartTime.UnixNano()))
		}
		if !a.Time.Equal(b.Time) {
			reasons = append(reasons, notEqualStr("Time", a.Time.UnixNano(), b.Time.UnixNano()))
		}
	}
	if !cfg.ignoreValue {
		if a.Count != b.Count {
			reasons = append(reasons, notEqualStr("Count", a.Count, b.Count))
		}
		if !slices.Equal(a.Bounds, b.Bounds) {
			reasons = append(reasons, notEqualStr("Bounds", a.Bounds, b.Bounds))
		}
		if !slices.Equal(a.BucketCounts, b.BucketCounts) {
			reasons = append(reasons, notEqualStr("BucketCounts", a.BucketCounts, b.BucketCounts))
		}
		if !eqExtrema(a.Min, b.Min) {
			reasons = append(reasons, notEqualStr("Min", a.Min, b.Min))
		}
		if !eqExtrema(a.Max, b.Max) {
			reasons = append(reasons, notEqualStr("Max", a.Max, b.Max))
		}
		if a.Sum != b.Sum {
			reasons = append(reasons, notEqualStr("Sum", a.Sum, b.Sum))
		}
	}
	if !cfg.ignoreExemplars {
		r := compareDiff(diffSlices(
			a.Exemplars,
			b.Exemplars,
			func(a, b metricdata.Exemplar[N]) bool {
				r := equalExemplars(a, b, cfg)
				return len(r) == 0
			},
		))
		if r != "" {
			reasons = append(reasons, "Exemplars not equal:\n"+r)
		}
	}
	return reasons
}

// equalExponentialHistograms returns reasons exponential Histograms are not equal. If they are
// equal, the returned reasons will be empty.
//
// The DataPoints each Histogram contains are compared based on containing the
// same HistogramDataPoint, not the order they are stored in.
func equalExponentialHistograms[N int64 | float64](a, b metricdata.ExponentialHistogram[N], cfg config) (reasons []string) {
	if a.Temporality != b.Temporality {
		reasons = append(reasons, notEqualStr("Temporality", a.Temporality, b.Temporality))
	}

	r := compareDiff(diffSlices(
		a.DataPoints,
		b.DataPoints,
		func(a, b metricdata.ExponentialHistogramDataPoint[N]) bool {
			r := equalExponentialHistogramDataPoints(a, b, cfg)
			return len(r) == 0
		},
	))
	if r != "" {
		reasons = append(reasons, "Histogram DataPoints not equal:\n"+r)
	}
	return reasons
}

// equalExponentialHistogramDataPoints returns reasons HistogramDataPoints are not equal.
// If they are equal, the returned reasons will be empty.
func equalExponentialHistogramDataPoints[N int64 | float64](a, b metricdata.ExponentialHistogramDataPoint[N], cfg config) (reasons []string) { // nolint: revive // Intentional internal control flag
	if !a.Attributes.Equals(&b.Attributes) {
		reasons = append(reasons, notEqualStr(
			"Attributes",
			a.Attributes.Encoded(attribute.DefaultEncoder()),
			b.Attributes.Encoded(attribute.DefaultEncoder()),
		))
	}
	if !cfg.ignoreTimestamp {
		if !a.StartTime.Equal(b.StartTime) {
			reasons = append(reasons, notEqualStr("StartTime", a.StartTime.UnixNano(), b.StartTime.UnixNano()))
		}
		if !a.Time.Equal(b.Time) {
			reasons = append(reasons, notEqualStr("Time", a.Time.UnixNano(), b.Time.UnixNano()))
		}
	}
	if !cfg.ignoreValue {
		if a.Count != b.Count {
			reasons = append(reasons, notEqualStr("Count", a.Count, b.Count))
		}
		if !eqExtrema(a.Min, b.Min) {
			reasons = append(reasons, notEqualStr("Min", a.Min, b.Min))
		}
		if !eqExtrema(a.Max, b.Max) {
			reasons = append(reasons, notEqualStr("Max", a.Max, b.Max))
		}
		if a.Sum != b.Sum {
			reasons = append(reasons, notEqualStr("Sum", a.Sum, b.Sum))
		}

		if a.Scale != b.Scale {
			reasons = append(reasons, notEqualStr("Scale", a.Scale, b.Scale))
		}
		if a.ZeroCount != b.ZeroCount {
			reasons = append(reasons, notEqualStr("ZeroCount", a.ZeroCount, b.ZeroCount))
		}

		r := equalExponentialBuckets(a.PositiveBucket, b.PositiveBucket, cfg)
		if len(r) > 0 {
			reasons = append(reasons, r...)
		}
		r = equalExponentialBuckets(a.NegativeBucket, b.NegativeBucket, cfg)
		if len(r) > 0 {
			reasons = append(reasons, r...)
		}
	}
	if !cfg.ignoreExemplars {
		r := compareDiff(diffSlices(
			a.Exemplars,
			b.Exemplars,
			func(a, b metricdata.Exemplar[N]) bool {
				r := equalExemplars(a, b, cfg)
				return len(r) == 0
			},
		))
		if r != "" {
			reasons = append(reasons, "Exemplars not equal:\n"+r)
		}
	}
	return reasons
}

func equalExponentialBuckets(a, b metricdata.ExponentialBucket, _ config) (reasons []string) {
	if a.Offset != b.Offset {
		reasons = append(reasons, notEqualStr("Offset", a.Offset, b.Offset))
	}
	if !slices.Equal(a.Counts, b.Counts) {
		reasons = append(reasons, notEqualStr("Counts", a.Counts, b.Counts))
	}
	return reasons
}

func equalSummary(a, b metricdata.Summary, cfg config) (reasons []string) {
	r := compareDiff(diffSlices(
		a.DataPoints,
		b.DataPoints,
		func(a, b metricdata.SummaryDataPoint) bool {
			r := equalSummaryDataPoint(a, b, cfg)
			return len(r) == 0
		},
	))
	if r != "" {
		reasons = append(reasons, "Summary DataPoints not equal:\n"+r)
	}
	return reasons
}

func equalSummaryDataPoint(a, b metricdata.SummaryDataPoint, cfg config) (reasons []string) {
	if !a.Attributes.Equals(&b.Attributes) {
		reasons = append(reasons, notEqualStr(
			"Attributes",
			a.Attributes.Encoded(attribute.DefaultEncoder()),
			b.Attributes.Encoded(attribute.DefaultEncoder()),
		))
	}
	if !cfg.ignoreTimestamp {
		if !a.StartTime.Equal(b.StartTime) {
			reasons = append(reasons, notEqualStr("StartTime", a.StartTime.UnixNano(), b.StartTime.UnixNano()))
		}
		if !a.Time.Equal(b.Time) {
			reasons = append(reasons, notEqualStr("Time", a.Time.UnixNano(), b.Time.UnixNano()))
		}
	}
	if !cfg.ignoreValue {
		if a.Count != b.Count {
			reasons = append(reasons, notEqualStr("Count", a.Count, b.Count))
		}
		if a.Sum != b.Sum {
			reasons = append(reasons, notEqualStr("Sum", a.Sum, b.Sum))
		}
		r := compareDiff(diffSlices(
			a.QuantileValues,
			b.QuantileValues,
			func(a, b metricdata.QuantileValue) bool {
				r := equalQuantileValue(a, b, cfg)
				return len(r) == 0
			},
		))
		if r != "" {
			reasons = append(reasons, r)
		}
	}
	return reasons
}

func equalQuantileValue(a, b metricdata.QuantileValue, _ config) (reasons []string) {
	if a.Quantile != b.Quantile {
		reasons = append(reasons, notEqualStr("Quantile", a.Quantile, b.Quantile))
	}
	if a.Value != b.Value {
		reasons = append(reasons, notEqualStr("Value", a.Value, b.Value))
	}
	return reasons
}

func notEqualStr(prefix string, expected, actual interface{}) string {
	return fmt.Sprintf("%s not equal:\nexpected: %v\nactual: %v", prefix, expected, actual)
}

func equalExtrema[N int64 | float64](a, b metricdata.Extrema[N], _ config) (reasons []string) {
	if !eqExtrema(a, b) {
		reasons = append(reasons, notEqualStr("Extrema", a, b))
	}
	return reasons
}

func eqExtrema[N int64 | float64](a, b metricdata.Extrema[N]) bool {
	aV, aOk := a.Value()
	bV, bOk := b.Value()

	if !aOk || !bOk {
		return aOk == bOk
	}
	return aV == bV
}

func equalKeyValue(a, b attribute.KeyValue) bool {
	if a.Key != b.Key {
		return false
	}
	if a.Value.Type() != b.Value.Type() {
		return false
	}
	switch a.Value.Type() {
	case attribute.BOOL:
		if a.Value.AsBool() != b.Value.AsBool() {
			return false
		}
	case attribute.INT64:
		if a.Value.AsInt64() != b.Value.AsInt64() {
			return false
		}
	case attribute.FLOAT64:
		if a.Value.AsFloat64() != b.Value.AsFloat64() {
			return false
		}
	case attribute.STRING:
		if a.Value.AsString() != b.Value.AsString() {
			return false
		}
	case attribute.BOOLSLICE:
		if ok := slices.Equal(a.Value.AsBoolSlice(), b.Value.AsBoolSlice()); !ok {
			return false
		}
	case attribute.INT64SLICE:
		if ok := slices.Equal(a.Value.AsInt64Slice(), b.Value.AsInt64Slice()); !ok {
			return false
		}
	case attribute.FLOAT64SLICE:
		if ok := slices.Equal(a.Value.AsFloat64Slice(), b.Value.AsFloat64Slice()); !ok {
			return false
		}
	case attribute.STRINGSLICE:
		if ok := slices.Equal(a.Value.AsStringSlice(), b.Value.AsStringSlice()); !ok {
			return false
		}
	default:
		// We control all types passed to this, panic to signal developers
		// early they changed things in an incompatible way.
		panic(fmt.Sprintf("unknown attribute value type: %s", a.Value.Type()))
	}
	return true
}

func equalExemplars[N int64 | float64](a, b metricdata.Exemplar[N], cfg config) (reasons []string) {
	if !slices.EqualFunc(a.FilteredAttributes, b.FilteredAttributes, equalKeyValue) {
		reasons = append(reasons, notEqualStr("FilteredAttributes", a.FilteredAttributes, b.FilteredAttributes))
	}
	if !cfg.ignoreTimestamp {
		if !a.Time.Equal(b.Time) {
			reasons = append(reasons, notEqualStr("Time", a.Time.UnixNano(), b.Time.UnixNano()))
		}
	}
	if !cfg.ignoreValue {
		if a.Value != b.Value {
			reasons = append(reasons, notEqualStr("Value", a.Value, b.Value))
		}
	}
	if !slices.Equal(a.SpanID, b.SpanID) {
		reasons = append(reasons, notEqualStr("SpanID", a.SpanID, b.SpanID))
	}
	if !slices.Equal(a.TraceID, b.TraceID) {
		reasons = append(reasons, notEqualStr("TraceID", a.TraceID, b.TraceID))
	}
	return reasons
}

func diffSlices[T any](a, b []T, equal func(T, T) bool) (extraA, extraB []T) {
	visited := make([]bool, len(b))
	for i := 0; i < len(a); i++ {
		found := false
		for j := 0; j < len(b); j++ {
			if visited[j] {
				continue
			}
			if equal(a[i], b[j]) {
				visited[j] = true
				found = true
				break
			}
		}
		if !found {
			extraA = append(extraA, a[i])
		}
	}

	for j := 0; j < len(b); j++ {
		if visited[j] {
			continue
		}
		extraB = append(extraB, b[j])
	}

	return extraA, extraB
}

func compareDiff[T any](extraExpected, extraActual []T) string {
	if len(extraExpected) == 0 && len(extraActual) == 0 {
		return ""
	}

	formatter := func(v T) string {
		return fmt.Sprintf("%#v", v)
	}

	var msg bytes.Buffer
	if len(extraExpected) > 0 {
		_, _ = msg.WriteString("missing expected values:\n")
		for _, v := range extraExpected {
			_, _ = msg.WriteString(formatter(v) + "\n")
		}
	}

	if len(extraActual) > 0 {
		_, _ = msg.WriteString("unexpected additional values:\n")
		for _, v := range extraActual {
			_, _ = msg.WriteString(formatter(v) + "\n")
		}
	}

	return msg.String()
}

func missingAttrStr(name string) string {
	return "missing attribute " + name
}

func hasAttributesExemplars[T int64 | float64](exemplar metricdata.Exemplar[T], attrs ...attribute.KeyValue) (reasons []string) {
	s := attribute.NewSet(exemplar.FilteredAttributes...)
	for _, attr := range attrs {
		val, ok := s.Value(attr.Key)
		if !ok {
			reasons = append(reasons, missingAttrStr(string(attr.Key)))
			continue
		}
		if val != attr.Value {
			reasons = append(reasons, notEqualStr(string(attr.Key), attr.Value.Emit(), val.Emit()))
		}
	}
	return reasons
}

func hasAttributesDataPoints[T int64 | float64](dp metricdata.DataPoint[T], attrs ...attribute.KeyValue) (reasons []string) {
	for _, attr := range attrs {
		val, ok := dp.Attributes.Value(attr.Key)
		if !ok {
			reasons = append(reasons, missingAttrStr(string(attr.Key)))
			continue
		}
		if val != attr.Value {
			reasons = append(reasons, notEqualStr(string(attr.Key), attr.Value.Emit(), val.Emit()))
		}
	}
	return reasons
}

func hasAttributesGauge[T int64 | float64](gauge metricdata.Gauge[T], attrs ...attribute.KeyValue) (reasons []string) {
	for n, dp := range gauge.DataPoints {
		reas := hasAttributesDataPoints(dp, attrs...)
		if len(reas) > 0 {
			reasons = append(reasons, fmt.Sprintf("gauge datapoint %d attributes:\n", n))
			reasons = append(reasons, reas...)
		}
	}
	return reasons
}

func hasAttributesSum[T int64 | float64](sum metricdata.Sum[T], attrs ...attribute.KeyValue) (reasons []string) {
	for n, dp := range sum.DataPoints {
		reas := hasAttributesDataPoints(dp, attrs...)
		if len(reas) > 0 {
			reasons = append(reasons, fmt.Sprintf("sum datapoint %d attributes:\n", n))
			reasons = append(reasons, reas...)
		}
	}
	return reasons
}

func hasAttributesHistogramDataPoints[T int64 | float64](dp metricdata.HistogramDataPoint[T], attrs ...attribute.KeyValue) (reasons []string) {
	for _, attr := range attrs {
		val, ok := dp.Attributes.Value(attr.Key)
		if !ok {
			reasons = append(reasons, missingAttrStr(string(attr.Key)))
			continue
		}
		if val != attr.Value {
			reasons = append(reasons, notEqualStr(string(attr.Key), attr.Value.Emit(), val.Emit()))
		}
	}
	return reasons
}

func hasAttributesHistogram[T int64 | float64](histogram metricdata.Histogram[T], attrs ...attribute.KeyValue) (reasons []string) {
	for n, dp := range histogram.DataPoints {
		reas := hasAttributesHistogramDataPoints(dp, attrs...)
		if len(reas) > 0 {
			reasons = append(reasons, fmt.Sprintf("histogram datapoint %d attributes:\n", n))
			reasons = append(reasons, reas...)
		}
	}
	return reasons
}

func hasAttributesExponentialHistogramDataPoints[T int64 | float64](dp metricdata.ExponentialHistogramDataPoint[T], attrs ...attribute.KeyValue) (reasons []string) {
	for _, attr := range attrs {
		val, ok := dp.Attributes.Value(attr.Key)
		if !ok {
			reasons = append(reasons, missingAttrStr(string(attr.Key)))
			continue
		}
		if val != attr.Value {
			reasons = append(reasons, notEqualStr(string(attr.Key), attr.Value.Emit(), val.Emit()))
		}
	}
	return reasons
}

func hasAttributesExponentialHistogram[T int64 | float64](histogram metricdata.ExponentialHistogram[T], attrs ...attribute.KeyValue) (reasons []string) {
	for n, dp := range histogram.DataPoints {
		reas := hasAttributesExponentialHistogramDataPoints(dp, attrs...)
		if len(reas) > 0 {
			reasons = append(reasons, fmt.Sprintf("histogram datapoint %d attributes:\n", n))
			reasons = append(reasons, reas...)
		}
	}
	return reasons
}

func hasAttributesAggregation(agg metricdata.Aggregation, attrs ...attribute.KeyValue) (reasons []string) {
	switch agg := agg.(type) {
	case metricdata.Gauge[int64]:
		reasons = hasAttributesGauge(agg, attrs...)
	case metricdata.Gauge[float64]:
		reasons = hasAttributesGauge(agg, attrs...)
	case metricdata.Sum[int64]:
		reasons = hasAttributesSum(agg, attrs...)
	case metricdata.Sum[float64]:
		reasons = hasAttributesSum(agg, attrs...)
	case metricdata.Histogram[int64]:
		reasons = hasAttributesHistogram(agg, attrs...)
	case metricdata.Histogram[float64]:
		reasons = hasAttributesHistogram(agg, attrs...)
	case metricdata.ExponentialHistogram[int64]:
		reasons = hasAttributesExponentialHistogram(agg, attrs...)
	case metricdata.ExponentialHistogram[float64]:
		reasons = hasAttributesExponentialHistogram(agg, attrs...)
	case metricdata.Summary:
		reasons = hasAttributesSummary(agg, attrs...)
	default:
		reasons = []string{fmt.Sprintf("unknown aggregation %T", agg)}
	}
	return reasons
}

func hasAttributesMetrics(metrics metricdata.Metrics, attrs ...attribute.KeyValue) (reasons []string) {
	reas := hasAttributesAggregation(metrics.Data, attrs...)
	if len(reas) > 0 {
		reasons = append(reasons, fmt.Sprintf("Metric %s:\n", metrics.Name))
		reasons = append(reasons, reas...)
	}
	return reasons
}

func hasAttributesScopeMetrics(sm metricdata.ScopeMetrics, attrs ...attribute.KeyValue) (reasons []string) {
	for n, metrics := range sm.Metrics {
		reas := hasAttributesMetrics(metrics, attrs...)
		if len(reas) > 0 {
			reasons = append(reasons, fmt.Sprintf("ScopeMetrics %s Metrics %d:\n", sm.Scope.Name, n))
			reasons = append(reasons, reas...)
		}
	}
	return reasons
}

func hasAttributesResourceMetrics(rm metricdata.ResourceMetrics, attrs ...attribute.KeyValue) (reasons []string) {
	for n, sm := range rm.ScopeMetrics {
		reas := hasAttributesScopeMetrics(sm, attrs...)
		if len(reas) > 0 {
			reasons = append(reasons, fmt.Sprintf("ResourceMetrics ScopeMetrics %d:\n", n))
			reasons = append(reasons, reas...)
		}
	}
	return reasons
}

func hasAttributesSummary(summary metricdata.Summary, attrs ...attribute.KeyValue) (reasons []string) {
	for n, dp := range summary.DataPoints {
		reas := hasAttributesSummaryDataPoint(dp, attrs...)
		if len(reas) > 0 {
			reasons = append(reasons, fmt.Sprintf("summary datapoint %d attributes:\n", n))
			reasons = append(reasons, reas...)
		}
	}
	return reasons
}

func hasAttributesSummaryDataPoint(dp metricdata.SummaryDataPoint, attrs ...attribute.KeyValue) (reasons []string) {
	for _, attr := range attrs {
		val, ok := dp.Attributes.Value(attr.Key)
		if !ok {
			reasons = append(reasons, missingAttrStr(string(attr.Key)))
			continue
		}
		if val != attr.Value {
			reasons = append(reasons, notEqualStr(string(attr.Key), attr.Value.Emit(), val.Emit()))
		}
	}
	return reasons
}
