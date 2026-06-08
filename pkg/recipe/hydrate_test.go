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

package recipe

import (
	"strings"
	"testing"
)

// TestHydrateHealthCheckAsserts exercises the hydration step that loads
// registry-declared healthCheck.assertFile content onto each ComponentRef
// during recipe resolution. See issue #1219.
func TestHydrateHealthCheckAsserts(t *testing.T) {
	provider := NewEmbeddedDataProvider(GetEmbeddedFS(), ".")
	registry, err := GetComponentRegistryFor(provider)
	if err != nil {
		t.Fatalf("GetComponentRegistryFor: %v", err)
	}

	// nfd is one of the components with a registry-declared assertFile.
	if cfg := registry.Get("nfd"); cfg == nil || cfg.HealthCheck.AssertFile == "" {
		t.Fatalf("test precondition: nfd must have healthCheck.assertFile in registry")
	}

	tests := []struct {
		name        string
		refs        []ComponentRef
		wantContent func(string) bool
		wantSkip    bool // expect HealthCheckAsserts to remain empty
	}{
		{
			name: "hydrates from registry assertFile",
			refs: []ComponentRef{{Name: "nfd"}},
			wantContent: func(s string) bool {
				return strings.Contains(s, "apiVersion:") && strings.Contains(s, "chainsaw")
			},
		},
		{
			name:     "skip sentinel suppresses hydration",
			refs:     []ComponentRef{{Name: "nfd", HealthCheckSkip: true}},
			wantSkip: true,
		},
		{
			name: "inline HealthCheckAsserts is preserved (overlay wins)",
			refs: []ComponentRef{{
				Name:               "nfd",
				HealthCheckAsserts: "inline-overlay-content",
			}},
			wantContent: func(s string) bool { return s == "inline-overlay-content" },
		},
		{
			name:     "unknown component is a no-op",
			refs:     []ComponentRef{{Name: "does-not-exist-in-registry"}},
			wantSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := tt.refs
			if err := hydrateHealthCheckAsserts(provider, registry, refs); err != nil {
				t.Fatalf("hydrateHealthCheckAsserts: %v", err)
			}
			got := refs[0].HealthCheckAsserts
			if tt.wantSkip {
				if got != "" {
					t.Fatalf("expected empty HealthCheckAsserts, got %q", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("expected hydrated HealthCheckAsserts, got empty")
			}
			if !tt.wantContent(got) {
				t.Fatalf("HealthCheckAsserts content did not satisfy predicate; got: %q", got)
			}
		})
	}
}

// TestHydrateHealthCheckAsserts_NilProvider verifies hydration falls back
// to the package-global embedded provider when nil is passed, matching the
// nil-fallback contract documented on applyRegistryDefaults.
func TestHydrateHealthCheckAsserts_NilProvider(t *testing.T) {
	registry, err := GetComponentRegistry()
	if err != nil {
		t.Fatalf("GetComponentRegistry: %v", err)
	}
	refs := []ComponentRef{{Name: "nfd"}}
	if err := hydrateHealthCheckAsserts(nil, registry, refs); err != nil {
		t.Fatalf("hydrateHealthCheckAsserts(nil provider): %v", err)
	}
	if refs[0].HealthCheckAsserts == "" {
		t.Fatalf("expected hydrated HealthCheckAsserts with nil provider fallback")
	}
}

// TestMergeComponentRef_HealthCheckSkip verifies the suppression sentinel
// propagates through overlay merge with set-if-true semantics (mirrors
// Cleanup).
func TestMergeComponentRef_HealthCheckSkip(t *testing.T) {
	tests := []struct {
		name     string
		base     ComponentRef
		overlay  ComponentRef
		wantSkip bool
	}{
		{
			name:     "overlay sets skip",
			base:     ComponentRef{Name: "x"},
			overlay:  ComponentRef{Name: "x", HealthCheckSkip: true},
			wantSkip: true,
		},
		{
			name:     "base set, overlay unset preserves",
			base:     ComponentRef{Name: "x", HealthCheckSkip: true},
			overlay:  ComponentRef{Name: "x"},
			wantSkip: true,
		},
		{
			name:     "neither set",
			base:     ComponentRef{Name: "x"},
			overlay:  ComponentRef{Name: "x"},
			wantSkip: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeComponentRef(tt.base, tt.overlay)
			if got.HealthCheckSkip != tt.wantSkip {
				t.Fatalf("HealthCheckSkip = %v, want %v", got.HealthCheckSkip, tt.wantSkip)
			}
		})
	}
}

// TestMixinComponentRefSafeForMerge_RejectsHealthCheckSkip ensures a mixin
// cannot silently suppress hydration of an inherited check. The merge
// resolver must reject the offending field by name.
func TestMixinComponentRefSafeForMerge_RejectsHealthCheckSkip(t *testing.T) {
	ref := ComponentRef{Name: "x", HealthCheckSkip: true}
	field, ok := mixinComponentRefSafeForMerge(ref)
	if ok {
		t.Fatalf("expected mixin with HealthCheckSkip to be rejected")
	}
	if field != "healthCheckSkip" {
		t.Fatalf("expected offending field name 'healthCheckSkip', got %q", field)
	}
}
