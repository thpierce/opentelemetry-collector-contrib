// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package containerinsight // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/containerinsight"

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// SumFields takes an array of type map[string]any and do
// the summation on the values corresponding to the same keys.
// It is assumed that the underlying type of any to be float64.
func SumFields(fields []map[string]any) map[string]float64 {
	if len(fields) == 0 {
		return nil
	}

	result := make(map[string]float64)
	// Use the first element as the base
	for k, v := range fields[0] {
		if fv, ok := v.(float64); ok {
			result[k] = fv
		}
	}

	if len(fields) == 1 {
		return result
	}

	for i := 1; i < len(fields); i++ {
		for k, v := range result {
			if fields[i][k] == nil {
				continue
			}
			if fv, ok := fields[i][k].(float64); ok {
				result[k] = v + fv
			}
		}
	}
	return result
}

// IsNode checks if a type belongs to node level metrics (for EKS)
func IsNode(mType string) bool {
	switch mType {
	case
		TypeNode,
		TypeNodeDiskIO,
		TypeNodeEFA,
		TypeNodeFS,
		TypeNodeGPU,
		TypeNodeNet,
		TypeHyperPodNode:
		return true
	}
	return false
}

// IsInstance checks if a type belongs to instance level metrics (for ECS)
func IsInstance(mType string) bool {
	switch mType {
	case TypeInstance, TypeInstanceNet, TypeInstanceFS, TypeInstanceDiskIO:
		return true
	}
	return false
}

// IsContainer checks if a type belongs to container level metrics
func IsContainer(mType string) bool {
	switch mType {
	case
		TypeContainer,
		TypeContainerDiskIO,
		TypeContainerEFA,
		TypeContainerFS,
		TypeContainerGPU:
		return true
	}
	return false
}

// IsPod checks if a type belongs to container level metrics
func IsPod(mType string) bool {
	switch mType {
	case
		TypePod,
		TypePodEFA,
		TypePodGPU,
		TypePodNet:
		return true
	}
	return false
}

func getPrefixByMetricType(mType string) string {
	prefix := ""
	instancePrefix := "instance_"
	nodePrefix := "node_"
	instanceNetPrefix := "instance_interface_"
	nodeNetPrefix := "node_interface_"
	nodeEfaPrefix := "node_efa_"
	hyperPodNodeHealthStatus := "hyperpod_node_health_status_"
	podPrefix := "pod_"
	podNetPrefix := "pod_interface_"
	podEfaPrefix := "pod_efa_"
	containerPrefix := "container_"
	containerEfaPrefix := "container_efa_"
	service := "service_"
	cluster := "cluster_"
	namespace := "namespace_"
	deployment := "deployment_"
	daemonSet := "daemonset_"
	statefulSet := "statefulset_"
	replicaSet := "replicaset_"
	persistentVolume := "persistent_volume_"
	persistentVolumeClaim := "persistent_volume_claim_"

	switch mType {
	case TypeInstance:
		prefix = instancePrefix
	case TypeInstanceFS:
		prefix = instancePrefix
	case TypeInstanceDiskIO:
		prefix = instancePrefix
	case TypeInstanceNet:
		prefix = instanceNetPrefix
	case TypeNode:
		prefix = nodePrefix
	case TypeNodeFS:
		prefix = nodePrefix
	case TypeNodeDiskIO:
		prefix = nodePrefix
	case TypeNodeNet:
		prefix = nodeNetPrefix
	case TypeNodeEFA:
		prefix = nodeEfaPrefix
	case TypePod, TypePodGPU:
		prefix = podPrefix
	case TypePodNet:
		prefix = podNetPrefix
	case TypePodEFA:
		prefix = podEfaPrefix
	case TypeContainer:
		prefix = containerPrefix
	case TypeContainerDiskIO:
		prefix = containerPrefix
	case TypeContainerFS:
		prefix = containerPrefix
	case TypeContainerEFA:
		prefix = containerEfaPrefix
	case TypeService:
		prefix = service
	case TypeCluster:
		prefix = cluster
	case TypeClusterService:
		prefix = service
	case TypeClusterNamespace:
		prefix = namespace
	case TypeClusterDeployment:
		prefix = deployment
	case TypeClusterDaemonSet:
		prefix = daemonSet
	case TypeClusterStatefulSet:
		prefix = statefulSet
	case TypeClusterReplicaSet:
		prefix = replicaSet
	case TypeHyperPodNode:
		prefix = hyperPodNodeHealthStatus
	case TypePersistentVolumeClaim:
		prefix = persistentVolumeClaim
	case TypePersistentVolume:
		prefix = persistentVolume
	default:
		log.Printf("E! Unexpected MetricType: %s", mType)
	}
	return prefix
}

// MetricName returns the metric name based on metric type and measurement name
// For example, a type "node" and a measurement "cpu_utilization" gives "node_cpu_utilization"
func MetricName(mType string, measurement string) string {
	return getPrefixByMetricType(mType) + measurement
}

// RemovePrefix removes the prefix (e.g. "node_", "pod_") from the metric name
func RemovePrefix(mType string, metricName string) string {
	prefix := getPrefixByMetricType(mType)
	return strings.Replace(metricName, prefix, "", 1)
}

// GetUnitForMetric returns unit for a given metric
func GetUnitForMetric(metric string) string {
	return metricToUnitMap[metric]
}

type FieldsAndTagsPair struct {
	Fields map[string]any
	Tags   map[string]string
}

// ConvertToOTLPMetrics converts a field containing metric values and tags containing the relevant labels to OTLP metrics.
// For legacy reasons, the timestamp is stored in the tags map with the key "Timestamp", but, unlike other tags,
// it is not added as a resource attribute to avoid high-cardinality metrics.
func ConvertToFieldsAndTags(m pmetric.Metric, logger *zap.Logger) []FieldsAndTagsPair {
	var converted []FieldsAndTagsPair
	if m.Name() == "" {
		return converted
	}

	var dps pmetric.NumberDataPointSlice
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		dps = m.Gauge().DataPoints()
	case pmetric.MetricTypeSum:
		dps = m.Sum().DataPoints()
	default:
		logger.Warn("Unsupported metric type", zap.String("metric", m.Name()), zap.String("type", m.Type().String()))
	}

	if dps.Len() == 0 {
		logger.Warn("Metric has no datapoint", zap.String("metric", m.Name()))
	}

	for i := 0; i < dps.Len(); i++ {
		tags := make(map[string]string)
		attrs := dps.At(i).Attributes()
		attrs.Range(func(k string, v pcommon.Value) bool {
			tags[k] = v.AsString()
			return true
		})
		converted = append(converted, FieldsAndTagsPair{
			Fields: map[string]any{
				m.Name(): nil, // metric value not needed for attribute decoration
			},
			Tags: tags,
		})
	}
	return converted
}

// ConvertToOTLPMetrics converts a field containing metric values and a tag containing the relevant labels to OTLP metrics
func ConvertToOTLPMetrics(fields map[string]any, tags map[string]string, logger *zap.Logger) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rms := md.ResourceMetrics()
	rm := rms.AppendEmpty()

	var timestamp pcommon.Timestamp
	resource := rm.Resource()
	for tagKey, tagValue := range tags {
		if tagKey == Timestamp {
			timeNs, _ := strconv.ParseUint(tagValue, 10, 64)
			timestamp = pcommon.Timestamp(timeNs)

			// Do not add Timestamp as a resource attribute to avoid high-cardinality.
			continue
		}
		resource.Attributes().PutStr(tagKey, tagValue)
	}

	ilms := rm.ScopeMetrics()

	metricType := tags[MetricType]
	for key, value := range fields {
		metric := RemovePrefix(metricType, key)
		unit := GetUnitForMetric(metric)
		scopeMetric := ilms.AppendEmpty()
		switch t := value.(type) {
		case int:
			intGauge(scopeMetric, key, unit, int64(t), timestamp)
		case int32:
			intGauge(scopeMetric, key, unit, int64(t), timestamp)
		case int64:
			intGauge(scopeMetric, key, unit, t, timestamp)
		case uint:
			intGauge(scopeMetric, key, unit, int64(t), timestamp)
		case uint32:
			intGauge(scopeMetric, key, unit, int64(t), timestamp)
		case uint64:
			intGauge(scopeMetric, key, unit, int64(t), timestamp)
		case float32:
			doubleGauge(scopeMetric, key, unit, float64(t), timestamp)
		case float64:
			doubleGauge(scopeMetric, key, unit, t, timestamp)
		default:
			valueType := fmt.Sprintf("%T", value)
			logger.Warn("Detected unexpected field", zap.String("key", key), zap.Any("value", value), zap.String("value type", valueType))
		}
	}

	return md
}

func intGauge(ilm pmetric.ScopeMetrics, metricName string, unit string, value int64, ts pcommon.Timestamp) {
	metric := initMetric(ilm, metricName, unit)

	intGauge := metric.SetEmptyGauge()
	dataPoints := intGauge.DataPoints()
	dataPoint := dataPoints.AppendEmpty()

	dataPoint.SetIntValue(value)
	dataPoint.SetTimestamp(ts)
}

func doubleGauge(ilm pmetric.ScopeMetrics, metricName string, unit string, value float64, ts pcommon.Timestamp) {
	metric := initMetric(ilm, metricName, unit)

	doubleGauge := metric.SetEmptyGauge()
	dataPoints := doubleGauge.DataPoints()
	dataPoint := dataPoints.AppendEmpty()

	dataPoint.SetDoubleValue(value)
	dataPoint.SetTimestamp(ts)
}

func initMetric(ilm pmetric.ScopeMetrics, name, unit string) pmetric.Metric {
	metric := ilm.Metrics().AppendEmpty()
	metric.SetName(name)
	metric.SetUnit(unit)

	return metric
}

func IsWindowsHostProcessContainer() bool {
	// todo: Remove this workaround func when Windows AMIs has containerd 1.7 which solves upstream bug
	// https://kubernetes.io/docs/tasks/configure-pod-container/create-hostprocess-pod/#containerd-v1-6
	if runtime.GOOS == OperatingSystemWindows && os.Getenv(RunInContainer) == TrueValue && os.Getenv(RunAsHostProcessContainer) == TrueValue {
		return true
	}
	return false
}
