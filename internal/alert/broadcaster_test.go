package alert

import "testing"

func TestCrossPlatformSync_Buffered(t *testing.T) {
	// CrossPlatformSync is a buffered channel of size 50
	if cap(CrossPlatformSync) != 50 {
		t.Errorf("expected buffer size 50, got: %d", cap(CrossPlatformSync))
	}
}

func TestCrossPlatformSync_SendReceive(t *testing.T) {
	msg := "Slack User Approved: Scale web to 3"
	CrossPlatformSync <- msg

	received := <-CrossPlatformSync
	if received != msg {
		t.Errorf("expected '%s', got: '%s'", msg, received)
	}
}
