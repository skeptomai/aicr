// Copyright (c) 2026, NVIDIA CORPORATION & AFFILIATES.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package agent

import (
	"context"
	"time"

	"github.com/NVIDIA/aicr/pkg/defaults"
	aicrerrors "github.com/NVIDIA/aicr/pkg/errors"
	"github.com/NVIDIA/aicr/pkg/k8s/pod"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// PreflightResult summarizes a successful preflight probe.
type PreflightResult struct {
	// Node is the node the probe pod was scheduled onto ("" if not observed).
	Node string
	// ProbeImage is the lightweight image used for the probe.
	ProbeImage string
}

// preflightPodName returns the probe pod name derived from the Job name.
func (d *Deployer) preflightPodName() string {
	return d.config.JobName + "-preflight"
}

// buildProbePod builds a minimal busybox pod that mirrors the agent pod's
// scheduling constraints (node selector, tolerations, image-pull secrets,
// runtime class). The pod reaching Running/terminal proves the agent pod could
// schedule and that the node can pull and start a container — without pulling
// the multi-gigabyte agent image.
func (d *Deployer) buildProbePod(name string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: d.config.Namespace,
			Labels:    map[string]string{labelAppName: appName},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:    corev1.RestartPolicyNever,
			NodeSelector:     d.config.NodeSelector,
			Tolerations:      d.config.Tolerations,
			ImagePullSecrets: toLocalObjectReferences(d.config.ImagePullSecrets),
			Containers: []corev1.Container{{
				Name:    "preflight",
				Image:   defaults.ProbeImage,
				Command: []string{"sh", "-c", "echo aicr preflight: scheduled and started"},
			}},
		},
	}
	if d.config.RuntimeClassName != "" {
		p.Spec.RuntimeClassName = ptr.To(d.config.RuntimeClassName)
	}
	return p
}

// Preflight deploys a lightweight busybox probe pod with the same placement the
// agent Job would use, waits for it to schedule and start (failing fast on
// ImagePullBackOff / Unschedulable via WaitForPodStarted), then removes it. It
// verifies the agent pod could schedule on the target cluster and that the node
// can pull and start a container — cheaply, ahead of the full agent image pull.
func (d *Deployer) Preflight(ctx context.Context, timeout time.Duration) (*PreflightResult, error) {
	name := d.preflightPodName()
	probePod := d.buildProbePod(name)

	// Always remove the probe pod, even on failure, under a fresh context so
	// cleanup runs even when the parent context is already canceled.
	//nolint:contextcheck // intentional: cleanup needs a fresh context when the parent is canceled
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), defaults.K8sCleanupTimeout)
		defer cancel()
		_ = d.clientset.CoreV1().Pods(d.config.Namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{})
	}()

	_, err := d.clientset.CoreV1().Pods(d.config.Namespace).Create(ctx, probePod, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		// Stale probe from a prior run — delete and recreate so we observe a
		// fresh scheduling attempt rather than adopting an old terminal pod.
		if delErr := d.clientset.CoreV1().Pods(d.config.Namespace).Delete(ctx, name, metav1.DeleteOptions{}); delErr != nil {
			return nil, aicrerrors.Wrap(aicrerrors.ErrCodeInternal, "failed to delete stale preflight probe pod", delErr)
		}
		_, err = d.clientset.CoreV1().Pods(d.config.Namespace).Create(ctx, probePod, metav1.CreateOptions{})
	}
	if err != nil {
		return nil, aicrerrors.Wrap(aicrerrors.ErrCodeInternal, "failed to create preflight probe pod", err)
	}

	if startErr := pod.WaitForPodStarted(ctx, d.clientset, d.config.Namespace, name, timeout); startErr != nil {
		return nil, aicrerrors.Wrap(aicrerrors.ErrCodeUnavailable,
			"preflight: probe pod could not schedule or start (the agent Job would hit the same)", startErr)
	}

	node := ""
	if p, getErr := d.clientset.CoreV1().Pods(d.config.Namespace).Get(ctx, name, metav1.GetOptions{}); getErr == nil {
		node = p.Spec.NodeName
	}
	return &PreflightResult{Node: node, ProbeImage: defaults.ProbeImage}, nil
}
