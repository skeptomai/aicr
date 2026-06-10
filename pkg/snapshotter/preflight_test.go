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

package snapshotter

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestRunPreflight_NilConfig covers the exported guard.
func TestRunPreflight_NilConfig(t *testing.T) {
	var buf bytes.Buffer
	if err := RunPreflight(context.Background(), nil, &buf); err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
}

func TestRunPreflight_OK(t *testing.T) {
	clientset := fake.NewClientset()
	w := watch.NewFake()
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(w, nil))

	go func() {
		running := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "aicr-preflight", Namespace: "ns"},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
			},
		}
		w.Modify(running)
	}()

	var buf bytes.Buffer
	cfg := &AgentConfig{Namespace: "ns", JobName: "aicr", Image: "ghcr.io/nvidia/aicr:v1", Timeout: 5 * time.Second}
	if err := runPreflight(context.Background(), clientset, cfg, &buf); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if !strings.Contains(buf.String(), "Preflight OK") {
		t.Errorf("report missing success line: %q", buf.String())
	}
}

func TestRunPreflight_Failed(t *testing.T) {
	clientset := fake.NewClientset()
	w := watch.NewFake()
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(w, nil))

	go func() {
		stuck := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "aicr-preflight", Namespace: "ns"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{
					Image: "busybox:1.37",
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "back-off"}},
				}},
			},
		}
		w.Modify(stuck)
	}()

	var buf bytes.Buffer
	cfg := &AgentConfig{Namespace: "ns", JobName: "aicr", Image: "ghcr.io/nvidia/aicr:v1", Timeout: 5 * time.Second}
	if err := runPreflight(context.Background(), clientset, cfg, &buf); err == nil {
		t.Fatal("expected error for stuck probe, got nil")
	}
	if !strings.Contains(buf.String(), "Preflight FAILED") {
		t.Errorf("report missing failure line: %q", buf.String())
	}
}
