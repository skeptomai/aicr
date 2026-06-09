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
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	aicrerrors "github.com/NVIDIA/aicr/pkg/errors"
	"github.com/NVIDIA/aicr/pkg/k8s/pod"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func startTestPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
}

// TestWaitForPodStarted_FastPath covers the initial-Get classification: a pod
// that is already running, terminal, or stuck is resolved without watching.
func TestWaitForPodStarted_FastPath(t *testing.T) {
	tests := []struct {
		name    string
		status  corev1.PodStatus
		wantErr bool
	}{
		{
			name: "running",
			status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
			},
			wantErr: false,
		},
		{
			name:    "succeeded (fast Job) passes through",
			status:  corev1.PodStatus{Phase: corev1.PodSucceeded},
			wantErr: false,
		},
		{
			name:    "failed passes through (completion wait classifies)",
			status:  corev1.PodStatus{Phase: corev1.PodFailed},
			wantErr: false,
		},
		{
			name: "image pull backoff fails fast",
			status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{
					Image: "ghcr.io/nvidia/aicr:v0.14.0",
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "back-off"}},
				}},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}, Status: tt.status}
			//nolint:staticcheck // SA1019: fake.NewSimpleClientset is sufficient for tests
			client := fake.NewSimpleClientset(p)
			err := pod.WaitForPodStarted(context.Background(), client, "default", "p", 2*time.Second)
			if (err != nil) != tt.wantErr {
				t.Errorf("WaitForPodStarted() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestWaitForPodStarted_TimeoutOnPending verifies a pod that never leaves
// Pending (no stuck reason) times out rather than returning early.
func TestWaitForPodStarted_TimeoutOnPending(t *testing.T) {
	//nolint:staticcheck // SA1019: fake.NewSimpleClientset is sufficient for tests
	client := fake.NewSimpleClientset(startTestPod("p"))
	err := pod.WaitForPodStarted(context.Background(), client, "default", "p", 400*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error for perpetually-pending pod, got nil")
	}
	if !stderrors.Is(err, aicrerrors.New(aicrerrors.ErrCodeTimeout, "")) {
		t.Errorf("expected ErrCodeTimeout, got: %v", err)
	}
}

// TestWaitForPodStarted_FastFailOnWatchTransition exercises the watch path: a
// pod that starts Pending and only later transitions to ImagePullBackOff must
// be caught via a watch event and fail fast with the reason named.
func TestWaitForPodStarted_FastFailOnWatchTransition(t *testing.T) {
	pending := startTestPod("p")
	pending.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:  "agent",
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}},
	}}
	//nolint:staticcheck // SA1019: fake.NewSimpleClientset is sufficient for tests
	client := fake.NewSimpleClientset(pending)
	watcher := watch.NewFake()
	client.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(watcher, nil))

	go func() {
		stuck := pending.DeepCopy()
		stuck.Status.ContainerStatuses[0].Image = "ghcr.io/nvidia/aicr:v0.14.0"
		stuck.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{
			Reason: "ImagePullBackOff", Message: "Back-off pulling image",
		}
		watcher.Modify(stuck)
	}()

	err := pod.WaitForPodStarted(context.Background(), client, "default", "p", 10*time.Second)
	if err == nil {
		t.Fatal("expected error after watch transition to ImagePullBackOff, got nil")
	}
	if !strings.Contains(err.Error(), "ImagePullBackOff") {
		t.Errorf("error should name the stuck reason, got: %v", err)
	}
	if !stderrors.Is(err, aicrerrors.New(aicrerrors.ErrCodeUnavailable, "")) {
		t.Errorf("expected ErrCodeUnavailable, got: %v", err)
	}
}

// TestWaitForPodStarted_RunningAfterPending verifies the happy path through the
// watch loop: a Pending pod that transitions to a Running container returns nil.
func TestWaitForPodStarted_RunningAfterPending(t *testing.T) {
	pending := startTestPod("p")
	//nolint:staticcheck // SA1019: fake.NewSimpleClientset is sufficient for tests
	client := fake.NewSimpleClientset(pending)
	watcher := watch.NewFake()
	client.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(watcher, nil))

	go func() {
		running := pending.DeepCopy()
		running.Status.Phase = corev1.PodRunning
		running.Status.ContainerStatuses = []corev1.ContainerStatus{{
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}}
		watcher.Modify(running)
	}()

	if err := pod.WaitForPodStarted(context.Background(), client, "default", "p", 10*time.Second); err != nil {
		t.Errorf("expected nil after pod started running, got: %v", err)
	}
}
