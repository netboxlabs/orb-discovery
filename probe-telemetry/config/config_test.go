package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeDefaults_ZeroOverride(t *testing.T) {
	base := Defaults{Site: "dc1", Role: "web", Location: "rack-a", Tenant: "acme", Tags: []string{"prod"}}
	result := MergeDefaults(base, Defaults{})
	assert.Equal(t, base, result)
}

func TestMergeDefaults_FullOverride(t *testing.T) {
	base := Defaults{Site: "dc1", Role: "web", Location: "rack-a", Tenant: "acme", Tags: []string{"prod"}}
	override := Defaults{Site: "dc2", Role: "infra", Location: "rack-b", Tenant: "beta", Tags: []string{"staging"}}
	result := MergeDefaults(base, override)
	assert.Equal(t, override, result)
}

func TestMergeDefaults_PartialOverride(t *testing.T) {
	base := Defaults{Site: "dc1", Role: "web", Tenant: "acme"}
	override := Defaults{Site: "dc2"}
	result := MergeDefaults(base, override)
	assert.Equal(t, "dc2", result.Site)
	assert.Equal(t, "web", result.Role)
	assert.Equal(t, "acme", result.Tenant)
}

func TestMergeDefaults_TagsOverride(t *testing.T) {
	base := Defaults{Tags: []string{"prod", "core"}}
	override := Defaults{Tags: []string{"staging"}}
	result := MergeDefaults(base, override)
	assert.Equal(t, []string{"staging"}, result.Tags)
}

func TestMergeDefaults_EmptyTagsDoNotOverride(t *testing.T) {
	base := Defaults{Tags: []string{"prod"}}
	result := MergeDefaults(base, Defaults{})
	assert.Equal(t, []string{"prod"}, result.Tags)
}

func TestMergeDefaults_BaseEmpty(t *testing.T) {
	override := Defaults{Site: "dc1", Role: "web"}
	result := MergeDefaults(Defaults{}, override)
	assert.Equal(t, "dc1", result.Site)
	assert.Equal(t, "web", result.Role)
}
