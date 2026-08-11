package cmd

import "testing"

// A permanent answer must not arrive as a retryable fault. Every in-band code
// used to fall through to E_SERVER, so an agent would have retried a QR that no
// longer exists and a session that is not accepted -- neither of which improves
// by trying again.
func TestBusinessCode_PermanentAnswersAreNotRetryable(t *testing.T) {
	for _, testCase := range []struct {
		code any
		want string
	}{
		{1001, "E_NOT_FOUND"},   // 二维码不存在
		{"1001", "E_NOT_FOUND"}, // upstream sends it as a string in some payloads
		{600, "E_AUTH"},         // 未登录，请先登录
		{999999, "E_SERVER"},    // anything unclassified stays a server fault
	} {
		if got := businessCode(testCase.code); got != testCase.want {
			t.Errorf("businessCode(%v) = %s, want %s", testCase.code, got, testCase.want)
		}
	}
}
