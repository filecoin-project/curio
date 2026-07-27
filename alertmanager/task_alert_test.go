package alertmanager

import (
	"strings"
	"testing"
	"time"

	"github.com/raulk/clock"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/alertmanager/plugin"
	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestAlertNow(t *testing.T) {
	// TestAlertNow tests alerting system
	realClock := build.Clock
	mockClock := clock.NewMock()
	// AlertTask runs only ping-health checks during the first AlertManagerInterval
	// of each FullAlertInterval. Pin this test just after that window so NowCheck
	// is included and the plugin dispatch path is exercised deterministically.
	mockClock.Set(time.Unix(int64(AlertManagerInterval/time.Second)+1, 0))
	build.Clock = mockClock
	t.Cleanup(func() {
		build.Clock = realClock
	})

	tp := &testPlugin{}
	oldTestPlugins := plugin.TestPlugins
	plugin.TestPlugins = []plugin.Plugin{
		tp,
	}
	t.Cleanup(func() {
		plugin.TestPlugins = oldTestPlugins
	})

	// Create dependencies
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	an := NewAlertNow(db, "alertNowMachine")
	an.AddAlert("testMessage")

	oldAlertFuncs := AlertFuncs
	AlertFuncs = map[AlertName]AlertFunc{
		Name_NowCheck: NowCheck,
	}
	t.Cleanup(func() {
		AlertFuncs = oldAlertFuncs
	})

	// Create a new alert task
	at := NewAlertTask(nil, db, config.CurioAlertingConfig{})
	done, err := at.Do(123, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, "Machine alertNowMachine: testMessage", tp.output)
}

func TestIsAlertMuted(t *testing.T) {
	pattern := func(s string) *string {
		return &s
	}

	tests := []struct {
		name      string
		alertName string
		message   string
		mutes     []alertMute
		want      bool
	}{
		{
			name:      "no mutes",
			alertName: "test_gui_render",
			message:   "Test ongoing alert for GUI rendering",
			want:      false,
		},
		{
			name:      "exact alert without pattern mutes alert",
			alertName: "test_gui_render",
			message:   "Test ongoing alert for GUI rendering",
			mutes: []alertMute{{
				AlertName: "test_gui_render",
			}},
			want: true,
		},
		{
			name:      "exact alert with matching pattern mutes alert",
			alertName: "test_gui_render",
			message:   "Test ongoing alert for GUI rendering",
			mutes: []alertMute{{
				AlertName: "test_gui_render",
				Pattern:   pattern("GUI rendering"),
			}},
			want: true,
		},
		{
			name:      "exact alert with non-matching pattern does not mute alert",
			alertName: "test_gui_render",
			message:   "Test ongoing alert for GUI rendering",
			mutes: []alertMute{{
				AlertName: "test_gui_render",
				Pattern:   pattern("different message"),
			}},
			want: false,
		},
		{
			name:      "others with matching pattern mutes custom alert",
			alertName: "test_gui_render",
			message:   "Test ongoing alert for GUI rendering",
			mutes: []alertMute{{
				AlertName: "others",
				Pattern:   pattern("Test ongoing alert for GUI rendering"),
			}},
			want: true,
		},
		{
			name:      "others with non-matching pattern does not mute custom alert",
			alertName: "test_gui_render",
			message:   "Test ongoing alert for GUI rendering",
			mutes: []alertMute{{
				AlertName: "others",
				Pattern:   pattern("different message"),
			}},
			want: false,
		},
		{
			name:      "unrelated alert name does not mute alert",
			alertName: "test_gui_render",
			message:   "Test ongoing alert for GUI rendering",
			mutes: []alertMute{{
				AlertName: "other_specific_alert",
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isAlertMuted(tt.alertName, tt.message, tt.mutes))
		})
	}
}

func TestAddAlertDetail(t *testing.T) {
	details := map[string]any{}

	// A message that pushes the total past maxAlertDetailLength gets truncated
	// instead of growing without bound.
	addAlertDetail(details, "Test", "first message")
	addAlertDetail(details, "Test", strings.Repeat("x", maxAlertDetailLength))
	got := details["Test"].(string)
	require.True(t, strings.HasSuffix(got, alertDetailTruncatedSuffix))
	require.LessOrEqual(t, len(got), maxAlertDetailLength+len(alertDetailTruncatedSuffix))

	// Once truncated, further additions in the same batch are dropped rather
	// than repeatedly appending the truncation suffix.
	addAlertDetail(details, "Test", "third message")
	require.Equal(t, got, details["Test"])
}

// testPlugin is a test plugin
type testPlugin struct {
	output string
}

func (tp *testPlugin) SendAlert(data *plugin.AlertPayload) error {
	tp.output = data.Details["NowCheck"].(string)
	return nil
}
