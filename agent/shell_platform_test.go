package agent

import "testing"

func TestPlatformComplete(t *testing.T) {
	if len(platform.Candidates) == 0 {
		t.Fatal("平台候选链不得为空")
	}
	for _, c := range platform.Candidates {
		if c == "" {
			t.Errorf("候选不得为空串: %v", platform.Candidates)
		}
	}
	if platform.ConfigureGroup == nil || platform.KillGroup == nil || platform.ProtectSignals == nil ||
		platform.ExitCode == nil || platform.ProcessStopped == nil || platform.Capabilities == nil {
		t.Errorf("平台能力存在缺项: %+v", platform)
	}
	for _, p := range platform.Programs {
		if p == "" {
			t.Errorf("程序清单不得含空串: %v", platform.Programs)
		}
	}
}
