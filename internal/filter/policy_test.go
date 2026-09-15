package filter_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
)

func modePtr(mode filter.BlockingMode) *filter.BlockingMode { return &mode }

func TestCompileResolvesAProfileChain(t *testing.T) {
	custom := netip.MustParseAddr("192.0.2.9")

	set, err := filter.Compile(filter.Config{
		Profiles: []filter.ProfileSpec{
			{ID: "base", Mode: modePtr(filter.Refused)},
			{ID: "middle", Extends: "base", Mode: modePtr(filter.CustomAddress), Custom: &custom},
			{ID: "leaf", Extends: "middle"},
		},
		Default: "leaf",
	})
	require.NoError(t, err)

	got := set.Decide(mustParse("example.com"), "")

	require.Equal(t, filter.CustomAddress, got.Policy.Mode)
	require.Equal(t, custom, got.Policy.Custom)
}

func TestAProfileInheritsWhatItDoesNotSet(t *testing.T) {
	custom := netip.MustParseAddr("192.0.2.9")

	set, err := filter.Compile(filter.Config{
		Profiles: []filter.ProfileSpec{
			{ID: "parent", Mode: modePtr(filter.CustomAddress), Custom: &custom},
			{ID: "child", Extends: "parent"},
		},
		Default: "child",
	})
	require.NoError(t, err)

	got := set.Decide(mustParse("example.com"), "")

	require.Equal(t, filter.CustomAddress, got.Policy.Mode)
	require.Equal(t, custom, got.Policy.Custom)
}

func TestAChildProfileOverridesTheModeItSets(t *testing.T) {
	set, err := filter.Compile(filter.Config{
		Profiles: []filter.ProfileSpec{
			{ID: "parent", Mode: modePtr(filter.NXDomain)},
			{ID: "child", Extends: "parent", Mode: modePtr(filter.Refused)},
		},
		Default: "child",
	})
	require.NoError(t, err)

	got := set.Decide(mustParse("example.com"), "")

	require.Equal(t, filter.Refused, got.Policy.Mode)
}

func TestAClientGetsThePolicyOfItsOwnProfile(t *testing.T) {
	set, err := filter.Compile(filter.Config{
		Profiles: []filter.ProfileSpec{
			{ID: "strict", Mode: modePtr(filter.Refused)},
			{ID: "loose", Mode: modePtr(filter.NullAddress)},
		},
		Clients: []filter.ClientSpec{{Key: "tablet", Profile: "strict"}},
		Default: "loose",
	})
	require.NoError(t, err)

	require.Equal(t, filter.Refused, set.Decide(mustParse("example.com"), "tablet").Policy.Mode)
	require.Equal(t, filter.NullAddress, set.Decide(mustParse("example.com"), "laptop").Policy.Mode)
	require.Equal(t, filter.NullAddress, set.Decide(mustParse("example.com"), "").Policy.Mode)
}

func TestCompileRejectsAProfileCycle(t *testing.T) {
	for name, profiles := range map[string][]filter.ProfileSpec{
		"two profiles": {
			{ID: "a", Extends: "b"},
			{ID: "b", Extends: "a"},
		},
		"one profile": {{ID: "a", Extends: "a"}},
	} {
		_, err := filter.Compile(filter.Config{Profiles: profiles, Default: "a"})

		require.Error(t, err, "case=%q", name)
		require.Contains(t, err.Error(), "extends itself", "case=%q", name)
	}
}

func TestCompileRejectsAMissingDefaultProfile(t *testing.T) {
	_, err := filter.Compile(filter.Config{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "default profile")
}

func TestCompileRejectsAClientNamingAnUnknownProfile(t *testing.T) {
	_, err := filter.Compile(filter.Config{
		Profiles: []filter.ProfileSpec{{ID: "default"}},
		Clients:  []filter.ClientSpec{{Key: "tablet", Profile: "nope"}},
		Default:  "default",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not defined")
}

func TestCompileRejectsADuplicateProfile(t *testing.T) {
	_, err := filter.Compile(filter.Config{
		Profiles: []filter.ProfileSpec{{ID: "default"}, {ID: "default"}},
		Default:  "default",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "twice")
}

func TestCompileRejectsAnAddressOnAModeThatCannotUseIt(t *testing.T) {
	custom := netip.MustParseAddr("192.0.2.9")

	_, err := filter.Compile(filter.Config{
		Profiles: []filter.ProfileSpec{{ID: "default", Mode: modePtr(filter.NXDomain), Custom: &custom}},
		Default:  "default",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "does not take an address")
}

func TestCompileRejectsACustomModeWithNoAddress(t *testing.T) {
	_, err := filter.Compile(filter.Config{
		Profiles: []filter.ProfileSpec{{ID: "default", Mode: modePtr(filter.CustomAddress)}},
		Default:  "default",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "needs a valid address")
}

func TestParseBlockingModeRoundTripsWithString(t *testing.T) {
	for _, name := range []string{"nxdomain", "null-address", "custom-address", "refused"} {
		mode, err := filter.ParseBlockingMode(name)
		require.NoError(t, err, "name=%q", name)
		require.Equal(t, name, mode.String())
	}
}

func TestParseBlockingModeRejectsAnUnknownName(t *testing.T) {
	_, err := filter.ParseBlockingMode("drop")

	require.Error(t, err)
}
