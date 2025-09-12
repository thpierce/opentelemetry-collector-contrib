// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package k8sclient // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/k8s/k8sclient"

import corev1 "k8s.io/api/core/v1"

type PersistentVolumeClaimInfo struct {
	Name      string
	Namespace string
	Status    *PersistentVolumeClaimStatus
}

type PersistentVolumeClaimStatus struct {
	Phase corev1.PersistentVolumeClaimPhase
}
