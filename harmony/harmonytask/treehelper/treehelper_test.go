package treehelper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindPreferredTaskRunOrder_chain(t *testing.T) {
	// test2_child -> test2_mid -> test2_root (each follows the previous)
	mayFollows := map[string][]string{
		"test2_child": {},
		"test2_mid":   {"test2_child"},
		"test2_root":  {"test2_mid"},
	}
	got := FindPreferredTaskRunOrder(mayFollows)
	require.Equal(t, []string{"test2_root", "test2_mid"}, got["test2_child"])
	require.Equal(t, []string{"test2_root"}, got["test2_mid"])
	require.Empty(t, got["test2_root"])
}

func TestFindPreferredTaskRunOrder_unrelated(t *testing.T) {
	got := FindPreferredTaskRunOrder(map[string][]string{
		"a": {},
		"b": {},
	})
	require.Empty(t, got["a"])
	require.Empty(t, got["b"])
}

func TestAssertMayFollowAcyclic_ok(t *testing.T) {
	require.NotPanics(t, func() {
		AssertMayFollowAcyclic(map[string][]string{
			"a": {},
			"b": {"a"},
			"c": {"a", "b"},
		})
	})
}

func TestAssertMayFollowAcyclic_selfLoop(t *testing.T) {
	require.Panics(t, func() {
		AssertMayFollowAcyclic(map[string][]string{
			"a": {"a"},
		})
	})
}

func TestAssertMayFollowAcyclic_twoCycle(t *testing.T) {
	require.Panics(t, func() {
		AssertMayFollowAcyclic(map[string][]string{
			"a": {"b"},
			"b": {"a"},
		})
	})
}

func TestAssertMayFollowAcyclic_longerCycle(t *testing.T) {
	require.Panics(t, func() {
		AssertMayFollowAcyclic(map[string][]string{
			"a": {"c"},
			"b": {"a"},
			"c": {"b"},
		})
	})
}
