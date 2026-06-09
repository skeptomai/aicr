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
	stderrors "errors"
	"testing"
	"time"

	aicrerrors "github.com/NVIDIA/aicr/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func agentLabeledPod(status corev1.PodStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aicr-abc",
			Namespace: "test-ns",
			Labels:    map[string]string{labelAppName: appName},
		},
		Status: status,
	}
}

// TestDeployerWaitForPodStarted_Running verifies the wrapper resolves the agent
// pod by label and returns once a container reports Running.
func TestDeployerWaitForPodStarted_Running(t *testing.T) {
	t.Parallel()

	pod := agentLabeledPod(corev1.PodStatus{
		Phase:             corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
	})
	d := NewDeployer(fake.NewClientset(pod), Config{Namespace: "test-ns", JobName: "aicr"})

	if err := d.WaitForPodStarted(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("expected nil for running pod, got %v", err)
	}
}

// TestDeployerWaitForPodStarted_ImagePullBackOff verifies the wrapper fails fast
// with ErrCodeUnavailable when the agent pod is stuck pulling its image.
func TestDeployerWaitForPodStarted_ImagePullBackOff(t *testing.T) {
	t.Parallel()

	pod := agentLabeledPod(corev1.PodStatus{
		Phase: corev1.PodPending,
		ContainerStatuses: []corev1.ContainerStatus{{
			Image: "ghcr.io/nvidia/aicr:v0.14.0",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "back-off"}},
		}},
	})
	d := NewDeployer(fake.NewClientset(pod), Config{Namespace: "test-ns", JobName: "aicr"})

	err := d.WaitForPodStarted(context.Background(), 5*time.Second)
	if err == nil {
		t.Fatal("expected fast-fail error for ImagePullBackOff, got nil")
	}
	if !stderrors.Is(err, aicrerrors.New(aicrerrors.ErrCodeUnavailable, "")) {
		t.Errorf("expected ErrCodeUnavailable, got %v", err)
	}
}
