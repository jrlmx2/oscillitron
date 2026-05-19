// CLAUDE GENERATED
package main

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/config"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestResolveEndpointsSingleFromFlags(t *testing.T) {
	eps, err := resolveEndpoints(config.Properties{}, "http://localhost:9000", "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) == 0 {
		t.Fatal("expected single-endpoint fallback to populate all brain functions")
	}
	for bf, ep := range eps {
		if ep.BaseURL != "http://localhost:9000" {
			t.Errorf("bf %q: BaseURL = %q", bf, ep.BaseURL)
		}
		if ep.Model != "m" {
			t.Errorf("bf %q: Model = %q", bf, ep.Model)
		}
	}
	if _, ok := eps[session.BrainPlanning]; !ok {
		t.Error("planning must be bound (it's the demo root)")
	}
}

func TestResolveEndpointsMultiFromProperties(t *testing.T) {
	props := config.Properties{
		"hermes.endpoints.planning.url":    "http://p:1",
		"hermes.endpoints.planning.model":  "model-p",
		"hermes.endpoints.reasoning.url":   "http://r:2",
		"hermes.endpoints.critic.url":      "http://c:3",
		"hermes.endpoints.critic.model":    "model-c",
		// single-endpoint settings present too — must be ignored when
		// any multi-endpoint entry exists.
		"hermes.url":   "http://ignored",
		"hermes.model": "ignored",
	}
	eps, err := resolveEndpoints(props, "http://ignored", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 3 {
		t.Fatalf("want 3 endpoints, got %d (%+v)", len(eps), eps)
	}
	if eps[session.BrainPlanning].BaseURL != "http://p:1" ||
		eps[session.BrainPlanning].Model != "model-p" {
		t.Errorf("planning endpoint wrong: %+v", eps[session.BrainPlanning])
	}
	if eps[session.BrainReasoning].BaseURL != "http://r:2" ||
		eps[session.BrainReasoning].Model != "" {
		t.Errorf("reasoning endpoint wrong: %+v", eps[session.BrainReasoning])
	}
	if eps[session.BrainCritic].Model != "model-c" {
		t.Errorf("critic model not picked up: %+v", eps[session.BrainCritic])
	}
	if _, has := eps[session.BrainRetrieval]; has {
		t.Error("multi-endpoint mode should NOT auto-bind brain functions absent from config")
	}
}

func TestResolveEndpointsEmptyMeansStubMode(t *testing.T) {
	eps, err := resolveEndpoints(config.Properties{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 0 {
		t.Errorf("expected empty endpoints (stub mode), got %+v", eps)
	}
}
