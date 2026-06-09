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

package pod

import (
	"context"
	"time"

	"github.com/NVIDIA/aicr/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// WaitForPodStarted waits until the pod has started executing — at least one
// container reports Running — or has reached a terminal phase (Succeeded or
// Failed, which a downstream completion wait will classify). It fails fast with
// ErrCodeUnavailable on a non-recoverable stuck state (ImagePullBackOff,
// ErrImagePull, InvalidImageName, CrashLoopBackOff, Unschedulable) and returns
// ErrCodeTimeout on deadline.
//
// Unlike WaitForPodReady it does NOT require the PodReady condition. A
// short-lived Job pod can run to completion (Succeeded) without kubelet ever
// publishing Ready=true; gating on Ready would falsely time out on such Jobs.
// This makes WaitForPodStarted the correct primitive for bounding the image
// pull / scheduling phase ahead of a separate Job-completion wait.
func WaitForPodStarted(ctx context.Context, client kubernetes.Interface, namespace, name string, timeout time.Duration) error {
	return watchPodUntil(ctx, client, namespace, name, timeout, checkPodStarted)
}

// checkPodStarted returns (true, nil) once the pod has started executing (a
// container is Running) or reached a terminal phase, (true, error) on a
// non-recoverable stuck state, and (false, nil) while still pending/pulling.
func checkPodStarted(p *corev1.Pod) (bool, error) {
	// Fail fast on non-recoverable states before anything else — a stuck pod
	// will neither start nor become terminal on its own.
	if reason := StuckReason(p); reason != "" {
		return true, errors.NewWithContext(errors.ErrCodeUnavailable,
			"pod stuck and will not start: "+reason,
			map[string]any{keyNamespace: p.Namespace, keyName: p.Name, keyReason: reason})
	}
	// Terminal phases are "started" for our purposes: the completion wait that
	// follows is responsible for classifying success vs failure.
	if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
		return true, nil
	}
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Running != nil {
			return true, nil
		}
	}
	return false, nil
}
