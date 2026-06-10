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

	"github.com/NVIDIA/aicr/pkg/defaults"
	aicrerrors "github.com/NVIDIA/aicr/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestBuildProbePod verifies the probe pod mirrors the agent's placement and
// uses the lightweight probe image.
func TestBuildProbePod(t *testing.T) {
	d := NewDeployer(fake.NewClientset(), Config{
		Namespace:        "ns",
		JobName:          "aicr",
		NodeSelector:     map[string]string{"disktype": "ssd"},
		Tolerations:      []corev1.Toleration{{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists}},
		ImagePullSecrets: []string{"regcred"},
		RuntimeClassName: "nvidia",
	})

	p := d.buildProbePod("aicr-preflight")

	if got := p.Spec.Containers[0].Image; got != defaults.ProbeImage {
		t.Errorf("probe image = %q, want %q", got, defaults.ProbeImage)
	}
	if p.Spec.NodeSelector["disktype"] != "ssd" {
		t.Error("node selector not propagated")
	}
	if len(p.Spec.Tolerations) != 1 {
		t.Error("tolerations not propagated")
	}
	if len(p.Spec.ImagePullSecrets) != 1 || p.Spec.ImagePullSecrets[0].Name != "regcred" {
		t.Error("imagePullSecrets not propagated")
	}
	if p.Spec.RuntimeClassName == nil || *p.Spec.RuntimeClassName != "nvidia" {
		t.Error("runtimeClassName not propagated")
	}
	if p.Labels[labelAppName] != appName {
		t.Error("agent label not set")
	}
	if p.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Error("restart policy should be Never")
	}
}

// TestDeployerPreflight_Started drives the probe pod to Running via the watch
// and asserts a successful result.
func TestDeployerPreflight_Started(t *testing.T) {
	clientset := fake.NewClientset()
	w := watch.NewFake()
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(w, nil))
	d := NewDeployer(clientset, Config{Namespace: "ns", JobName: "aicr", Image: "ghcr.io/nvidia/aicr:v1"})

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

	res, err := d.Preflight(context.Background(), 5*time.Second)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res == nil || res.ProbeImage != defaults.ProbeImage {
		t.Errorf("unexpected result: %+v", res)
	}
}

// TestDeployerPreflight_ImagePullBackOff drives the probe pod into
// ImagePullBackOff and asserts a fast-fail with ErrCodeUnavailable.
func TestDeployerPreflight_ImagePullBackOff(t *testing.T) {
	clientset := fake.NewClientset()
	w := watch.NewFake()
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(w, nil))
	d := NewDeployer(clientset, Config{Namespace: "ns", JobName: "aicr", Image: "ghcr.io/nvidia/aicr:v1"})

	go func() {
		stuck := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "aicr-preflight", Namespace: "ns"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{
					Image: defaults.ProbeImage,
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "back-off"}},
				}},
			},
		}
		w.Modify(stuck)
	}()

	_, err := d.Preflight(context.Background(), 5*time.Second)
	if err == nil {
		t.Fatal("expected error for ImagePullBackOff probe, got nil")
	}
	if !stderrors.Is(err, aicrerrors.New(aicrerrors.ErrCodeUnavailable, "")) {
		t.Errorf("expected ErrCodeUnavailable, got %v", err)
	}
}
