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
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// StuckReason inspects a Pod for non-recoverable "stuck" states and returns a
// human-readable reason, or "" if the pod is not stuck. It reports container
// and init-container image-pull / crash-loop failures (ImagePullBackOff,
// ErrImagePull, InvalidImageName, CrashLoopBackOff) and unschedulable Pods —
// states that a readiness or completion wait would otherwise block on until its
// deadline. Callers use it to fail fast with an actionable reason instead.
func StuckReason(p *corev1.Pod) string {
	for _, cs := range p.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil {
			switch w.Reason {
			case "ImagePullBackOff", "ErrImagePull", "InvalidImageName", "CrashLoopBackOff":
				return fmt.Sprintf("%s: %s (image: %s)", w.Reason, w.Message, cs.Image)
			}
		}
	}
	for _, cs := range p.Status.InitContainerStatuses {
		if w := cs.State.Waiting; w != nil {
			switch w.Reason {
			case "ImagePullBackOff", "ErrImagePull", "InvalidImageName", "CrashLoopBackOff":
				return fmt.Sprintf("%s: %s (init container, image: %s)", w.Reason, w.Message, cs.Image)
			}
		}
	}
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse &&
			cond.Reason == string(corev1.PodReasonUnschedulable) {

			return fmt.Sprintf("Unschedulable: %s", cond.Message)
		}
	}
	return ""
}

// WaitingStatus returns the first container's waiting reason and message, or
// "none" if no container is in a waiting state. Intended for diagnostic output
// (e.g. the last observed state) when a wait times out.
func WaitingStatus(p *corev1.Pod) string {
	for _, cs := range p.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil {
			return fmt.Sprintf("%s: %s", w.Reason, w.Message)
		}
	}
	return "none"
}
