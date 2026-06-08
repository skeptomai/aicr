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

package pod_test

import (
	"strings"
	"testing"

	"github.com/NVIDIA/aicr/pkg/k8s/pod"
	corev1 "k8s.io/api/core/v1"
)

func waitingPod(reason, image string, init bool) *corev1.Pod {
	cs := []corev1.ContainerStatus{{
		Image: image,
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: "back-off"},
		},
	}}
	p := &corev1.Pod{Status: corev1.PodStatus{}}
	if init {
		p.Status.InitContainerStatuses = cs
	} else {
		p.Status.ContainerStatuses = cs
	}
	return p
}

func TestStuckReason(t *testing.T) {
	tests := []struct {
		name      string
		pod       *corev1.Pod
		wantEmpty bool
		wantSub   string
	}{
		{name: "empty pod", pod: &corev1.Pod{}, wantEmpty: true},
		{name: "image pull backoff", pod: waitingPod("ImagePullBackOff", "ghcr.io/x:y", false), wantSub: "ImagePullBackOff"},
		{name: "err image pull", pod: waitingPod("ErrImagePull", "ghcr.io/x:y", false), wantSub: "ErrImagePull"},
		{name: "invalid image name", pod: waitingPod("InvalidImageName", "::::", false), wantSub: "InvalidImageName"},
		{name: "init crash loop", pod: waitingPod("CrashLoopBackOff", "init:latest", true), wantSub: "init container"},
		{
			name: "running not stuck",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				}},
			}},
			wantEmpty: true,
		},
		{
			name:      "creating not stuck",
			pod:       waitingPod("ContainerCreating", "img", false),
			wantEmpty: true,
		},
		{
			name: "unschedulable",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{{
					Type:    corev1.PodScheduled,
					Status:  corev1.ConditionFalse,
					Reason:  string(corev1.PodReasonUnschedulable),
					Message: "insufficient gpu",
				}},
			}},
			wantSub: "Unschedulable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pod.StuckReason(tt.pod)
			if tt.wantEmpty && got != "" {
				t.Errorf("StuckReason() = %q, want empty", got)
			}
			if !tt.wantEmpty && !strings.Contains(got, tt.wantSub) {
				t.Errorf("StuckReason() = %q, want substring %q", got, tt.wantSub)
			}
		})
	}
}

func TestWaitingStatus(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected string
	}{
		{name: "none", pod: &corev1.Pod{}, expected: "none"},
		{
			name:     "waiting",
			pod:      waitingPod("ContainerCreating", "img", false),
			expected: "ContainerCreating: back-off",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pod.WaitingStatus(tt.pod); got != tt.expected {
				t.Errorf("WaitingStatus() = %q, want %q", got, tt.expected)
			}
		})
	}
}
